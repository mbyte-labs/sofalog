package common

import (
	"context"
	"database/sql"
	"errors"

	"mbyte.ro/sofalog/db"
)

// jellyfinEntryFor reports whether the last scan found the target on the
// server, and how many episode files (for a season; 0 for a movie). The
// predicate carries target_type — the id alone identifies nothing (D-013).
func jellyfinEntryFor(ctx context.Context, d *db.DB, targetType string, targetID int64) (present bool, episodes int, err error) {
	const q = `
SELECT episodes FROM jellyfin_library WHERE target_type = ? AND target_id = ?`
	err = d.ReadRow(ctx, q, targetType, targetID).Scan(&episodes)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	return true, episodes, nil
}

// jellyfinSeasonsByShow maps the season NUMBERS of a show the last scan found
// on the server to their episode-file counts — keyed like seasonCopiesByShow,
// for the same positional projection into the show details view.
func jellyfinSeasonsByShow(ctx context.Context, d *db.DB, showTMDBID int64) (map[int]int, error) {
	const q = `
SELECT s.season_number, j.episodes
FROM jellyfin_library j
JOIN seasons s ON s.id = j.target_id
WHERE j.target_type = 'season' AND s.show_tmdb_id = ?`
	rows, err := d.Read(ctx, q, showTMDBID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int]int)
	for rows.Next() {
		var number, episodes int
		if err := rows.Scan(&number, &episodes); err != nil {
			return nil, err
		}
		out[number] = episodes
	}
	return out, rows.Err()
}
