package common

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/tz"
)

// ShowRow mirrors the shows catalog table. Nullable columns are pointers.
// Deliberately few columns: MetadataJSON is the verbatim /tv/{id} body with its
// appended sections, and everything the DTO renders beyond these is parsed back
// out of it — the same arrangement the movies table uses.
type ShowRow struct {
	ID           int64
	TMDBID       int64
	Title        string
	FirstAirDate *string
	Status       *string // TMDB's PRODUCTION status, never a watch status
	Overview     *string
	GenresJSON   *string
	PosterPath   *string
	BackdropPath *string
	MetadataJSON string
	FetchedAt    string
}

const ShowColumns = `id, tmdb_id, title, first_air_date, status, overview,
genres, poster_path, backdrop_path, metadata_json, fetched_at`

// ScanShowInto reads ShowColumns, in order, off s — the one Scan behind the
// field list whichever of *sql.Row or *sql.Rows a query returns (rowScanner is
// the movie store's).
func ScanShowInto(s rowScanner, sh *ShowRow) error {
	return s.Scan(&sh.ID, &sh.TMDBID, &sh.Title, &sh.FirstAirDate, &sh.Status,
		&sh.Overview, &sh.GenresJSON, &sh.PosterPath, &sh.BackdropPath,
		&sh.MetadataJSON, &sh.FetchedAt)
}

func scanShow(row *sql.Row) (ShowRow, error) {
	var sh ShowRow
	err := ScanShowInto(row, &sh)
	if errors.Is(err, sql.ErrNoRows) {
		return ShowRow{}, fmt.Errorf("%w: no such show", httpx.ErrNotFound)
	}
	if err != nil {
		return ShowRow{}, err
	}
	return sh, nil
}

func GetShowByID(ctx context.Context, d *db.DB, id int64) (ShowRow, error) {
	const q = `SELECT ` + ShowColumns + ` FROM shows WHERE id = ?`
	return scanShow(d.ReadRow(ctx, q, id))
}

func GetShowByTMDBID(ctx context.Context, d *db.DB, tmdbID int64) (ShowRow, error) {
	const q = `SELECT ` + ShowColumns + ` FROM shows WHERE tmdb_id = ?`
	return scanShow(d.ReadRow(ctx, q, tmdbID))
}

// UpsertShowByTMDBID inserts a show snapshot or, when the tmdb_id already
// exists, replaces it wholesale — the movies table's arrangement exactly. One
// statement covers the first materialise, a staleness refresh, and two
// concurrent first requests racing.
func UpsertShowByTMDBID(ctx context.Context, tx db.Tx, sh ShowRow) (ShowRow, error) {
	const q = `
INSERT INTO shows (tmdb_id, title, first_air_date, status, overview, genres,
                   poster_path, backdrop_path, metadata_json, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (tmdb_id) DO UPDATE SET
    title          = excluded.title,
    first_air_date = excluded.first_air_date,
    status         = excluded.status,
    overview       = excluded.overview,
    genres         = excluded.genres,
    poster_path    = excluded.poster_path,
    backdrop_path  = excluded.backdrop_path,
    metadata_json  = excluded.metadata_json,
    fetched_at     = excluded.fetched_at
RETURNING ` + ShowColumns
	return scanShow(tx.QueryRow(ctx, q, sh.TMDBID, sh.Title, sh.FirstAirDate,
		sh.Status, sh.Overview, sh.GenresJSON, sh.PosterPath, sh.BackdropPath,
		sh.MetadataJSON, sh.FetchedAt))
}

