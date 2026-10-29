package common

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// The TV half of add-item enrichment. Tracking is what creates catalog rows
// (D-015), and for a show "the rows" means the show, every season and every
// episode — SPEC §5's eager rule applied to the whole subtree, so the library
// is complete BEFORE a provider fails rather than after.

// ShowStaleAfter matches movieStaleAfter and seasonStaleAfter: D-015 gives all
// three snapshots the same two-day window, so there is deliberately not a
// fourth number here.
const ShowStaleAfter = 48 * time.Hour

// fetchShow fetches /tv/{id} and maps the response into a ShowRow without
// touching the database. ID stays 0; the caller's upsert assigns one.
func (s *Core) fetchShow(ctx context.Context, tmdbID int64) (ShowRow, provider.TMDBTVDetails, error) {
	details, raw, err := s.TMDB.TVDetails(ctx, tmdbID)
	if err != nil {
		return ShowRow{}, provider.TMDBTVDetails{}, err
	}

	names := make([]string, 0, len(details.Genres))
	for _, g := range details.Genres {
		if g.Name != "" {
			names = append(names, g.Name)
		}
	}
	genresJSON, err := json.Marshal(names)
	if err != nil {
		return ShowRow{}, provider.TMDBTVDetails{}, fmt.Errorf("encoding genres: %w", err)
	}
	genres := string(genresJSON)

	return ShowRow{
		TMDBID:       details.ID,
		Title:        details.Name,
		FirstAirDate: NilIfEmpty(details.FirstAirDate),
		Status:       NilIfEmpty(details.Status),
		Overview:     NilIfEmpty(details.Overview),
		GenresJSON:   &genres,
		PosterPath:   NilIfEmpty(details.PosterPath),
		BackdropPath: NilIfEmpty(details.BackdropPath),
		// Pruned to the configured region, not verbatim — see FetchMovie and
		// D-022. This is the listing the show's seasons and episodes borrow.
		MetadataJSON: string(PruneWatchProviders(raw, s.WatchRegion)),
		FetchedAt:    s.Now().UTC().Format(time.RFC3339),
	}, details, nil
}

// RefreshShow rewrites ONLY the shows row, plus any season the refreshed root
// now lists that has no local row yet.
//
// It deliberately does not re-fetch existing seasons: this runs on a details
// VIEW, and re-fetching would make a routine page load cost one TMDB call per
// season every two days for every tracked show. The new-season branch is
// bounded — it fires only when a show actually gained a season — and it closes
// the hole where a returning series' new season would otherwise never appear.
// Keeping existing seasons and episodes current is the season view's job
// (seasonStaleAfter) and the scheduled air-date refresh's (D-006).
//
// "No local row yet" is a plain existence check: every season row is written
// beneath a tracked show, with its link and its episodes, so a row here always
// means a complete season. The season details view materialises a new season
// the same way when it is the page the user happens to open first.
func (s *Core) RefreshShow(ctx context.Context, tmdbID int64) (ShowRow, error) {
	row, details, err := s.fetchShow(ctx, tmdbID)
	if err != nil {
		return ShowRow{}, err
	}

	known, err := SeasonIDsByShow(ctx, s.DB, tmdbID)
	if err != nil {
		return ShowRow{}, err
	}
	var missing []seasonMaterialisation
	for _, season := range details.Seasons {
		if _, ok := known[season.SeasonNumber]; ok {
			continue
		}
		fetched, err := s.fetchSeasonForShow(ctx, tmdbID, season.SeasonNumber)
		if err != nil {
			// A new season that will not fetch must not fail the refresh: the
			// show's own snapshot is still good, so write it and whatever
			// seasons did arrive, and let the next view pick up the rest.
			//
			// Partial is acceptable HERE and not in MaterialiseShow, because
			// nothing recomputes a status off the back of this: a refresh runs
			// on a details view, where the catalog merely being behind TMDB is
			// already the normal state.
			slog.Warn("tmdb: fetching a new season failed, refreshing without it",
				"tmdb_id", tmdbID, "season_number", season.SeasonNumber, "err", err)
			break
		}
		missing = append(missing, fetched)
	}
	return s.writeShow(ctx, row, missing)
}

