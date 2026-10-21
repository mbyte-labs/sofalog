package common

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// episodeRow mirrors the episodes catalog table. Nullable columns are pointers.
// The addressing key is (ShowID, SeasonNumber, EpisodeNumber); the id is what
// user_watch_history and user_video_ratings target.
type episodeRow struct {
	ID            int64
	ShowID        int64
	SeasonNumber  int
	EpisodeNumber int
	Title         *string
	AirDate       *string
	RuntimeMin    *int
	Overview      *string
	StillPath     *string
}

const episodeColumns = `id, show_id, season_number, episode_number, title,
air_date, runtime_min, overview, still_path`

func scanEpisode(row *sql.Row) (episodeRow, error) {
	var e episodeRow
	err := row.Scan(&e.ID, &e.ShowID, &e.SeasonNumber, &e.EpisodeNumber, &e.Title,
		&e.AirDate, &e.RuntimeMin, &e.Overview, &e.StillPath)
	if errors.Is(err, sql.ErrNoRows) {
		return episodeRow{}, fmt.Errorf("%w: no such episode", httpx.ErrNotFound)
	}
	if err != nil {
		return episodeRow{}, err
	}
	return e, nil
}

func getEpisodeByID(ctx context.Context, d *db.DB, id int64) (episodeRow, error) {
	const q = `SELECT ` + episodeColumns + ` FROM episodes WHERE id = ?`
	return scanEpisode(d.ReadRow(ctx, q, id))
}

// ShowIDForEpisode resolves an episode's owning show inside the caller's
// transaction — getEpisodeByID runs on the read pool and so cannot see writes
// the open transaction has not committed. Deleting a watch needs this to know
// which show to recompute.
func ShowIDForEpisode(ctx context.Context, tx db.Tx, episodeID int64) (int64, error) {
	const q = `SELECT show_id FROM episodes WHERE id = ?`
	var showID int64
	err := tx.QueryRow(ctx, q, episodeID).Scan(&showID)
	return showID, err
}

// GetEpisodeByShowKey resolves the addressing form the episode details page
// actually holds: the show's local id plus the two positional numbers.
func GetEpisodeByShowKey(ctx context.Context, d *db.DB, showID int64, season, episode int) (episodeRow, error) {
	const q = `SELECT ` + episodeColumns + ` FROM episodes
WHERE show_id = ? AND season_number = ? AND episode_number = ?`
	return scanEpisode(d.ReadRow(ctx, q, showID, season, episode))
}

// EpisodeIDsBySeason maps one season's episode numbers to their local episode
// ids, so the season and episode views can render a mark control that addresses
// an episode by id. Joins shows on tmdb_id because that is the key those views
// already hold.
//
// An untracked show has no episodes rows and returns an EMPTY MAP, never an
// error — deliberately the same contract as SeasonIDsByShow, so an untracked
// view degrades to null ids rather than failing.
func EpisodeIDsBySeason(ctx context.Context, d *db.DB, showTMDBID int64, seasonNumber int) (map[int]int64, error) {
	const q = `
SELECT e.episode_number, e.id
FROM episodes e JOIN shows s ON s.id = e.show_id
WHERE s.tmdb_id = ? AND e.season_number = ?`
	rows, err := d.Read(ctx, q, showTMDBID, seasonNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[int]int64)
	for rows.Next() {
		var number int
		var id int64
		if err := rows.Scan(&number, &id); err != nil {
			return nil, err
		}
		ids[number] = id
	}
	return ids, rows.Err()
}