// ShowReferenced reports whether ANY user references the show — D-008's
// reference set, which for a show is TEN ways, not four: a state row, a
// watchlist row, a favorite row (D-030), a watch on ANY of its episodes, a
// rating on the show, on ANY of its seasons, or on ANY of its episodes, or an
// owned copy on the show, on ANY of its seasons, or on ANY of its episodes
// (D-021). A rating, a watchlist row, a favorite or a copy alone counts.
//
// Missing one arm here deletes artwork somebody still tracks, which is why the
// arms that reach through seasons and episodes are spelled out rather than
// folded together. The show- and episode-target copy arms are defensive, like
// the season-rating arm: unwritable today, but the CHECK admits the values, so
// the count must not depend on the service layer staying that way. Every
// polymorphic predicate carries its target_type (D-013): show 5, season 5 and
// episode 5 all exist.
func ShowReferenced(ctx context.Context, d *db.DB, showID int64) (bool, error) {
	const q = `
SELECT EXISTS (SELECT 1 FROM user_show_states     WHERE show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_watchlist WHERE target_type = 'show' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_favorites WHERE target_type = 'show' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_ratings   WHERE target_type = 'show' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_watch_history wh
               JOIN episodes e ON e.id = wh.target_id
               WHERE wh.target_type = 'episode' AND e.show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_ratings vr
               JOIN episodes e ON e.id = vr.target_id
               WHERE vr.target_type = 'episode' AND e.show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_ratings vr
               JOIN seasons se ON se.id = vr.target_id
               WHERE vr.target_type = 'season' AND se.show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_copies    WHERE target_type = 'show' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_copies vc
               JOIN seasons se ON se.id = vc.target_id
               WHERE vc.target_type = 'season' AND se.show_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_copies vc
               JOIN episodes e ON e.id = vc.target_id
               WHERE vc.target_type = 'episode' AND e.show_id = ?)`
	var referenced bool
	err := d.ReadRow(ctx, q, showID, showID, showID, showID, showID, showID,
		showID, showID, showID, showID).Scan(&referenced)
	return referenced, err
}

// userShowState is one user's whole private state for one show — the truthful
// initial state of every control on the show details page.
type userShowState struct {
	Status      *string // watching | watched | dropped; nil when untracked
	Watchlisted bool
	Favorited   bool
	Rating      *int // this user's rating, 1-10; nil when unrated
	// This user's EPISODE ratings across the whole show folded into one mean,
	// and how many went into it — the show page's companion read under the
	// show's own rating. Computed here, not summed client-side from the
	// per-season means: those would weight seasons equally, not episodes.
	EpisodeRatingAvg *float64 // nil when no episode of the show is rated
	RatedEpisodes    int
}

// UserShowFlags returns this user's state for the show in one round trip.
// Scalar subqueries only at the top level, never a join: user_watch_history
// fans out. The two rating aggregates join episodes INSIDE their subqueries,
// which is safe — an episode id is unique, and the ratings table's primary
// key holds one row per (user, target), so neither side can multiply.
func UserShowFlags(ctx context.Context, d *db.DB, userID, showID int64) (userShowState, error) {
	const q = `
SELECT (SELECT status FROM user_show_states
        WHERE user_id = ? AND show_id = ?),
       EXISTS (SELECT 1 FROM user_video_watchlist
               WHERE user_id = ? AND target_type = 'show' AND target_id = ?),
       EXISTS (SELECT 1 FROM user_video_favorites
               WHERE user_id = ? AND target_type = 'show' AND target_id = ?),
       (SELECT rating FROM user_video_ratings
        WHERE user_id = ? AND target_type = 'show' AND target_id = ?),
       (SELECT AVG(vr.rating) FROM user_video_ratings vr
        JOIN episodes e ON e.id = vr.target_id
        WHERE vr.user_id = ? AND vr.target_type = 'episode' AND e.show_id = ?),
       (SELECT COUNT(*) FROM user_video_ratings vr
        JOIN episodes e ON e.id = vr.target_id
        WHERE vr.user_id = ? AND vr.target_type = 'episode' AND e.show_id = ?)`
	var st userShowState
	err := d.ReadRow(ctx, q, userID, showID, userID, showID, userID, showID, userID, showID, userID, showID, userID, showID).
		Scan(&st.Status, &st.Watchlisted, &st.Favorited, &st.Rating, &st.EpisodeRatingAvg, &st.RatedEpisodes)
	return st, err
}

