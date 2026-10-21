package stats

import (
	"context"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
)

// Every query here takes the period as a pair of RFC3339 UTC bounds, From
// inclusive and Until exclusive, computed in Go from the user's zone (D-038).
// Empty bounds mean "all time" and pick the unbounded compile-time variant of
// the query — the suffix is a constant, never a spliced value. Stored _at
// columns are fixed-width RFC3339 UTC text, so a plain string comparison is a
// chronological one and no date function is needed (the SQLite/Postgres rule).

// playsInPeriod is the bounded variant's suffix for user_watch_history rows.
const playsInPeriod = `
  AND wh.watched_at >= ? AND wh.watched_at < ?`

// ratingsInPeriod is the bounded variant's suffix for user_video_ratings rows:
// a rating belongs to the period its updated_at falls in — a re-rating moves
// the opinion to when it was last given, since no rating history exists.
const ratingsInPeriod = `
  AND vr.updated_at >= ? AND vr.updated_at < ?`

// statsBounds is what the period defaults and the offered years need: the
// user's first play, their first rating (either nil without rows), and whether
// the window [curFrom, curUntil) — the current year in their zone — holds a
// play. Ratings deliberately do not decide the default: a year with ratings but
// no plays would open on zeros in every play-based section.
func statsBounds(ctx context.Context, d *db.DB, userID int64, curFrom, curUntil string) (firstPlay, firstRating *string, playsThisYear bool, err error) {
	const q = `
SELECT (SELECT MIN(wh.watched_at) FROM user_watch_history wh WHERE wh.user_id = ?),
       (SELECT MIN(vr.updated_at) FROM user_video_ratings vr WHERE vr.user_id = ?),
       EXISTS (SELECT 1 FROM user_watch_history wh
               WHERE wh.user_id = ? AND wh.watched_at >= ? AND wh.watched_at < ?)`
	err = d.ReadRow(ctx, q, userID, userID, userID, curFrom, curUntil).Scan(&firstPlay, &firstRating, &playsThisYear)
	return firstPlay, firstRating, playsThisYear, err
}

// playRow is one play (one user_watch_history row) with the catalog facts the
// play-based sections need carried along, so the whole page is one bulk read
// and the bucketing happens in Go. The movie columns are nil for an episode
// play and the episode/show columns nil for a movie play; TargetType says
// which arm applies, because movie 5 and episode 5 both exist (D-013).
type playRow struct {
	ID         int64
	TargetType string // movie | episode
	TargetID   int64
	WatchedAt  string // RFC3339 UTC

	MovieRuntime *int
	MovieRelease *string // YYYY-MM-DD
	MovieGenres  *string // JSON array of names

	ShowID         *int64
	Season         *int
	Number         *int
	EpisodeRuntime *int
	ShowFirstAir   *string // YYYY-MM-DD
	ShowGenres     *string // JSON array of names
}

