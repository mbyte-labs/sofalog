package history

import (
	"context"
	"math"

	"mbyte.ro/sofalog/db"
)

// watchRow is one user_watch_history row, with this user's current rating of
// the target resolved alongside. The rating belongs to the target, not the
// watch, so every row of one target carries the same value — which is exactly
// what the history feed renders.
type watchRow struct {
	ID         int64
	WatchedAt  string
	WatchType  string
	TargetType string // movie | episode
	TargetID   int64  // movies.id or episodes.id — meaningless without TargetType
	UserRating *int
}

// watchFilter is one page of the feed. Type narrows it to one kind of target;
// Limit <= 0 means unlimited.
//
// Three MUTUALLY EXCLUSIVE narrowings, and the service refuses more than one
// rather than letting a silent winner emerge:
//   - TargetType/TargetID — a single item's watches, the pair identifying it.
//   - SeasonShowTMDBID/SeasonNumber — one season's episodes.
//   - ShowID — one show's episodes, across every season.
//
// The last two are SCOPES rather than targets: neither a season nor a show is
// ever itself watched, which is why neither is a TargetType value.
type watchFilter struct {
	UserID     int64
	Type       string // "" | movie | episode
	TargetType string // "" unless narrowing to one item
	TargetID   int64
	// SeasonNumber is a pointer, and not for nullability: 0 is Specials, a real
	// season, so a zero value cannot mean "no season filter". A show id has no
	// such problem, so ShowID is a plain id with a > 0 guard.
	SeasonShowTMDBID int64
	SeasonNumber     *int
	ShowID           int64
	Desc             bool
	Limit            int
	Offset           int
}

// listWatchPage returns one ordered page of this user's watches. It serves the
// whole feed, the type-filtered feed, the per-item feed and the season- and
// show-scoped feeds — the only differences are which predicates are appended —
// so ordering and paging have a single definition, in SQL.
//
// Every polymorphic predicate carries its target_type (D-013: movie 5 and
// episode 5 both exist). That includes the rating subquery, which correlates on
// wh.target_type rather than a literal — without that, a rating of movie 5
// would decorate a watch of episode 5. The subquery is a scalar subquery and
// not a join: user_watch_history fans out, so nothing may LEFT JOIN it or
// through it.
//
// The two EXISTS clauses drop watches whose catalog row is gone — target_id has
// no foreign key, so a dangling id is representable — and they do so before
// LIMIT, so a page is exactly as long as the rows it returns.
func listWatchPage(ctx context.Context, d *db.DB, f watchFilter) ([]watchRow, error) {
	// ORDER BY (watched_at, id) matches idx_user_watch_history_feed. Fixed-width
	// RFC3339 UTC compares chronologically as plain text on both engines, so no
	// date function is needed. The id tie-break is not decorative: a bulk mark
	// writes one watched_at to many rows.
	const base = `
SELECT wh.id, wh.watched_at, wh.watch_type, wh.target_type, wh.target_id,
       (SELECT vr.rating FROM user_video_ratings vr
        WHERE vr.user_id = ? AND vr.target_type = wh.target_type
          AND vr.target_id = wh.target_id)
FROM user_watch_history wh
WHERE wh.user_id = ?
  AND ((wh.target_type = 'movie'
        AND EXISTS (SELECT 1 FROM movies m WHERE m.id = wh.target_id))
    OR (wh.target_type = 'episode'
        AND EXISTS (SELECT 1 FROM episodes e WHERE e.id = wh.target_id)))`
	const oneType = `
  AND wh.target_type = ?`
	const oneTarget = `
  AND wh.target_type = ? AND wh.target_id = ?`
	// One season's episodes. EXISTS rather than a join into the FROM list, for
	// two independent reasons: user_watch_history fans out per rewatch (D-009),
	// and (seasons.show_id, season_number) is not unique — only
	// (show_tmdb_id, season_number) is — so a join could duplicate feed rows
	// twice over. It reaches the season through shows.tmdb_id because episodes
	// has no season_id at all and seasons.show_id is nullable by design; this
	// is EpisodeIDsBySeason's join, verbatim in shape.
	const oneSeason = `
  AND wh.target_type = 'episode'
  AND EXISTS (SELECT 1 FROM episodes e JOIN shows sh ON sh.id = e.show_id
              WHERE e.id = wh.target_id
                AND sh.tmdb_id = ? AND e.season_number = ?)`
	// One show's episodes, every season. Simpler than oneSeason because
	// episodes.show_id is NOT NULL with a real FK to shows(id) — no join and no
	// tmdb_id hop. Still EXISTS, for the same D-009 fan-out reason.
	const oneShow = `
  AND wh.target_type = 'episode'
  AND EXISTS (SELECT 1 FROM episodes e
              WHERE e.id = wh.target_id AND e.show_id = ?)`
	const orderAsc = `
ORDER BY wh.watched_at, wh.id`
	const orderDesc = `
ORDER BY wh.watched_at DESC, wh.id DESC`
	// Always emit both keywords and pass a sentinel count for the unlimited
	// case: SQLite requires LIMIT before OFFSET and rejects a bare OFFSET, while
	// Postgres rejects a negative LIMIT, so neither `LIMIT -1 OFFSET n` nor
	// `OFFSET n` alone runs on both. Both engines take a 64-bit LIMIT.
	const page = `
LIMIT ? OFFSET ?`

	q, args := base, []any{f.UserID, f.UserID}
	if f.Type != "" {
		q += oneType
		args = append(args, f.Type)
	}
	if f.TargetType != "" && f.TargetID > 0 {
		q += oneTarget
		args = append(args, f.TargetType, f.TargetID)
	}
	if f.SeasonNumber != nil {
		q += oneSeason
		args = append(args, f.SeasonShowTMDBID, *f.SeasonNumber)
	}
	if f.ShowID > 0 {
		q += oneShow
		args = append(args, f.ShowID)
	}
	if f.Desc {
		q += orderDesc
	} else {
		q += orderAsc
	}
	limit := int64(f.Limit)
	if f.Limit <= 0 {
		limit = math.MaxInt64
	}
	q += page
	args = append(args, limit, f.Offset)

	rows, err := d.Read(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []watchRow
	for rows.Next() {
		var r watchRow
		if err := rows.Scan(&r.ID, &r.WatchedAt, &r.WatchType, &r.TargetType,
			&r.TargetID, &r.UserRating); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
