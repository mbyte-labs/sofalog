package common

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// UpsertVideoRating writes this user's rating of one target, overwriting any
// previous value. Re-rating is an overwrite by design (D-009): a watch is an
// event that accumulates, a rating is the one current opinion, so there is no
// history row and nothing to report about whether the row already existed.
// Every predicate carries target_type, because movie N and episode N both
// exist (D-013).
func UpsertVideoRating(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64, rating int, at string) error {
	const q = `
INSERT INTO user_video_ratings (user_id, target_type, target_id, rating, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_id, target_type, target_id) DO UPDATE SET
    rating     = excluded.rating,
    updated_at = excluded.updated_at`
	_, err := tx.Exec(ctx, q, userID, targetType, targetID, rating, at)
	return err
}
