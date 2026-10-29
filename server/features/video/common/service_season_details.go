package common

import (
	"context"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/provider"
)

// The two episode-still variants. StillSizeSingle is what a season's episode
// grid renders; StillSizeFull is the episode details page's hero, which is the
// one view that shows a still larger than its row. Stills are 16:9, so both
// values are backdrop sizes and are already in the proxy whitelist
// (BackdropSizeThumb / BackdropSizeFull).
const (
	StillSizeSingle = "w300"
	StillSizeFull   = "w1280"
)

// FetchSeason fetches one season and maps it into a SeasonRow without touching
// the database. ID stays 0; RefreshSeason's upsert is what assigns one.
func (s *Core) FetchSeason(ctx context.Context, showTMDBID int64, seasonNumber int) (SeasonRow, error) {
	details, showTitle, raw, err := s.TMDB.TVSeasonDetails(ctx, showTMDBID, seasonNumber)
	if err != nil {
		return SeasonRow{}, err
	}
	return SeasonRow{
		ShowTMDBID:   showTMDBID,
		SeasonNumber: details.SeasonNumber,
		Name:         details.Name,
		ShowTitle:    showTitle,
		AirDate:      NilIfEmpty(details.AirDate),
		EpisodeCount: len(details.Episodes),
		Overview:     NilIfEmpty(details.Overview),
		PosterPath:   NilIfEmpty(details.PosterPath),
		MetadataJSON: string(raw),
		FetchedAt:    s.Now().UTC().Format(time.RFC3339),
	}, nil
}

// RefreshSeason fetches a fresh snapshot of one season of a TRACKED show and
// writes it, linked, with its episodes — the same subtree slice writeShow
// commits, minus the shows row. One code path covers materialising a season
// TMDB has added since the show was written, a staleness refresh, and two
// concurrent views racing — the loser's upsert lands on the winner's row
// instead of failing.
//
// It takes the show rather than a tmdb id because there is no untracked case:
// only tracking creates catalog rows (D-015), and a caller that has no show has
// nothing to write a season under.
//
// Deliberately no status recompute here: like RefreshShow's new-season branch,
// this runs on a details view, where the catalog being behind TMDB is the
// normal state — a stored status that newly-visible episodes would demote
// self-corrects at the user's next mark, delete or bulk action, and
// ShowProgress is derived per request so the progress line is right
// immediately.
func (s *Core) RefreshSeason(ctx context.Context, show ShowRow, seasonNumber int) (SeasonRow, error) {
	// The fetch stays OUTSIDE the transaction — writeShow's WAL note applies
	// here too.
	fetched, err := s.fetchSeasonForShow(ctx, show.TMDBID, seasonNumber)
	if err != nil {
		return SeasonRow{}, err
	}
	row := fetched.row
	row.ShowID = &show.ID

	var out SeasonRow
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		var txErr error
		out, txErr = UpsertSeasonByTMDBKey(ctx, tx, show.ID, row)
		if txErr != nil {
			return txErr
		}
		return upsertEpisodes(ctx, tx, show.ID, row.SeasonNumber, fetched.episodes)
	})
	if err != nil {
		return SeasonRow{}, err
	}
	// Best-effort, like every other call site; it self-gates on ShowReferenced,
	// so an unreferenced show enqueues nothing.
	s.SyncShowAssets(ctx, show)
	return out, nil
}

// SeasonSnapshot is the shape parsed back out of a season row's metadata_json.
// Only what the DTO renders beyond the denormalised columns — the episodes and
// the season score. The one appended section a season document carries, its
// aggregate credits, is read through MediaSnapshot instead so the show's cast
// projection serves it unchanged; a season has no videos or image galleries.
type SeasonSnapshot struct {
	VoteAverage float64                        `json:"vote_average"`
	Episodes    []provider.TMDBTVSeasonEpisode `json:"episodes"`
}

// EpisodeMarker maps TMDB's episode_type (plus the premiere rule) onto the
// up-next cards' marker vocabulary, or nil for a plain episode.
func EpisodeMarker(e provider.TMDBTVSeasonEpisode) *string {
	var m string
	switch {
	case e.EpisodeType == "finale":
		m = "season_finale"
	case e.EpisodeType == "mid_season":
		m = "mid_season_finale"
	case e.EpisodeNumber == 1:
		m = "season_premiere"
	default:
		return nil
	}
	return &m
}
