package common

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// The three things the outbox mirrors, each queued by every local write once
// the user is linked: a rating, a watchlist entry, or a favorite (D-030).
const (
	SyncKindRating    = "rating"
	SyncKindWatchlist = "watchlist"
	SyncKindFavorite  = "favorite"
)

// EnqueueTMDBSync queues one change for the push. The link check AND the
// kind's switch are IN the statement — an unlinked or broken user, or one who
// turned that kind off (D-036), writes nothing even if the caller forgot to
// ask — and a pending row for the same (kind, target) is replaced: the outbox
// holds the current intent, not a history. rating is nil except for a rating
// 'set'.
func EnqueueTMDBSync(ctx context.Context, tx db.Tx, userID int64, kind, targetType string, targetID int64, op string, rating *int, now string) error {
	const q = `
INSERT INTO user_tmdb_sync
    (user_id, kind, target_type, target_id, op, rating, attempts, next_retry_at, last_error, created_at, updated_at)
SELECT ?, ?, ?, ?, ?, ?, 0, NULL, NULL, ?, ?
WHERE EXISTS (
    SELECT 1 FROM user_tmdb_accounts
    WHERE user_id = ? AND broken_at IS NULL
      AND ((? = 'rating'    AND sync_ratings   = 1)
        OR (? = 'watchlist' AND sync_watchlist = 1)
        OR (? = 'favorite'  AND sync_favorites = 1)))
ON CONFLICT (user_id, kind, target_type, target_id) DO UPDATE SET
    op            = excluded.op,
    rating        = excluded.rating,
    attempts      = 0,
    next_retry_at = NULL,
    last_error    = NULL,
    updated_at    = excluded.updated_at`
	_, err := tx.Exec(ctx, q, userID, kind, targetType, targetID, op, rating, now, now, userID, kind, kind, kind)
	return err
}
