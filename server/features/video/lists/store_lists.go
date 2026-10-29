package lists

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// listCount is one (list, kind) cell of the Lists hub's counts: how many
// targets of one kind a user holds on one list.
type listCount struct {
	List       string // watchlist | favorites | dropped
	TargetType string // movie | show
	N          int
}

// countListTargets counts this user's watchlist and favorites rows per kind,
// and their dropped shows, in one round trip. Each arm carries its list
// query's joins and guards, so a count always agrees with what the list page
// would render: a target_id pointing at no catalog row is not an item. A kind
// with no rows is simply absent from the result.
func countListTargets(ctx context.Context, d *db.DB, userID int64) ([]listCount, error) {
	const q = `
SELECT 'watchlist', w.target_type, COUNT(*)
FROM user_video_watchlist w
LEFT JOIN movies m ON w.target_type = 'movie' AND m.id = w.target_id
LEFT JOIN shows sh ON w.target_type = 'show'  AND sh.id = w.target_id
WHERE w.user_id = ? AND (m.id IS NOT NULL OR sh.id IS NOT NULL)
GROUP BY w.target_type
UNION ALL
SELECT 'favorites', fv.target_type, COUNT(*)
FROM user_video_favorites fv
LEFT JOIN movies m ON fv.target_type = 'movie' AND m.id = fv.target_id
LEFT JOIN shows sh ON fv.target_type = 'show'  AND sh.id = fv.target_id
WHERE fv.user_id = ? AND (m.id IS NOT NULL OR sh.id IS NOT NULL)
GROUP BY fv.target_type
UNION ALL
SELECT 'dropped', 'show', COUNT(*)
FROM user_show_states st
JOIN shows sh ON sh.id = st.show_id
WHERE st.user_id = ? AND st.status = 'dropped'
GROUP BY st.status`
	rows, err := d.Read(ctx, q, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []listCount
	for rows.Next() {
		var c listCount
		if err := rows.Scan(&c.List, &c.TargetType, &c.N); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
