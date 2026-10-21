package upcoming

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// upcomingRow is one dated entry of the upcoming calendar, identity only: the
// service projects the catalog row behind it through the same helpers the
// history feed uses, so a poster or a title is resolved in exactly one place.
//
// ItemID is episodes.id when Kind is "episode" and movies.id when it is
// "movie" — meaningless without Kind, because movie 5 and episode 5 both exist
// (D-013). The three show columns are nil for a movie.
type upcomingRow struct {
	Kind          string // episode | movie
	Date          string // YYYY-MM-DD: the episode's air date or the movie's release date
	ShowID        *int64
	SeasonNumber  *int
	EpisodeNumber *int
	ItemID        int64
}

// upcomingFilter carries the validated parameters. Type is one of
// all | episode | movie and picks a compile-time query below; From and Until
// are YYYY-MM-DD strings computed in Go — From inclusive, Until exclusive.
type upcomingFilter struct {
	UserID int64
	Type   string
	From   string
	Until  string
}

// listUpcoming returns, in calendar order, every dated entry inside the window
// that this user is waiting for:
//
//   - an unwatched, dated, non-special episode of a show the user FOLLOWS (a
//     non-dropped user_show_states row — which exists iff the log holds an
//     episode watch, D-010) or has on their WATCHLIST;
//   - an unwatched movie on their watchlist whose release date is in the window.
//
// Dropped wins over the watchlist: a dropped show is excluded even when listed,
// exactly as listUpNext excludes it — the log defines the followed set, and a
// missing state row must read as not-dropped, so it is a NOT EXISTS rather than
// a join. The state row is unique per (user, show), which is why the follow arm
// is a plain EXISTS with the dropped exclusion carried once, outside the OR.
//
// This window is the COMPLEMENT of D-012's EligibleEpisode, and deliberately
// not that constant: it starts AT the day passed as From (today's unwatched
// episode is the most useful calendar entry, and the NOT EXISTS on the log is
// what makes it leave once marked), so the two predicates overlap by one day.
// Do not unify them — EligibleEpisode decides completion, this decides display.
// An undated episode has nowhere to sit on a calendar and is left out; Up Next
// surfaces it with its Upcoming badge instead.
//
// The two arms are one UNION ALL so ordering has a single definition, in SQL.
// A compound SELECT may only ORDER BY its result columns on both engines, so
// the case-folded title is emitted as sort_name rather than applied in the
// ORDER BY. Every arm's sort keys are non-null within that arm, so no nulls-last
// prefix is needed; show_id precedes season and number so two shows sharing a
// title never interleave (the SPA collapses a same-show run into one row).
func listUpcoming(ctx context.Context, d *db.DB, f upcomingFilter) ([]upcomingRow, error) {
	const episodes = `
SELECT 'episode' AS kind, e.air_date AS on_date, LOWER(sh.title) AS sort_name,
       sh.id AS show_id, e.season_number AS season_number,
       e.episode_number AS episode_number, e.id AS item_id
FROM episodes e
JOIN shows sh ON sh.id = e.show_id
WHERE e.season_number > 0
  AND e.air_date IS NOT NULL AND e.air_date >= ? AND e.air_date < ?
  AND (EXISTS (SELECT 1 FROM user_video_watchlist wl
               WHERE wl.user_id = ? AND wl.target_type = 'show' AND wl.target_id = sh.id)
       OR EXISTS (SELECT 1 FROM user_show_states ss
                  WHERE ss.user_id = ? AND ss.show_id = sh.id))
  AND NOT EXISTS (SELECT 1 FROM user_show_states ss
                  WHERE ss.user_id = ? AND ss.show_id = sh.id
                    AND ss.status = 'dropped')
  AND NOT EXISTS (SELECT 1 FROM user_watch_history wh
                  WHERE wh.user_id = ? AND wh.target_type = 'episode'
                    AND wh.target_id = e.id)`

	// The three NULL columns take the episode arm's types in the union; alone,
	// they are untyped nulls, which scan into a nil pointer on both engines.
	const movies = `
SELECT 'movie' AS kind, m.release_date AS on_date, LOWER(m.title) AS sort_name,
       NULL AS show_id, NULL AS season_number, NULL AS episode_number,
       m.id AS item_id
FROM movies m
WHERE m.release_date IS NOT NULL AND m.release_date >= ? AND m.release_date < ?
  AND EXISTS (SELECT 1 FROM user_video_watchlist wl
              WHERE wl.user_id = ? AND wl.target_type = 'movie' AND wl.target_id = m.id)
  AND NOT EXISTS (SELECT 1 FROM user_watch_history wh
                  WHERE wh.user_id = ? AND wh.target_type = 'movie'
                    AND wh.target_id = m.id)`

	const order = `
ORDER BY on_date, sort_name, kind, show_id, season_number, episode_number, item_id`

	const (
		both = episodes + `
UNION ALL` + movies + order
		episodesOnly = episodes + order
		moviesOnly   = movies + order
	)

	episodeArgs := []any{f.From, f.Until, f.UserID, f.UserID, f.UserID, f.UserID}
	movieArgs := []any{f.From, f.Until, f.UserID, f.UserID}

	var q string
	var args []any
	switch f.Type {
	case "episode":
		q, args = episodesOnly, episodeArgs
	case "movie":
		q, args = moviesOnly, movieArgs
	default:
		q, args = both, append(episodeArgs, movieArgs...)
	}

	rows, err := d.Read(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []upcomingRow
	for rows.Next() {
		var r upcomingRow
		var sortName string
		if err := rows.Scan(&r.Kind, &r.Date, &sortName, &r.ShowID, &r.SeasonNumber,
			&r.EpisodeNumber, &r.ItemID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
