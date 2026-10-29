package refresh

import (
	"context"
	"database/sql"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
)

// The scheduled refresh's selection queries (D-025). Two classes, disjoint by
// construction: a watchlist row or a favorite row (D-030) always puts an item
// in the ACTIVE class, so the ARCHIVE arms all start with NOT EXISTS on both.
// Every arm reads what ANY user holds — one user dropping a show must not
// starve another's calendar.
//
// "Due" is decided by fetched_at alone: fixed-width RFC3339 UTC, so `< ?`
// against a Go-computed cutoff is a chronological comparison on both engines
// and no date function is needed (0001_init.sql's timestamp preamble).

// refreshClass names one of the two scheduled jobs. It selects a compile-time
// query string, never a fragment spliced into one.
type refreshClass string

const (
	// RefreshActive: what people are currently interested in — every
	// watchlisted or favorited movie and show, plus followed shows still in
	// production.
	RefreshActive refreshClass = "active"
	// RefreshArchive: what they have finished with — watched movies and
	// followed shows TMDB marks Ended or Canceled, neither watchlisted nor
	// favorited.
	RefreshArchive refreshClass = "archive"
)

// MovieRef is the pair a refresh needs: the local id for logging and the
// provider id for the fetch.
type MovieRef struct {
	ID     int64
	tmdbID int64
}

