package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

func insertSession(ctx context.Context, tx db.Tx, token string, userID int64, createdAt, expiresAt, userAgent, ip string) error {
	const q = `
INSERT INTO sessions (token, user_id, created_at, expires_at, user_agent, ip)
VALUES (?, ?, ?, ?, ?, ?)`
	_, err := tx.Exec(ctx, q, token, userID, createdAt, expiresAt, userAgent, ip)
	return err
}

// getSessionUser resolves a live session token to its user. Expiry is part of
// the predicate: an expired row is indistinguishable from a missing one.
// RFC3339 UTC strings compare chronologically as strings.
func getSessionUser(ctx context.Context, d *db.DB, token, now string) (User, string, error) {
	const q = `
SELECT u.id, u.email, u.display_name, u.is_admin, u.match_watchlist, u.timezone, s.expires_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token = ? AND s.expires_at > ?`
	var u User
	var expiresAt string
	var displayName, timezone sql.NullString
	err := d.ReadRow(ctx, q, token, now).Scan(&u.ID, &u.Email, &displayName, &u.IsAdmin, &u.MatchWatchlist, &timezone, &expiresAt)
	u.DisplayName = displayName.String
	u.Timezone = nullableString(timezone)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", fmt.Errorf("%w: no valid session", httpx.ErrUnauthorized)
	}
	return u, expiresAt, err
}

func touchSession(ctx context.Context, tx db.Tx, token, expiresAt string) error {
	const q = `UPDATE sessions SET expires_at = ? WHERE token = ?`
	_, err := tx.Exec(ctx, q, expiresAt, token)
	return err
}

func deleteSession(ctx context.Context, tx db.Tx, token string) error {
	const q = `DELETE FROM sessions WHERE token = ?`
	_, err := tx.Exec(ctx, q, token)
	return err
}

// deleteExpiredSessions is run opportunistically inside login/setup
// transactions — a household app's sessions table stays tiny without a sweeper.
func deleteExpiredSessions(ctx context.Context, tx db.Tx, now string) error {
	const q = `DELETE FROM sessions WHERE expires_at <= ?`
	_, err := tx.Exec(ctx, q, now)
	return err
}

// deleteUserSessions revokes a user's sessions, keeping exceptToken alive.
// Pass "" to revoke every one of them — an admin resetting someone else's
// password supplies a token that cannot match any of the target's rows, so
// the reset logs that user out everywhere.
func deleteUserSessions(ctx context.Context, tx db.Tx, userID int64, exceptToken string) error {
	const q = `DELETE FROM sessions WHERE user_id = ? AND token <> ?`
	_, err := tx.Exec(ctx, q, userID, exceptToken)
	return err
}