// WatchedEpisodeIDs maps one season's episode numbers to whether THIS user has
// ever watched them. Same empty-map-not-an-error contract as EpisodeIDsBySeason.
//
// EXISTS rather than a join: user_watch_history is many-per-target (D-009), so
// joining it would return one row per rewatch.
func WatchedEpisodeIDs(ctx context.Context, d *db.DB, userID, showTMDBID int64, seasonNumber int) (map[int]bool, error) {
	const q = `
SELECT e.episode_number
FROM episodes e JOIN shows s ON s.id = e.show_id
WHERE s.tmdb_id = ? AND e.season_number = ?
  AND EXISTS (SELECT 1 FROM user_watch_history wh
              WHERE wh.user_id = ? AND wh.target_type = 'episode' AND wh.target_id = e.id)`
	rows, err := d.Read(ctx, q, showTMDBID, seasonNumber, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	watched := make(map[int]bool)
	for rows.Next() {
		var number int
		if err := rows.Scan(&number); err != nil {
			return nil, err
		}
		watched[number] = true
	}
	return watched, rows.Err()
}

// RatedEpisodeIDs maps one season's episode numbers to THIS user's rating of
// them. Same empty-map-not-an-error contract as the two lookups above.
func RatedEpisodeIDs(ctx context.Context, d *db.DB, userID, showTMDBID int64, seasonNumber int) (map[int]int, error) {
	const q = `
SELECT e.episode_number, vr.rating
FROM episodes e
JOIN shows s ON s.id = e.show_id
JOIN user_video_ratings vr
  ON vr.target_type = 'episode' AND vr.target_id = e.id AND vr.user_id = ?
WHERE s.tmdb_id = ? AND e.season_number = ?`
	rows, err := d.Read(ctx, q, userID, showTMDBID, seasonNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ratings := make(map[int]int)
	for rows.Next() {
		var number, rating int
		if err := rows.Scan(&number, &rating); err != nil {
			return nil, err
		}
		ratings[number] = rating
	}
	return ratings, rows.Err()
}

// episodeStillPaths maps a show's (season, episode) pairs to their still paths,
// for the asset diff. Reading the columns rather than re-parsing every season's
// metadata_json matters: a twenty-season show is megabytes of JSON and a few
// hundred short rows.
func episodeStillPaths(ctx context.Context, d *db.DB, showID int64) (map[[2]int]string, error) {
	const q = `SELECT season_number, episode_number, still_path FROM episodes
WHERE show_id = ? AND still_path IS NOT NULL`
	rows, err := d.Read(ctx, q, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stills := make(map[[2]int]string)
	for rows.Next() {
		var season, episode int
		var path string
		if err := rows.Scan(&season, &episode, &path); err != nil {
			return nil, err
		}
		stills[[2]int{season, episode}] = path
	}
	return stills, rows.Err()
}

// upsertEpisodes writes one season's episodes for a show, converging on a
// second materialise rather than duplicating.
//
// It deliberately NEVER DELETES an episode TMDB stopped listing. A delete would
// orphan user_watch_history rows — the column is polymorphic and has no foreign
// key — and would silently discard a user's marks on a provider blip. The
// accepted cost is that a phantom episode can hold a show at 'watching'. Real
// removal belongs to the D-008 sweep, which does its mirror deletes explicitly.
func upsertEpisodes(ctx context.Context, tx db.Tx, showID int64, seasonNumber int, eps []provider.TMDBTVSeasonEpisode) error {
	const q = `
INSERT INTO episodes (show_id, season_number, episode_number, title, air_date,
                      runtime_min, overview, still_path)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (show_id, season_number, episode_number) DO UPDATE SET
    title       = excluded.title,
    air_date    = excluded.air_date,
    runtime_min = excluded.runtime_min,
    overview    = excluded.overview,
    still_path  = excluded.still_path`
	for _, e := range eps {
		// TMDB sends 0 for an unknown runtime, which is not the same as a
		// zero-minute episode — the season DTO already treats it as absent.
		var runtime *int
		if e.Runtime > 0 {
			r := e.Runtime
			runtime = &r
		}
		if _, err := tx.Exec(ctx, q, showID, seasonNumber, e.EpisodeNumber,
			NilIfEmpty(e.Name), NilIfEmpty(e.AirDate), runtime,
			NilIfEmpty(e.Overview), NilIfEmpty(e.StillPath)); err != nil {
			return err
		}
	}
	return nil
}

// historyEpisodeRow is one episode joined to its show, plus the season poster
// the feed prefers — everything a history entry needs in one round trip.
type historyEpisodeRow struct {
	ShowID       int64
	ShowTitle    string
	SeasonNumber int
	// SeasonID is the local seasons.id — the handle the episode details page
	// addresses this episode by. An episode row is only ever written alongside
	// its season (show tracking, or a season view/refresh of a tracked show),
	// in the same transaction that upserts that season with a non-null
	// show_id, so in practice this is never null; it is scanned as a pointer
	// anyway, because a nullable target costs nothing and the feed keeps
	// rendering if the season row ever goes away.
	SeasonID         *int64
	EpisodeNumber    int
	Title            *string
	ShowPosterPath   *string
	SeasonPosterPath *string
}

// getHistoryEpisode resolves one episode for the history feed. The season
// poster and the season id are scalar subqueries rather than a third join: a
// season may not have a row at all, and a LEFT JOIN here would be one more
// thing to reason about for no gain.
//
// Both carry ORDER BY … LIMIT 1 for portability: SQLite silently takes one row
// from a scalar subquery that returns several, where Postgres raises.
func getHistoryEpisode(ctx context.Context, d *db.DB, episodeID int64) (historyEpisodeRow, error) {
	const q = `
SELECT e.show_id, s.title, e.season_number, e.episode_number, e.title, s.poster_path,
       (SELECT se.poster_path FROM seasons se
        WHERE se.show_id = s.id AND se.season_number = e.season_number
        ORDER BY se.id LIMIT 1),
       (SELECT se.id FROM seasons se
        WHERE se.show_id = s.id AND se.season_number = e.season_number
        ORDER BY se.id LIMIT 1)
FROM episodes e JOIN shows s ON s.id = e.show_id
WHERE e.id = ?`
	var r historyEpisodeRow
	err := d.ReadRow(ctx, q, episodeID).Scan(&r.ShowID, &r.ShowTitle, &r.SeasonNumber,
		&r.EpisodeNumber, &r.Title, &r.ShowPosterPath, &r.SeasonPosterPath, &r.SeasonID)
	if errors.Is(err, sql.ErrNoRows) {
		return historyEpisodeRow{}, fmt.Errorf("%w: no such episode", httpx.ErrNotFound)
	}
	if err != nil {
		return historyEpisodeRow{}, err
	}
	return r, nil
}
