package upnext

import (
	"context"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
)

// upNextRow is one show's next-episode pick plus everything the card renders
// that lives in the catalog: titles, both poster paths (the service resolves
// season-falls-back-to-show), and the episode's identity and air date.
type upNextRow struct {
	ShowID           int64
	ShowTitle        string
	ShowPosterPath   *string
	SeasonPosterPath *string
	// SeasonID is the local seasons.id — the handle the episode details page
	// addresses this episode by, exactly as historyEpisodeRow carries it: an
	// episode has no id-addressed endpoint, so a link needs its season plus
	// its number. An episode row is only ever written alongside its season, in
	// the same transaction that upserts that season with a non-null show_id,
	// so in practice this is never null; it is scanned as a pointer anyway,
	// because a nullable target costs nothing and the card keeps rendering
	// (just not linking) if the season row ever goes away.
	SeasonID      *int64
	EpisodeID     int64
	SeasonNumber  int
	EpisodeNumber int
	EpisodeTitle  *string
	AirDate       *string // YYYY-MM-DD, or nil when undated
	LastWatchedAt string  // RFC3339 UTC; non-null by construction (see below)
}

// upNextFilter carries the validated query parameters. OrderBy is one of
// last_watched | show_name | air_date — the service validates before the value
// gets anywhere near this file, and the switch below maps it onto compile-time
// SQL constants, so the requested key never reaches the query as text.
type upNextFilter struct {
	UserID              int64
	OrderBy             string
	Desc                bool
	OnlyWithReleaseDate bool
	OnlyReleased        bool
	// Today is YYYY-MM-DD, Go-computed; consumed only by OnlyReleased.
	Today string
}

// listUpNext returns one row per started show: the first unwatched non-special
// episode STRICTLY AFTER the user's highest watched position. Watched episode
// 2 → episode 3 is next, never episode 1; a finished show (nothing after the
// highest watched) returns no row rather than wrapping around.
//
// "Started" is defined by the log alone — the aggregate derived table requires
// ≥1 episode watch, which is also what makes LastWatchedAt (MAX over those
// rows) non-null by construction. The log fans out (D-009), so it is grouped
// and probed with EXISTS, never LEFT JOINed.
//
// The pick deliberately does NOT require the episode to have aired: an
// announced or undated next episode is a real answer the card renders with
// aired=false, and only_released is the filter that removes it. That filter
// splices EligibleEpisode verbatim (D-012) and lands AFTER the pick by
// construction — a show whose next episode is unaired must yield nothing under
// only_released, never fall back to a later aired episode.
func listUpNext(ctx context.Context, d *db.DB, f upNextFilter) ([]upNextRow, error) {
	// The pick subquery names the minimum episode e2 such that no watched
	// non-special episode sits at a position >= e2 — identical to "first
	// episode strictly after the highest watched position" (every position
	// above the maximum watched is unwatched by definition), but expressible
	// without row-value comparisons, which SQLite only gained in 3.15. Watches
	// of specials (season 0) never anchor the pick: a user whose only watch is
	// a special is offered S1E1.
	//
	// The two season scalar subqueries — its poster and its id — both carry
	// ORDER BY … LIMIT 1 for portability: SQLite silently takes one row from a
	// scalar subquery that returns several, where Postgres raises. They
	// resolve the same season row; getHistoryEpisode pairs them identically.
	//
	// Dropped is a NOT EXISTS on the state row rather than a join: the log
	// defines the show set, and a missing state row must read as not-dropped.
	const base = `
SELECT sh.id, sh.title, sh.poster_path,
       (SELECT se.poster_path FROM seasons se
        WHERE se.show_id = sh.id AND se.season_number = e.season_number
        ORDER BY se.id LIMIT 1),
       (SELECT se.id FROM seasons se
        WHERE se.show_id = sh.id AND se.season_number = e.season_number
        ORDER BY se.id LIMIT 1),
       e.id, e.season_number, e.episode_number, e.title, e.air_date,
       lw.last_watched_at
FROM (SELECT we.show_id AS show_id, MAX(wh.watched_at) AS last_watched_at
      FROM user_watch_history wh
      JOIN episodes we ON we.id = wh.target_id
      WHERE wh.user_id = ? AND wh.target_type = 'episode'
      GROUP BY we.show_id) lw
JOIN shows sh ON sh.id = lw.show_id
JOIN episodes e ON e.show_id = sh.id
WHERE e.id = (SELECT e2.id FROM episodes e2
              WHERE e2.show_id = sh.id AND e2.season_number > 0
                AND NOT EXISTS (
                    SELECT 1 FROM episodes w
                    WHERE w.show_id = e2.show_id AND w.season_number > 0
                      AND (w.season_number > e2.season_number
                           OR (w.season_number = e2.season_number
                               AND w.episode_number >= e2.episode_number))
                      AND EXISTS (SELECT 1 FROM user_watch_history wh2
                                  WHERE wh2.user_id = ? AND wh2.target_type = 'episode'
                                    AND wh2.target_id = w.id))
              ORDER BY e2.season_number, e2.episode_number
              LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM user_show_states ss
                  WHERE ss.user_id = ? AND ss.show_id = sh.id
                    AND ss.status = 'dropped')`

	const withReleaseDate = `
  AND e.air_date IS NOT NULL`
	const released = `
  AND ` + common.EligibleEpisode

	// Compile-time ORDER BY constants, per the listWatchlistTargets rules:
	// LOWER() so the engines agree on text ordering, an `(x IS NULL)` prefix so
	// an undated episode lands last in both directions on both engines, and a
	// deterministic tie-break following the direction. lw.last_watched_at needs
	// no null guard — it is non-null by construction (MAX over ≥1 row).
	const (
		name = `LOWER(sh.title)`

		lastWatchedAsc = `
ORDER BY lw.last_watched_at, ` + name + `, sh.id`
		lastWatchedDesc = `
ORDER BY lw.last_watched_at DESC, ` + name + ` DESC, sh.id DESC`
		nameAsc = `
ORDER BY ` + name + `, sh.id`
		nameDesc = `
ORDER BY ` + name + ` DESC, sh.id DESC`
		airDateAsc = `
ORDER BY (e.air_date IS NULL), e.air_date, ` + name + `, sh.id`
		airDateDesc = `
ORDER BY (e.air_date IS NULL), e.air_date DESC, ` + name + ` DESC, sh.id DESC`
	)

	q := base
	args := []any{f.UserID, f.UserID, f.UserID}
	if f.OnlyWithReleaseDate {
		q += withReleaseDate
	}
	if f.OnlyReleased {
		q += released
		args = append(args, f.Today)
	}

	switch {
	case f.OrderBy == "show_name" && f.Desc:
		q += nameDesc
	case f.OrderBy == "show_name":
		q += nameAsc
	case f.OrderBy == "air_date" && f.Desc:
		q += airDateDesc
	case f.OrderBy == "air_date":
		q += airDateAsc
	case f.Desc:
		q += lastWatchedDesc
	default:
		q += lastWatchedAsc
	}

	rows, err := d.Read(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []upNextRow
	for rows.Next() {
		var r upNextRow
		if err := rows.Scan(&r.ShowID, &r.ShowTitle, &r.ShowPosterPath, &r.SeasonPosterPath,
			&r.SeasonID, &r.EpisodeID, &r.SeasonNumber, &r.EpisodeNumber, &r.EpisodeTitle,
			&r.AirDate, &r.LastWatchedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
