package details

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// movieStaleAfter is how old a movie's metadata snapshot may get before a
// details view refreshes it from TMDB (D-015).
const movieStaleAfter = 48 * time.Hour

// MovieDetailsParams carries the raw query strings of GET /video/movies/details;
// the service owns validation. Exactly one of the two must be set — they are
// different id namespaces (D-013), kept apart by parameter name.
type MovieDetailsParams struct {
	TMDBID string
	ID     string
}

// MovieDetails returns one movie, live or from the local catalog (D-015):
//
//   - tmdb_id lookup with no local row: fetch /movie/{id} and serve it live,
//     storing nothing — viewing never creates catalog rows; tracking does.
//     TMDB errors propagate: with no local row there is nothing to fall back on.
//   - tmdb_id or id lookup with a row: serve the snapshot; one older than
//     movieStaleAfter is refreshed synchronously, and if that refresh fails the
//     stale snapshot is served and the failure logged — a movie with a local
//     row never turns into a 503.
//   - id lookup with no row: a plain 404, never a TMDB call.
func (s *Service) MovieDetails(ctx context.Context, userID int64, p MovieDetailsParams) (common.MovieDetails, error) {
	byTMDB, byID := p.TMDBID != "", p.ID != ""
	if byTMDB == byID {
		return common.MovieDetails{}, fmt.Errorf("%w: exactly one of tmdb_id and id is required", httpx.ErrValidation)
	}

	var row common.MovieRow
	if byID {
		id, err := strconv.ParseInt(p.ID, 10, 64)
		if err != nil || id < 1 {
			return common.MovieDetails{}, fmt.Errorf("%w: id must be a positive integer", httpx.ErrValidation)
		}
		row, err = common.GetMovieByID(ctx, s.DB, id)
		if err != nil {
			return common.MovieDetails{}, err
		}
	} else {
		tmdbID, err := strconv.ParseInt(p.TMDBID, 10, 64)
		if err != nil || tmdbID < 1 {
			return common.MovieDetails{}, fmt.Errorf("%w: tmdb_id must be a positive integer", httpx.ErrValidation)
		}
		row, err = common.GetMovieByTMDBID(ctx, s.DB, tmdbID)
		if errors.Is(err, httpx.ErrNotFound) {
			// No catalog row: serve TMDB's answer live and store nothing. The
			// row struct is just the in-memory carrier; ID stays 0 (= no row).
			live, ferr := s.FetchMovie(ctx, tmdbID)
			if ferr != nil {
				return common.MovieDetails{}, ferr
			}
			d := common.MovieDetailsFromRow(live, nil, s.WatchRegion)
			d.JellyfinSynced = s.JellyfinSynced()
			return d, nil
		}
		if err != nil {
			return common.MovieDetails{}, err
		}
	}

	if s.movieSnapshotStale(row.FetchedAt) || movieSnapshotMissingSections(row.MetadataJSON, s.WatchRegion) {
		fresh, err := s.RefreshMovie(ctx, row.TMDBID)
		if err != nil {
			// Serving slightly old data beats failing a page the library can
			// answer — the local-first contract (D-015).
			slog.Warn("tmdb: movie refresh failed, serving stale snapshot",
				"movie_id", row.ID, "tmdb_id", row.TMDBID, "fetched_at", row.FetchedAt, "err", err)
		} else {
			row = fresh
			// The refreshed snapshot may name different artwork: reconcile the
			// permanent copies — delete replaced files, enqueue new ones.
			s.SyncMovieAssets(ctx, row)
		}
	}

	d := common.MovieDetailsFromRow(row, s.MovieFiles(row.ID), s.WatchRegion)
	tracked, err := common.MovieReferenced(ctx, s.DB, row.ID)
	if err != nil {
		return common.MovieDetails{}, err
	}
	d.Tracked = tracked
	st, err := common.UserMovieFlags(ctx, s.DB, userID, row.ID)
	if err != nil {
		return common.MovieDetails{}, err
	}
	d.Watched, d.Watchlisted, d.Favorited, d.UserRating = st.Watched, st.Watchlisted, st.Favorited, st.Rating
	d.Copies, _, err = s.CopiesFor(ctx, userID, "movie", row.ID)
	if err != nil {
		return common.MovieDetails{}, err
	}
	d.JellyfinSynced = s.JellyfinSynced()
	return d, nil
}

// movieSnapshotStale reports whether a stored fetched_at is older than
// movieStaleAfter. An unparseable value counts as stale: refreshing rewrites
// it, which is also the self-repair path.
func (s *Service) movieSnapshotStale(fetchedAt string) bool {
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return true
	}
	return s.Now().Sub(t) > movieStaleAfter
}

// movieSnapshotMissingSections reports whether metadata_json predates one of
// the appended sections — external_ids/videos, the images galleries added with
// the media section, or the recommendations added with the similar rail — or
// carries a provider listing for a country other than the one now configured
// (watchProvidersStale). Such a snapshot needs one refresh even when it is not
// yet time-stale, which is how a widened append reaches existing rows without a
// migration: every stored movie refetches once on its next view. A fresh fetch
// always carries every key (empty or not), so this triggers at most once per old
// row; if the refresh fails, the serve-stale path applies and the new fields
// render as null or empty.
func movieSnapshotMissingSections(metadataJSON, region string) bool {
	var probe struct {
		ExternalIDs     json.RawMessage `json:"external_ids"`
		Images          json.RawMessage `json:"images"`
		Recommendations json.RawMessage `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &probe); err != nil {
		return true
	}
	return probe.ExternalIDs == nil || probe.Images == nil || probe.Recommendations == nil ||
		watchProvidersStale(metadataJSON, region)
}
