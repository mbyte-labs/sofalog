package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

// oidcProvider is the value stored in oauth_identities.provider. sofalog
// supports one configured IdP, so the column records which KIND of identity a
// row is rather than distinguishing between several providers; the UNIQUE
// (provider, provider_user_id) pair still identifies the subject.
const oidcProvider = "oidc"

// getUserByOIDCSubject resolves a returning user by the IdP's stable subject
// id — the only identifier guaranteed not to change when someone's email or
// display name does.
func getUserByOIDCSubject(ctx context.Context, d *db.DB, subject string) (userRow, error) {
	const q = `
SELECT u.id, u.email, u.display_name, u.password_hash, u.is_admin
FROM oauth_identities oi
JOIN users u ON u.id = oi.user_id
WHERE oi.provider = ? AND oi.provider_user_id = ?`
	var u userRow
	err := d.ReadRow(ctx, q, oidcProvider, subject).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return userRow{}, fmt.Errorf("%w: no account for that identity", httpx.ErrNotFound)
	}
	return u, err
}

func linkOIDCIdentity(ctx context.Context, tx db.Tx, userID int64, subject, now string) error {
	const q = `
INSERT INTO oauth_identities (user_id, provider, provider_user_id, created_at)
VALUES (?, ?, ?, ?)`
	_, err := tx.Exec(ctx, q, userID, oidcProvider, subject, now)
	return err
}
