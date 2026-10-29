package common

import (
	"context"
	"database/sql"
	"errors"

	"mbyte.ro/sofalog/db"
)

// userTimezone reads the IANA zone a user chose (D-038). NULL — and a user
// row that is gone, which a live session cannot have but a background job
// might race — both mean "the server default", so neither is an error.
// The column is auth's to write; video only reads it, as it reads
// match_watchlist.
func userTimezone(ctx context.Context, d *db.DB, userID int64) (string, error) {
	const q = `SELECT timezone FROM users WHERE id = ?`
	var zone sql.NullString
	err := d.ReadRow(ctx, q, userID).Scan(&zone)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return zone.String, err
}
