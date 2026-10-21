package copies

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// insertCopy records that the user holds a copy of the target on one kind;
// recording it again is a no-op reported as created=false.
func insertCopy(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64, kind, at string) (bool, error) {
	const q = `
INSERT INTO user_video_copies (user_id, target_type, target_id, kind, added_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_id, target_type, target_id, kind) DO NOTHING`
	res, err := tx.Exec(ctx, q, userID, targetType, targetID, kind, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// insertSeasonCopiesForShow is the show-scope fan-out: one copy row per
// NON-SPECIAL season of the show (season 0 never rides a show-level action —
// D-021; it stays addressable from its own page). Seasons that already carry
// the kind are skipped, so the return value counts rows actually written.
func insertSeasonCopiesForShow(ctx context.Context, tx db.Tx, userID, showID int64, kind, at string) (int64, error) {
	const q = `
INSERT INTO user_video_copies (user_id, target_type, target_id, kind, added_at)
SELECT ?, 'season', s.id, ?, ?
FROM seasons s
WHERE s.show_id = ? AND s.season_number > 0
ON CONFLICT (user_id, target_type, target_id, kind) DO NOTHING`
	res, err := tx.Exec(ctx, q, userID, kind, at, showID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// deleteCopy removes one kind from the user's copies of the target. Removing
// an absent row is a no-op, not an error — the operation is idempotent.
func deleteCopy(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64, kind string) error {
	const q = `
DELETE FROM user_video_copies
WHERE user_id = ? AND target_type = ? AND target_id = ? AND kind = ?`
	_, err := tx.Exec(ctx, q, userID, targetType, targetID, kind)
	return err
}

// deleteSeasonCopiesForShow mirrors the fan-out insert: the kind comes off
// every NON-SPECIAL season of the show, symmetrically sparing a copy set on
// Specials from its own page (D-021).
func deleteSeasonCopiesForShow(ctx context.Context, tx db.Tx, userID, showID int64, kind string) error {
	const q = `
DELETE FROM user_video_copies
WHERE user_id = ? AND target_type = 'season' AND kind = ?
  AND target_id IN (SELECT id FROM seasons WHERE show_id = ? AND season_number > 0)`
	_, err := tx.Exec(ctx, q, userID, kind, showID)
	return err
}
