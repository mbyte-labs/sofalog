package watches

import (
	"context"
	"database/sql"
	"errors"

	"mbyte.ro/sofalog/db"
)

// insertWatchIfNever records a watch only if this user has never watched the
// target. user_watch_history has NO unique constraint (a rewatch is a
// legitimate new row, D-009), so idempotency lives here: a double-submit or
// optimistic retry inserts nothing and reports created=false. Logging a
// deliberate rewatch is InsertWatch below, which always inserts.
func insertWatchIfNever(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64, at, watchType string) (bool, error) {
	const q = `
INSERT INTO user_watch_history (user_id, target_type, target_id, watched_at, watch_type)
SELECT ?, ?, ?, ?, ?
WHERE NOT EXISTS (
    SELECT 1 FROM user_watch_history
    WHERE user_id = ? AND target_type = ? AND target_id = ?)`
	res, err := tx.Exec(ctx, q, userID, targetType, targetID, at, watchType,
		userID, targetType, targetID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// deleteWatchByID removes ONE row from the watch log and reports what it
// pointed at, so the caller can recompute the right status. Unlike the
// watchlist and rating deletes this one is not idempotent: it addresses a
// specific row rather than a set, and found=false is the caller's 404.
//
// Scoped by user_id as well as id, and not for tidiness: user_watch_history.id
// is a global sequence, so without it one user could delete another's row.
//
// RETURNING gets the existence check and the recompute key in one statement,
// inside the caller's transaction. It runs on both engines and is already used
// by the catalog upserts.
func deleteWatchByID(ctx context.Context, tx db.Tx, userID, watchID int64) (targetType string, targetID int64, found bool, err error) {
	const q = `
DELETE FROM user_watch_history
WHERE id = ? AND user_id = ?
RETURNING target_type, target_id`
	err = tx.QueryRow(ctx, q, watchID, userID).Scan(&targetType, &targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return targetType, targetID, true, nil
}
