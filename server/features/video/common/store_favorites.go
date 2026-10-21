package common

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// Favorites (D-030): the third independent per-user axis, beside the watchlist
// (D-011) and owned copies (D-021). The table is the watchlist's shape byte
// for byte, which is why the list query below returns watchlistTarget rows and
// the service reuses the watchlist's two projection helpers — the card is the
// same card. A favorite row alone is a D-008 reference and puts the item in
// the scheduled refresh's ACTIVE class (store_refresh.go).

// InsertFavorite marks the target as one of the user's favorites; adding it
// again is a no-op reported as created=false.
func InsertFavorite(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64, at string) (bool, error) {
	const q = `
INSERT INTO user_video_favorites (user_id, target_type, target_id, added_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_id, target_type, target_id) DO NOTHING`
	res, err := tx.Exec(ctx, q, userID, targetType, targetID, at)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
