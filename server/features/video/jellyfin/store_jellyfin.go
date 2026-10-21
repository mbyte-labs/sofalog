package jellyfin

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// The Jellyfin library scan's queries (D-033). Two whole-catalog reads give
// the scan its matching keys — compile-time queries mapped in Go, the
// LibraryMovieFlags pattern, never an IN list spliced from what the server
// returned — one replace writes the answer, and two reads project it into the
// copies lists.

// movieIDsByTMDB maps every catalog movie's TMDB id to its local id.
func movieIDsByTMDB(ctx context.Context, d *db.DB) (map[int64]int64, error) {
	const q = `SELECT tmdb_id, id FROM movies`
	rows, err := d.Read(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]int64)
	for rows.Next() {
		var tmdbID, id int64
		if err := rows.Scan(&tmdbID, &id); err != nil {
			return nil, err
		}
		out[tmdbID] = id
	}
	return out, rows.Err()
}

// jellyfinSeasonKey addresses a season the way Jellyfin can: by its show's TMDB id
// and its number. seasons has no TMDB id of its own, and (show_tmdb_id,
// season_number) is its UNIQUE key.
type jellyfinSeasonKey struct {
	showTMDBID int64
	number     int
}

// seasonIDsByShowTMDB maps every catalog season's key to its local id.
func seasonIDsByShowTMDB(ctx context.Context, d *db.DB) (map[jellyfinSeasonKey]int64, error) {
	const q = `SELECT show_tmdb_id, season_number, id FROM seasons`
	rows, err := d.Read(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[jellyfinSeasonKey]int64)
	for rows.Next() {
		var key jellyfinSeasonKey
		var id int64
		if err := rows.Scan(&key.showTMDBID, &key.number, &id); err != nil {
			return nil, err
		}
		out[key] = id
	}
	return out, rows.Err()
}

// jellyfinEntry is one matched row: a movie or a season the server holds.
// episodes is the distinct episode files found in a season (0 for a movie) —
// what lets a page tell a complete season from a partial one.
type jellyfinEntry struct {
	targetType string
	targetID   int64
	jellyfinID string
	episodes   int
}

// replaceJellyfinLibrary rewrites the whole table with this scan's matches.
// Delete-then-insert inside the caller's transaction, so readers see either
// the previous scan's answer or this one's, never a half-written one, and a
// scan that failed before reaching here has left the table untouched.
func replaceJellyfinLibrary(ctx context.Context, tx db.Tx, entries []jellyfinEntry, seenAt string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM jellyfin_library`); err != nil {
		return err
	}
	const q = `
INSERT INTO jellyfin_library (target_type, target_id, jellyfin_id, episodes, seen_at)
VALUES (?, ?, ?, ?, ?)`
	for _, e := range entries {
		if _, err := tx.Exec(ctx, q, e.targetType, e.targetID, e.jellyfinID, e.episodes, seenAt); err != nil {
			return err
		}
	}
	return nil
}