// ShowDroppedFor reports whether this user has dropped the show — the one piece
// of show-level state the season and episode views borrow (D-010).
//
// Narrower than UserShowFlags on purpose. That function answers "every control
// on the show details page" and hands back the watchlist flag and the show
// rating too; a season-shaped view renders neither, and fetching them would
// make rendering show-level state there a one-liner rather than a decision. The
// predicate also keeps the comparison to 'dropped' in SQL, where every other
// use of that value lives.
func ShowDroppedFor(ctx context.Context, d *db.DB, userID, showID int64) (bool, error) {
	const q = `
SELECT EXISTS (SELECT 1 FROM user_show_states
               WHERE user_id = ? AND show_id = ? AND status = 'dropped')`
	var dropped bool
	err := d.ReadRow(ctx, q, userID, showID).Scan(&dropped)
	return dropped, err
}

// ShowProgressCounts is a show's derived progress. Never stored (D-010) — these
// three numbers are recomputed from episodes + user_watch_history every time.
type ShowProgressCounts struct {
	Watched    int `json:"watched"`     // aired, non-special, watched by this user
	AiredTotal int `json:"aired_total"` // aired, non-special
	Total      int `json:"total"`       // non-special, aired or not
}

// ShowProgress counts NON-SPECIAL episodes only, in all three figures, and
// counts episodes rather than watch rows — SUM(CASE …) over episodes with an
// EXISTS on the log, so a rewatch can never push Watched past Total. That makes
// `Watched == AiredTotal AND AiredTotal > 0` exactly equivalent to
// RecomputeShowStatus promoting to 'watched': the invariant expressed as
// integers, which is what the status tests assert against.
//
// An episode marked before it airs is a real watch, appears in history, and
// correctly does not count here until its air date passes.
func ShowProgress(ctx context.Context, d *db.DB, userID, showID int64, today string) (ShowProgressCounts, error) {
	const q = `
SELECT COUNT(*),
       SUM(CASE WHEN e.air_date IS NOT NULL AND e.air_date <= ? THEN 1 ELSE 0 END),
       SUM(CASE WHEN e.air_date IS NOT NULL AND e.air_date <= ?
                 AND EXISTS (SELECT 1 FROM user_watch_history wh
                             WHERE wh.user_id = ? AND wh.target_type = 'episode'
                               AND wh.target_id = e.id)
                THEN 1 ELSE 0 END)
FROM episodes e
WHERE e.show_id = ? AND e.season_number > 0`
	// COUNT is never NULL, but both SUMs are when no row matches the WHERE —
	// a show with only specials — so they scan through nullable ints.
	var p ShowProgressCounts
	var aired, watched *int
	err := d.ReadRow(ctx, q, today, today, userID, showID).Scan(&p.Total, &aired, &watched)
	if err != nil {
		return ShowProgressCounts{}, err
	}
	if aired != nil {
		p.AiredTotal = *aired
	}
	if watched != nil {
		p.Watched = *watched
	}
	return p, nil
}

// SeasonUserStats is one season's slice of this user's state on the show page's
// grid: the season's derived progress, and the average of the user's EPISODE
// ratings in it — a derived read, not a season rating (D-020 refuses those).
type SeasonUserStats struct {
	Progress  ShowProgressCounts
	RatingAvg *float64 // nil when no episode of the season is rated
}