// DueMovies lists the movies of one class whose snapshot predates cutoff.
func DueMovies(ctx context.Context, d *db.DB, class refreshClass, cutoff string) ([]MovieRef, error) {
	const active = `
SELECT m.id, m.tmdb_id FROM movies m
WHERE m.fetched_at < ?
  AND (EXISTS (SELECT 1 FROM user_video_watchlist wl
               WHERE wl.target_type = 'movie' AND wl.target_id = m.id)
       OR EXISTS (SELECT 1 FROM user_video_favorites fv
                  WHERE fv.target_type = 'movie' AND fv.target_id = m.id))
ORDER BY m.id`
	const archive = `
SELECT m.id, m.tmdb_id FROM movies m
WHERE m.fetched_at < ?
  AND NOT EXISTS (SELECT 1 FROM user_video_watchlist wl
                  WHERE wl.target_type = 'movie' AND wl.target_id = m.id)
  AND NOT EXISTS (SELECT 1 FROM user_video_favorites fv
                  WHERE fv.target_type = 'movie' AND fv.target_id = m.id)
  AND EXISTS (SELECT 1 FROM user_watch_history wh
              WHERE wh.target_type = 'movie' AND wh.target_id = m.id)
ORDER BY m.id`
	q := active
	if class == RefreshArchive {
		q = archive
	}
	rows, err := d.Read(ctx, q, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MovieRef
	for rows.Next() {
		var m MovieRef
		if err := rows.Scan(&m.ID, &m.tmdbID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DueShows lists the shows of one class whose snapshot predates cutoff, as full
// rows: RefreshSeason takes the show, and the status column decides the class.
//
// shows.status is TMDB's PRODUCTION status. A NULL one counts as in production —
// an unknown show is refreshed on the faster cadence until TMDB says otherwise.
// A state row exists iff the log holds an episode watch or the user dropped the
// show (status.go), so "non-dropped state row" is "some follower still counts".
func DueShows(ctx context.Context, d *db.DB, class refreshClass, cutoff string) ([]common.ShowRow, error) {
	const active = `
SELECT ` + common.ShowColumns + ` FROM shows
WHERE shows.fetched_at < ?
  AND (EXISTS (SELECT 1 FROM user_video_watchlist wl
               WHERE wl.target_type = 'show' AND wl.target_id = shows.id)
       OR EXISTS (SELECT 1 FROM user_video_favorites fv
                  WHERE fv.target_type = 'show' AND fv.target_id = shows.id)
       OR (EXISTS (SELECT 1 FROM user_show_states ss
                   WHERE ss.show_id = shows.id AND ss.status <> 'dropped')
           AND (shows.status IS NULL OR shows.status NOT IN ('Ended', 'Canceled'))))
ORDER BY shows.id`
	const archive = `
SELECT ` + common.ShowColumns + ` FROM shows
WHERE shows.fetched_at < ?
  AND NOT EXISTS (SELECT 1 FROM user_video_watchlist wl
                  WHERE wl.target_type = 'show' AND wl.target_id = shows.id)
  AND NOT EXISTS (SELECT 1 FROM user_video_favorites fv
                  WHERE fv.target_type = 'show' AND fv.target_id = shows.id)
  AND EXISTS (SELECT 1 FROM user_show_states ss
              WHERE ss.show_id = shows.id AND ss.status <> 'dropped')
  AND shows.status IN ('Ended', 'Canceled')
ORDER BY shows.id`
	q := active
	if class == RefreshArchive {
		q = archive
	}
	rows, err := d.Read(ctx, q, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []common.ShowRow
	for rows.Next() {
		var sh common.ShowRow
		if err := common.ScanShowInto(rows, &sh); err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// dueOpenSeasons lists the season numbers of one show that are still "open" —
// where TMDB may yet add or re-date an episode — and whose own snapshot
// predates cutoff: the highest-numbered season, plus any non-special season
// holding an undated or future episode. today is YYYY-MM-DD, Go-computed.
//
// Specials are left out of the undated arm on purpose: an undated special is
// common and often permanent, and would make its season due every interval for
// nothing. A closed season (every episode dated and aired) is never re-fetched
// by the job; the season page's own staleness rule still covers it on view.
func dueOpenSeasons(ctx context.Context, d *db.DB, showID int64, cutoff, today string) ([]int, error) {
	const q = `
SELECT s.season_number FROM seasons s
WHERE s.show_id = ? AND s.fetched_at < ?
  AND (s.season_number = (SELECT MAX(s2.season_number) FROM seasons s2 WHERE s2.show_id = s.show_id)
       OR (s.season_number > 0 AND EXISTS (
             SELECT 1 FROM episodes e
             WHERE e.show_id = s.show_id AND e.season_number = s.season_number
               AND (e.air_date IS NULL OR e.air_date > ?))))
ORDER BY s.season_number`
	rows, err := d.Read(ctx, q, showID, cutoff, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// userShow is one (user, show) pair the status sweep recomputes.
type userShow struct {
	userID int64
	showID int64
	// zone is the user's chosen IANA zone (D-038), "" for the server
	// default — where this pair's "aired" ends.
	zone string
}

// showStatusTargets lists every (user, show) whose status the sweep should
// recompute: each non-dropped state row, UNIONed with each user holding an
// episode watch on the show. The second arm is a repair: ensure guarantees a
// state row for every episode watch, so it normally adds nothing — but if a row
// ever went missing, the sweep recreates it rather than leaving the show
// invisible. Dropped pairs are left out of both arms because every recompute
// statement is inert on them anyway (D-010) — the count the sweep reports is
// then the pairs it could have moved. Each pair carries its user's zone, read
// in the same statement, so the sweep needs no per-user lookup.
func showStatusTargets(ctx context.Context, d *db.DB) ([]userShow, error) {
	const q = `
SELECT t.user_id, t.show_id, u.timezone
FROM (
    SELECT user_id, show_id FROM user_show_states WHERE status <> 'dropped'
    UNION
    SELECT wh.user_id, e.show_id FROM user_watch_history wh
    JOIN episodes e ON e.id = wh.target_id
    WHERE wh.target_type = 'episode'
      AND NOT EXISTS (SELECT 1 FROM user_show_states ss
                      WHERE ss.user_id = wh.user_id AND ss.show_id = e.show_id
                        AND ss.status = 'dropped')
) t
JOIN users u ON u.id = t.user_id
ORDER BY t.user_id, t.show_id`
	rows, err := d.Read(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []userShow
	for rows.Next() {
		var t userShow
		var zone sql.NullString
		if err := rows.Scan(&t.userID, &t.showID, &zone); err != nil {
			return nil, err
		}
		t.zone = zone.String
		out = append(out, t)
	}
	return out, rows.Err()
}
