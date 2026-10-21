package ratings

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// deleteVideoRating clears this user's rating of one target. Clearing what was
// never rated is a no-op, not an error — the operation is idempotent, like
// deleteWatchlist.
func deleteVideoRating(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64) error {
	const q = `
DELETE FROM user_video_ratings
WHERE user_id = ? AND target_type = ? AND target_id = ?`
	_, err := tx.Exec(ctx, q, userID, targetType, targetID)
	return err
}