// SeasonUserStatsByShow returns every season's stats in one round trip, keyed
// by season number like seasonCopiesByShow — the DTO's seasons are the
// snapshot's array, addressed by number. It is ShowProgress's three SUM(CASE …)
// arms grouped per season, so Specials get their OWN count here: this is
// display, not D-012's completion rule, and a season has no status to gate.
// The ratings are LEFT JOINed: their primary key is (user, target_type,
// target_id), so the join is at most one row per episode and cannot fan out
// the way the watch log would (that one stays an EXISTS). AVG ignores NULLs and
// is NULL when nothing is rated. A show with no episode rows yields an empty
// map, never an error.
func SeasonUserStatsByShow(ctx context.Context, d *db.DB, userID, showID int64, today string) (map[int]SeasonUserStats, error) {
	const q = `
SELECT e.season_number,
       COUNT(*),
       SUM(CASE WHEN e.air_date IS NOT NULL AND e.air_date <= ? THEN 1 ELSE 0 END),
       SUM(CASE WHEN e.air_date IS NOT NULL AND e.air_date <= ?
                 AND EXISTS (SELECT 1 FROM user_watch_history wh
                             WHERE wh.user_id = ? AND wh.target_type = 'episode'
                               AND wh.target_id = e.id)
                THEN 1 ELSE 0 END),
       AVG(vr.rating)
FROM episodes e
LEFT JOIN user_video_ratings vr
  ON vr.user_id = ? AND vr.target_type = 'episode' AND vr.target_id = e.id
WHERE e.show_id = ?
GROUP BY e.season_number`
	rows, err := d.Read(ctx, q, today, today, userID, userID, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := make(map[int]SeasonUserStats)
	for rows.Next() {
		var number int
		var st SeasonUserStats
		// A group always has rows, so the SUMs are never NULL here — but they
		// scan through nullable ints anyway, matching ShowProgress.
		var aired, watched *int
		if err := rows.Scan(&number, &st.Progress.Total, &aired, &watched, &st.RatingAvg); err != nil {
			return nil, err
		}
		if aired != nil {
			st.Progress.AiredTotal = *aired
		}
		if watched != nil {
			st.Progress.Watched = *watched
		}
		stats[number] = st
	}
	return stats, rows.Err()
}

// BulkMarkShowWatched records a watch for every aired, non-special episode of
// the show — or, with seasonNumber set, of that one season: SPEC §6's "mark
// whole show watched" and "mark whole season watched", each specified as a
// single API call and not a loop of per-episode requests. "Aired" is
// EligibleEpisode against clk.Today, the user's day (D-038).
//
// The NOT EXISTS is insertWatchIfNever's rule applied set-wise, and it is the
// only thing standing between a double tap and two hundred duplicate rows.
// rewatch=true drops it on purpose — D-009's deliberate rewatch applied
// set-wise, guarded by the client's awaited pending state exactly like the
// single dated log.
//
// mode picks each row's watched_at. now/fixed share the one instant in `at`
// (which idx_user_watch_history_feed's (watched_at, id) tie-break was built
// for) and stay ONE INSERT … SELECT. episode_air takes each episode's own air
// date and season_air its season's premiere, both as MIDNIGHT IN THE USER'S
// ZONE stored as its UTC instant — which no portable SQL can compute, because
// the offset of a zone's midnight varies by date (DST). So those two modes
// select the eligible rows under the same predicates and insert them one by
// one in the caller's transaction; tz.Midnight emits the same fixed-width
// RFC3339 every _at column holds, so the feed's plain-text ordering stands.
// Neither instant can be in the future: an eligible episode aired on or before
// the user's today, and season_air only takes a premiere that did too. For
// season_air, `at` is the fallback for a missing or not-yet-aired premiere;
// the caller pre-checks whether a real fallback is needed via
// CountEligibleWithoutSeasonDate.
//
// Returns how many rows were actually written.
func BulkMarkShowWatched(ctx context.Context, tx db.Tx, userID int64, show ShowRow, seasonNumber *int, mode, at string, clk Clock, rewatch bool) (int64, error) {
	if mode != "episode_air" && mode != "season_air" {
		return bulkInsertSharedInstant(ctx, tx, userID, show, seasonNumber, at, clk.Today, rewatch)
	}
	rows, err := bulkEligibleEpisodes(ctx, tx, userID, show, seasonNumber, clk.Today, rewatch)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		watchedAt := at
		day := r.airDate
		if mode == "season_air" {
			day = r.premiere.String
		}
		if mode == "episode_air" || r.premiere.Valid {
			if watchedAt, err = tz.Midnight(day, clk.Zone); err != nil {
				return 0, fmt.Errorf("episode %d: air date %q: %w", r.id, day, err)
			}
		}
		if err := InsertWatch(ctx, tx, userID, "episode", r.id, watchedAt, "watch"); err != nil {
			return 0, err
		}
	}
	return int64(len(rows)), nil
}

