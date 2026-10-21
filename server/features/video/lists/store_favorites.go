package lists

import (
	"context"
	"math"

	"mbyte.ro/sofalog/db"
)

// deleteFavorite removes the target from the user's favorites. Removing an
// absent row is a no-op, not an error — the operation is idempotent.
func deleteFavorite(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64) error {
	const q = `
DELETE FROM user_video_favorites
WHERE user_id = ? AND target_type = ? AND target_id = ?`
	_, err := tx.Exec(ctx, q, userID, targetType, targetID)
	return err
}

// favoritesFilter is one page of the favorites list: watchlistFilter without
// the matching-users intersection, which is a watchlist-only feature (D-028).
type favoritesFilter struct {
	UserID  int64
	OrderBy string // added | title | release_date
	Type    string // all | movie | show
	Desc    bool
	Limit   int
	Offset  int
}

// listFavoriteTargets returns one ordered page of this user's favorites,
// movies and shows interleaved. It is listWatchlistTargets over the favorites
// table, and every rule documented there holds here too: the guarded LEFT
// JOINs (safe because catalog tables match on a primary key at most once),
// the dangling-row guard BEFORE LIMIT, the compile-time ORDER BY constants
// with their (target_type, target_id) tie-break, LOWER() for the engines'
// collations, `(x IS NULL)` for nulls-last, and the LIMIT sentinel. The
// result is the watchlistTarget triple so the service projects an entry the
// same way for both lists.
func listFavoriteTargets(ctx context.Context, d *db.DB, f favoritesFilter) ([]watchlistTarget, error) {
	const base = `
SELECT fv.target_type, fv.target_id, fv.added_at
FROM user_video_favorites fv
LEFT JOIN movies m ON fv.target_type = 'movie' AND m.id = fv.target_id
LEFT JOIN shows sh ON fv.target_type = 'show'  AND sh.id = fv.target_id
WHERE fv.user_id = ? AND (m.id IS NOT NULL OR sh.id IS NOT NULL)`
	const oneType = `
  AND fv.target_type = ?`

	const (
		title = `LOWER(COALESCE(m.title, sh.title))`
		date  = `COALESCE(m.release_date, sh.first_air_date)`

		addedAsc = `
ORDER BY fv.added_at, fv.target_type, fv.target_id`
		addedDesc = `
ORDER BY fv.added_at DESC, fv.target_type DESC, fv.target_id DESC`
		titleAsc = `
ORDER BY ` + title + `, fv.target_type, fv.target_id`
		titleDesc = `
ORDER BY ` + title + ` DESC, fv.target_type DESC, fv.target_id DESC`
		releasedAsc = `
ORDER BY (` + date + ` IS NULL), ` + date + `, fv.target_type, fv.target_id`
		releasedDesc = `
ORDER BY (` + date + ` IS NULL), ` + date + ` DESC, fv.target_type DESC, fv.target_id DESC`
	)
	const page = `
LIMIT ? OFFSET ?`

	q := base
	args := []any{f.UserID}
	if f.Type == "movie" || f.Type == "show" {
		q += oneType
		args = append(args, f.Type)
	}

	switch {
	case f.OrderBy == "title" && f.Desc:
		q += titleDesc
	case f.OrderBy == "title":
		q += titleAsc
	case f.OrderBy == "release_date" && f.Desc:
		q += releasedDesc
	case f.OrderBy == "release_date":
		q += releasedAsc
	case f.Desc:
		q += addedDesc
	default:
		q += addedAsc
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

	var out []watchlistTarget
	for rows.Next() {
		var t watchlistTarget
		if err := rows.Scan(&t.TargetType, &t.TargetID, &t.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