// seasonMaterialisation pairs a fetched season row with the episodes parsed out
// of its snapshot, so the whole subtree can be buffered before any of it is
// written.
type seasonMaterialisation struct {
	row      SeasonRow
	episodes []provider.TMDBTVSeasonEpisode
}

// fetchSeasonForShow fetches one season and parses its episode list back out of
// the snapshot it just stored — the same round trip seasonDetailsFromRow makes,
// so the rows written here and the DTO rendered from them cannot disagree.
func (s *Core) fetchSeasonForShow(ctx context.Context, showTMDBID int64, seasonNumber int) (seasonMaterialisation, error) {
	row, err := s.FetchSeason(ctx, showTMDBID, seasonNumber)
	if err != nil {
		return seasonMaterialisation{}, err
	}
	var snap SeasonSnapshot
	// Tolerant, like every other read of a snapshot: an undecodable season
	// materialises with no episodes rather than failing the whole show.
	_ = json.Unmarshal([]byte(row.MetadataJSON), &snap)
	return seasonMaterialisation{row: row, episodes: snap.Episodes}, nil
}

// MaterialiseShow fetches a show and EVERY one of its seasons and episodes,
// then writes the lot in ONE transaction.
//
// All-or-nothing on purpose. RecomputeShowStatus's completion predicate reads
// every episode of the show, so a half-written show reports a status that is
// not reproducible from the log — and SPEC §6 specifies "mark whole show
// watched" as a single call, which needs the complete episode set to exist
// before it runs. A failure part-way therefore writes nothing and surfaces the
// provider error, exactly as an untracked movie's materialisation does.
//
// Cost, stated plainly: this is N+1 sequential TMDB calls inside one request —
// roughly 1-3 s for a typical show and up to ~8 s for a twenty-season one, once
// per show ever. The SPA shows a pending state rather than marking optimistically.
func (s *Core) MaterialiseShow(ctx context.Context, tmdbID int64) (ShowRow, error) {
	row, details, err := s.fetchShow(ctx, tmdbID)
	if err != nil {
		return ShowRow{}, err
	}

	seasons := make([]seasonMaterialisation, 0, len(details.Seasons))
	for _, season := range details.Seasons {
		fetched, err := s.fetchSeasonForShow(ctx, tmdbID, season.SeasonNumber)
		if err != nil {
			return ShowRow{}, err
		}
		seasons = append(seasons, fetched)
	}
	return s.writeShow(ctx, row, seasons)
}

// writeShow commits a show and the seasons and episodes it was fetched with.
//
// The fetches happen BEFORE this is called, never inside it: under WAL a write
// transaction held across several seconds of network calls blocks every other
// writer in the process. Do not "tidy" the loop above into this function.
func (s *Core) writeShow(ctx context.Context, row ShowRow, seasons []seasonMaterialisation) (ShowRow, error) {
	var out ShowRow
	err := s.DB.InTx(ctx, func(tx db.Tx) error {
		var txErr error
		out, txErr = UpsertShowByTMDBID(ctx, tx, row)
		if txErr != nil {
			return txErr
		}
		for _, season := range seasons {
			sr := season.row
			sr.ShowID = &out.ID
			if _, txErr = UpsertSeasonByTMDBKey(ctx, tx, out.ID, sr); txErr != nil {
				return txErr
			}
			if txErr = upsertEpisodes(ctx, tx, out.ID, sr.SeasonNumber, season.episodes); txErr != nil {
				return txErr
			}
		}
		return nil
	})
	return out, err
}

// ResolveTrackedShow is the single materialisation gate: every show-, season-
// and episode-shaped tracking entry point funnels through it, so an episode can
// never be marked watched before its show's catalog rows exist.
//
// A show that already has a row is returned as-is — tracking is not a refresh,
// and re-fetching twenty seasons because somebody rated an episode would be
// absurd. Staleness is the details view's business.
func (s *Core) ResolveTrackedShow(ctx context.Context, tmdbID int64) (ShowRow, error) {
	row, err := GetShowByTMDBID(ctx, s.DB, tmdbID)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, httpx.ErrNotFound) {
		return ShowRow{}, err
	}
	return s.MaterialiseShow(ctx, tmdbID)
}