// bulkInsertSharedInstant is the now/fixed arm: every eligible episode gets
// the same instant, in one statement. The SQL and its args are assembled
// together so placeholder order cannot drift; textual order is SELECT list →
// WHERE (show, then the optional season, then EligibleEpisode's today) → NOT
// EXISTS.
func bulkInsertSharedInstant(ctx context.Context, tx db.Tx, userID int64, show ShowRow, seasonNumber *int, at, today string, rewatch bool) (int64, error) {
	q := `
INSERT INTO user_watch_history (user_id, target_type, target_id, watched_at, watch_type)
SELECT ?, 'episode', e.id, ?, 'watch'
FROM episodes e
WHERE e.show_id = ?`
	args := []any{userID, at, show.ID}
	if seasonNumber != nil {
		q += `
  AND e.season_number = ?`
		args = append(args, *seasonNumber)
	}
	q += `
  AND ` + EligibleEpisode
	args = append(args, today)
	if !rewatch {
		q += `
  AND NOT EXISTS (
      SELECT 1 FROM user_watch_history wh
      WHERE wh.user_id = ? AND wh.target_type = 'episode' AND wh.target_id = e.id)`
		args = append(args, userID)
	}
	res, err := tx.Exec(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// bulkEpisode is one episode an air-date bulk mode will mark: its own air
// date (never NULL — EligibleEpisode guarantees it) and its season's premiere
// when that premiere has itself aired, else NULL (season_air's fallback case).
type bulkEpisode struct {
	id       int64
	airDate  string
	premiere sql.NullString
}

// bulkEligibleEpisodes selects the rows bulkInsertSharedInstant would insert,
// with the dates the air-date modes stamp them with. Same predicates, same
// order of args; the season join goes through show_tmdb_id, the table's
// natural key, which is 1:1 by UNIQUE(show_tmdb_id, season_number), so no
// fan-out. Ordered by position so the inserted ids follow the show.
func bulkEligibleEpisodes(ctx context.Context, tx db.Tx, userID int64, show ShowRow, seasonNumber *int, today string, rewatch bool) ([]bulkEpisode, error) {
	q := `
SELECT e.id, e.air_date, CASE WHEN s.air_date <= ? THEN s.air_date END
FROM episodes e
LEFT JOIN seasons s ON s.show_tmdb_id = ? AND s.season_number = e.season_number
WHERE e.show_id = ?`
	args := []any{today, show.TMDBID, show.ID}
	if seasonNumber != nil {
		q += `
  AND e.season_number = ?`
		args = append(args, *seasonNumber)
	}
	q += `
  AND ` + EligibleEpisode
	args = append(args, today)
	if !rewatch {
		q += `
  AND NOT EXISTS (
      SELECT 1 FROM user_watch_history wh
      WHERE wh.user_id = ? AND wh.target_type = 'episode' AND wh.target_id = e.id)`
		args = append(args, userID)
	}
	q += `
ORDER BY e.season_number, e.episode_number`
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bulkEpisode
	for rows.Next() {
		var r bulkEpisode
		if err := rows.Scan(&r.id, &r.airDate, &r.premiere); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountEligibleWithoutSeasonDate counts the eligible episodes season_air could
// not date from their season — the premiere is missing, or is itself still in
// the future while the episode has somehow aired (provider dirt). seasonNumber
// narrows the check to one season, mirroring the bulk insert's own filter. The
// service turns a non-zero count with no fallback into a validation error; the
// store stays error-code-agnostic, matching deleteWatchByID's found-bool shape.
func CountEligibleWithoutSeasonDate(ctx context.Context, tx db.Tx, showTMDBID, showID int64, seasonNumber *int, today string) (int64, error) {
	q := `
SELECT COUNT(*)
FROM episodes e
LEFT JOIN seasons s ON s.show_tmdb_id = ? AND s.season_number = e.season_number
WHERE e.show_id = ?`
	args := []any{showTMDBID, showID}
	if seasonNumber != nil {
		q += `
  AND e.season_number = ?`
		args = append(args, *seasonNumber)
	}
	q += `
  AND ` + EligibleEpisode + `
  AND (s.air_date IS NULL OR s.air_date > ?)`
	args = append(args, today, today)
	var n int64
	err := tx.QueryRow(ctx, q, args...).Scan(&n)
	return n, err
}