// listPlays is THE bulk read: every play of this user in the period, oldest
// first (ties on the instant broken by id, so "first play" and "most recent
// play" are a first-wins / last-wins loop in Go). The log is the driving table
// and each catalog join is guarded by the row's target_type, so the arms can
// never cross; a play whose target row is gone (neither join matched) is
// dropped before anything counts, the watchlist and history feeds' own rule.
// The joins go FROM the log TO the catalog — the fan-out hazard is the other
// direction, and no query here ever joins the log onto something else.
func listPlays(ctx context.Context, d *db.DB, userID int64, from, until string) ([]playRow, error) {
	const base = `
SELECT wh.id, wh.target_type, wh.target_id, wh.watched_at,
       m.runtime_min, m.release_date, m.genres,
       e.show_id, e.season_number, e.episode_number, e.runtime_min,
       sh.first_air_date, sh.genres
FROM user_watch_history wh
LEFT JOIN movies m   ON wh.target_type = 'movie'   AND m.id = wh.target_id
LEFT JOIN episodes e ON wh.target_type = 'episode' AND e.id = wh.target_id
LEFT JOIN shows sh   ON sh.id = e.show_id
WHERE wh.user_id = ?
  AND (m.id IS NOT NULL OR e.id IS NOT NULL)`
	const order = `
ORDER BY wh.watched_at, wh.id`
	const (
		all    = base + order
		period = base + playsInPeriod + order
	)

	q, args := all, []any{userID}
	if from != "" {
		q, args = period, []any{userID, from, until}
	}
	rows, err := d.Read(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []playRow
	for rows.Next() {
		var r playRow
		if err := rows.Scan(&r.ID, &r.TargetType, &r.TargetID, &r.WatchedAt,
			&r.MovieRuntime, &r.MovieRelease, &r.MovieGenres,
			&r.ShowID, &r.Season, &r.Number, &r.EpisodeRuntime,
			&r.ShowFirstAir, &r.ShowGenres); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// showDoc is a show's stored TMDB body, read only when the show's regular
// episode runtime is needed as a fallback.
type showDoc struct {
	ShowID       int64
	MetadataJSON string
}

// listShowRuntimeFallbacks returns the stored body of every show that has, in
// the period, a played episode WITHOUT a runtime of its own — exactly the shows
// whose regular runtime (TMDB's episode_run_time, which lives only in the body)
// the headline needs. A show whose played episodes all carry a runtime is not
// read, so the heavy column is fetched only where it changes the answer.
func listShowRuntimeFallbacks(ctx context.Context, d *db.DB, userID int64, from, until string) ([]showDoc, error) {
	const base = `
SELECT sh.id, sh.metadata_json
FROM shows sh
WHERE EXISTS (SELECT 1 FROM user_watch_history wh
              JOIN episodes e ON e.id = wh.target_id
              WHERE wh.user_id = ? AND wh.target_type = 'episode'
                AND e.show_id = sh.id AND e.runtime_min IS NULL`
	const (
		all    = base + `)`
		period = base + playsInPeriod + `)`
	)

	q, args := all, []any{userID}
	if from != "" {
		q, args = period, []any{userID, from, until}
	}
	rows, err := d.Read(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []showDoc
	for rows.Next() {
		var r showDoc
		if err := rows.Scan(&r.ShowID, &r.MetadataJSON); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ratingRow is one of this user's ratings: the target pair, the value and when
// it was (last) given.
type ratingRow struct {
	TargetType string // movie | show | episode
	TargetID   int64
	Rating     int
	UpdatedAt  string // RFC3339 UTC
}

// listRatings returns this user's movie, show and episode ratings in the
// period, oldest first. A season row is left out even though the CHECK admits
// the value: seasons are not rateable (D-020), so none exists, and one that
// somehow did has no section to land in. Each arm carries an EXISTS on its
// catalog table so a rating whose target row is gone is dropped, the same
// dangling-row guard the list pages apply.
func listRatings(ctx context.Context, d *db.DB, userID int64, from, until string) ([]ratingRow, error) {
	const base = `
SELECT vr.target_type, vr.target_id, vr.rating, vr.updated_at
FROM user_video_ratings vr
WHERE vr.user_id = ?
  AND ((vr.target_type = 'movie'
        AND EXISTS (SELECT 1 FROM movies m WHERE m.id = vr.target_id))
    OR (vr.target_type = 'show'
        AND EXISTS (SELECT 1 FROM shows sh WHERE sh.id = vr.target_id))
    OR (vr.target_type = 'episode'
        AND EXISTS (SELECT 1 FROM episodes e WHERE e.id = vr.target_id)))`
	const order = `
ORDER BY vr.updated_at, vr.target_type, vr.target_id`
	const (
		all    = base + order
		period = base + ratingsInPeriod + order
	)

	q, args := all, []any{userID}
	if from != "" {
		q, args = period, []any{userID, from, until}
	}
	rows, err := d.Read(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ratingRow
	for rows.Next() {
		var r ratingRow
		if err := rows.Scan(&r.TargetType, &r.TargetID, &r.Rating, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// titleDoc is a rated movie's or show's stored TMDB body, keyed by kind and
// local id.
type titleDoc struct {
	Kind         string // movie | show
	ID           int64
	MetadataJSON string
}

// listRatedSnapshots returns the stored body of every movie and show this user
// rated in the period — the crowd comparison's only input beyond the ratings
// themselves, since TMDB's vote average and count live in the body alone and no
// portable SQL reads JSON. It is a separate read from listRatings on purpose:
// the ratings sections that need no document never pay for one.
func listRatedSnapshots(ctx context.Context, d *db.DB, userID int64, from, until string) ([]titleDoc, error) {
	const movies = `
SELECT 'movie' AS kind, m.id, m.metadata_json
FROM movies m
WHERE EXISTS (SELECT 1 FROM user_video_ratings vr
              WHERE vr.user_id = ? AND vr.target_type = 'movie' AND vr.target_id = m.id`
	const shows = `
SELECT 'show' AS kind, sh.id, sh.metadata_json
FROM shows sh
WHERE EXISTS (SELECT 1 FROM user_video_ratings vr
              WHERE vr.user_id = ? AND vr.target_type = 'show' AND vr.target_id = sh.id`
	const (
		all = movies + `)
UNION ALL` + shows + `)`
		period = movies + ratingsInPeriod + `)
UNION ALL` + shows + ratingsInPeriod + `)`
	)

	q, args := all, []any{userID, userID}
	if from != "" {
		q, args = period, []any{userID, from, until, userID, from, until}
	}
	rows, err := d.Read(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []titleDoc
	for rows.Next() {
		var r titleDoc
		if err := rows.Scan(&r.Kind, &r.ID, &r.MetadataJSON); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// completionRow is one followed show's derived progress: how many of its
// aired, non-special episodes exist and how many of those this user watched —
// ShowProgress's first two figures (never stored, D-010).
type completionRow struct {
	ShowID  int64
	Aired   int
	Watched int
}

// listCompletion returns the progress of every show this user follows and has
// not dropped — every non-dropped user_show_states row, which exists iff the
// log holds an episode watch of the show (D-010). It is driven from the state
// table rather than from episodes so a followed show with no aired non-special
// episode still counts as tracked (aired 0, in progress), and the aired
// predicate is D-012's EligibleEpisode spliced verbatim, so these counts are
// exactly ShowProgress's for every show in one round trip. Watched counts
// episodes with an EXISTS on the log, so a rewatch can never exceed aired.
func listCompletion(ctx context.Context, d *db.DB, userID int64, today string) ([]completionRow, error) {
	const q = `
SELECT ss.show_id,
       (SELECT COUNT(*) FROM episodes e
        WHERE e.show_id = ss.show_id AND ` + common.EligibleEpisode + `),
       (SELECT COUNT(*) FROM episodes e
        WHERE e.show_id = ss.show_id AND ` + common.EligibleEpisode + `
          AND EXISTS (SELECT 1 FROM user_watch_history wh
                      WHERE wh.user_id = ? AND wh.target_type = 'episode'
                        AND wh.target_id = e.id))
FROM user_show_states ss
WHERE ss.user_id = ? AND ss.status <> 'dropped'
ORDER BY ss.show_id`

	rows, err := d.Read(ctx, q, today, today, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []completionRow
	for rows.Next() {
		var r completionRow
		if err := rows.Scan(&r.ShowID, &r.Aired, &r.Watched); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
