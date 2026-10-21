package common

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// UserTargetCopies lists the kinds this user holds one target on, ordered so
// the response is stable. Always a non-nil slice — the API serves [], never
// null.
func UserTargetCopies(ctx context.Context, d *db.DB, userID int64, targetType string, targetID int64) ([]string, error) {
	const q = `
SELECT kind FROM user_video_copies
WHERE user_id = ? AND target_type = ? AND target_id = ?
ORDER BY kind`
	rows, err := d.Read(ctx, q, userID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	kinds := make([]string, 0, 2)
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return nil, err
		}
		kinds = append(kinds, kind)
	}
	return kinds, rows.Err()
}

// seasonCopiesByShow maps a show's season numbers to the kinds this user holds
// that season on, for the show details view's per-season copies. Keyed by
// season_number like SeasonIDsByShow, and for the same reason: the seasons in
// a details response are the snapshot's, addressed positionally. The join
// carries target_type — the id alone identifies nothing (D-013). An untracked
// show has no season rows, so this returns an empty map.
func seasonCopiesByShow(ctx context.Context, d *db.DB, userID, showTMDBID int64) (map[int][]string, error) {
	const q = `
SELECT s.season_number, c.kind
FROM user_video_copies c
JOIN seasons s ON s.id = c.target_id
WHERE c.user_id = ? AND c.target_type = 'season' AND s.show_tmdb_id = ?
ORDER BY s.season_number, c.kind`
	rows, err := d.Read(ctx, q, userID, showTMDBID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	copies := make(map[int][]string)
	for rows.Next() {
		var number int
		var kind string
		if err := rows.Scan(&number, &kind); err != nil {
			return nil, err
		}
		copies[number] = append(copies[number], kind)
	}
	return copies, rows.Err()
}
