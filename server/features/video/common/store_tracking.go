package common

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// InsertWatch records a watch unconditionally — the deliberate log of one
// viewing at one instant, which is what makes rewatch history first-class
// (D-009). No NOT EXISTS guard, because a repeat here is the point, not a
// double-submit: the caller reaches this instead of insertWatchIfNever only
// when the user named the instant, and the SPA disables its confirm while the
// write is in flight.
//
// at is already normalised to RFC3339 UTC by the service; this never invents a
// timestamp of its own.
func InsertWatch(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64, at, watchType string) error {
	const q = `
INSERT INTO user_watch_history (user_id, target_type, target_id, watched_at, watch_type)
VALUES (?, ?, ?, ?, ?)`
	_, err := tx.Exec(ctx, q, userID, targetType, targetID, at, watchType)
	return err
}

// InsertWatchlist adds the target to the user's watchlist; adding it again is
// a no-op reported as created=false.
func InsertWatchlist(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64, at string) (bool, error) {
	const q = `
INSERT INTO user_video_watchlist (user_id, target_type, target_id, added_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id, target_type, target_id) DO NOTHING`
	res, err := tx.Exec(ctx, q, userID, targetType, targetID, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
