package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

// userRow is a users row as the service consumes it. password_hash is
// nullable — an OIDC-only user (M5) has no local password.
type userRow struct {
	ID    int64
	Email string
	// DisplayName is NULL until someone sets one; callers fall back to the
	// email's local part for display.
	DisplayName  sql.NullString
	PasswordHash sql.NullString
	IsAdmin      bool
	// MatchWatchlist is the D-028 consent flag: whether other users may pick
	// this account in their watchlist's Matching filter.
	MatchWatchlist bool
	// Timezone is the account's chosen IANA zone (D-038); NULL follows the
	// server default.
	Timezone sql.NullString
}

// createFirstAdmin inserts the first user, as admin, only while the users
// table is empty. The guard is inside the INSERT itself so two concurrent
// setup requests cannot both pass a separate count check.
func createFirstAdmin(ctx context.Context, tx db.Tx, email, displayName, passwordHash, now string) (int64, error) {
	const q = `
INSERT INTO users (email, display_name, password_hash, is_admin, created_at)
SELECT ?, ?, ?, 1, ?
WHERE NOT EXISTS (SELECT 1 FROM users)
RETURNING id`
	var id int64
	err := tx.QueryRow(ctx, q, email, nullString(displayName), nullString(passwordHash), now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: setup already completed", httpx.ErrConflict)
	}
	return id, err
}

// createUser inserts an account unless the email is already registered. The
// existence check is inside the INSERT for the same atomicity reason as
// createFirstAdmin — and it keeps the SQL portable instead of sniffing
// driver-specific UNIQUE-violation errors.
func createUser(ctx context.Context, tx db.Tx, email, displayName, passwordHash string, isAdmin bool, now string) (int64, error) {
	const q = `
INSERT INTO users (email, display_name, password_hash, is_admin, created_at)
SELECT ?, ?, ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM users WHERE email = ?)
RETURNING id`
	var id int64
	err := tx.QueryRow(ctx, q, email, nullString(displayName), nullString(passwordHash), isAdmin, now, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: that email is already registered", httpx.ErrConflict)
	}
	return id, err
}

func getUserByID(ctx context.Context, d *db.DB, id int64) (userRow, error) {
	const q = `SELECT id, email, display_name, password_hash, is_admin, match_watchlist, timezone FROM users WHERE id = ?`
	return scanUserRow(d.ReadRow(ctx, q, id))
}

func scanUserRow(row *sql.Row) (userRow, error) {
	var u userRow
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin, &u.MatchWatchlist, &u.Timezone)
	if errors.Is(err, sql.ErrNoRows) {
		return userRow{}, fmt.Errorf("%w: no such user", httpx.ErrNotFound)
	}
	return u, err
}

func countUsers(ctx context.Context, d *db.DB) (int64, error) {
	var n int64
	err := d.ReadRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// listUsers returns every account for the admin user list. No password
// hashes: nothing outside the verify path ever reads one.
func listUsers(ctx context.Context, d *db.DB) ([]UserListEntry, error) {
	// `linked` tells the admin at a glance whether an account already has an
	// SSO identity — i.e. whether it will attach on the next OIDC login or
	// still needs its email filled in.
	const q = `
SELECT u.id, u.email, u.display_name, u.is_admin, u.created_at,
       EXISTS (SELECT 1 FROM oauth_identities oi WHERE oi.user_id = u.id)
FROM users u
ORDER BY u.email`
	rows, err := d.Read(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []UserListEntry{}
	for rows.Next() {
		var u UserListEntry
		var displayName sql.NullString
		if err := rows.Scan(&u.ID, &u.Email, &displayName, &u.IsAdmin, &u.CreatedAt, &u.Linked); err != nil {
			return nil, err
		}
		u.DisplayName = displayName.String
		users = append(users, u)
	}
	return users, rows.Err()
}

func updatePasswordHash(ctx context.Context, tx db.Tx, userID int64, passwordHash string) error {
	const q = `UPDATE users SET password_hash = ? WHERE id = ?`
	_, err := tx.Exec(ctx, q, passwordHash, userID)
	return err
}

// deleteUserRow removes the account. Every per-user table declares
// `user_id ... REFERENCES users(id) ON DELETE CASCADE` — sessions, watch
// history, states, watchlist and ratings included — and the driver runs with
// foreign_keys(1), so this one statement takes all of that user's state with
// it. What it deliberately does NOT do is clean up catalog rows nobody
// references any more: queueing those is the D-008 sweep's job, and nothing
// queues yet (watchlist and rating removal have the same gap today — see
// features/video/service_tracking.go).
func deleteUserRow(ctx context.Context, tx db.Tx, userID int64) error {
	const q = `DELETE FROM users WHERE id = ?`
	_, err := tx.Exec(ctx, q, userID)
	return err
}

// getUserByEmail finds an account by email for OIDC linking. Only ever called
// with a verified address (see linkOrProvision).
func getUserByEmail(ctx context.Context, d *db.DB, email string) (userRow, error) {
	const q = `SELECT id, email, display_name, password_hash, is_admin, match_watchlist, timezone FROM users WHERE email = ?`
	return scanUserRow(d.ReadRow(ctx, q, email))
}

// updateUserEmail changes an account's login identifier. The guard keeps the
// UNIQUE constraint from surfacing as a driver-specific error. It cannot
// clear the address: email is NOT NULL because it is how you sign in.
func updateUserEmail(ctx context.Context, tx db.Tx, userID int64, email string) error {
	const q = `
UPDATE users SET email = ?
WHERE id = ?
  AND NOT EXISTS (SELECT 1 FROM users other WHERE other.email = ? AND other.id <> ?)`
	res, err := tx.Exec(ctx, q, email, userID, email, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: that email is already used by another account", httpx.ErrConflict)
	}
	return nil
}

// setUserAdmin is used by the OIDC login path, which recomputes the flag from
// the IdP claim on every sign-in.
func setUserAdmin(ctx context.Context, tx db.Tx, userID int64, isAdmin bool) error {
	const q = `UPDATE users SET is_admin = ? WHERE id = ?`
	_, err := tx.Exec(ctx, q, isAdmin, userID)
	return err
}

// setMatchWatchlist flips the D-028 consent flag. A plain UPDATE on a NOT
// NULL DEFAULT 0 column: there is no row to create and nothing to validate.
func setMatchWatchlist(ctx context.Context, tx db.Tx, userID int64, on bool) error {
	const q = `UPDATE users SET match_watchlist = ? WHERE id = ?`
	_, err := tx.Exec(ctx, q, on, userID)
	return err
}

// setTimezone stores or clears (nil) the account's zone (D-038). The name was
// validated by the service; the column is a plain nullable label.
func setTimezone(ctx context.Context, tx db.Tx, userID int64, zone *string) error {
	const q = `UPDATE users SET timezone = ? WHERE id = ?`
	var v any
	if zone != nil {
		v = *zone
	}
	_, err := tx.Exec(ctx, q, v, userID)
	return err
}

// nullString keeps optional TEXT columns NULL rather than empty: users.email
// is UNIQUE, and SQLite/Postgres both allow many NULLs but only one "".
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// updateDisplayName sets or clears the human label. Unlike email there is no
// uniqueness guard: two accounts may share a display name (D-019).
func updateDisplayName(ctx context.Context, tx db.Tx, userID int64, name string) error {
	const q = `UPDATE users SET display_name = ? WHERE id = ?`
	_, err := tx.Exec(ctx, q, nullString(name), userID)
	return err
}
