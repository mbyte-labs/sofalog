package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/migrations"
)

// testDB opens a real temp-file SQLite database with the real migrations
// applied. Service and store tests run against actual SQL — there are no
// store interfaces to mock (see the go-backend skill).
func testDB(t *testing.T) *db.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := db.Migrate(ctx, database, migrations.FS()); err != nil {
		t.Fatalf("migrating test db: %v", err)
	}
	return database
}

// seedUserWithPassword inserts an account with a real argon2id hash. password
// "" seeds a NULL password_hash (an OIDC-only account). The first argument is
// the EMAIL — the login identifier — not a username.
func seedUserWithPassword(t *testing.T, database *db.DB, email, password string, isAdmin bool) int64 {
	t.Helper()
	var hash any
	if password != "" {
		h, err := hashPassword(password)
		if err != nil {
			t.Fatalf("hashing password: %v", err)
		}
		hash = h
	}
	var id int64
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		return tx.QueryRow(context.Background(),
			`INSERT INTO users (email, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?) RETURNING id`,
			email, hash, isAdmin, "2026-08-18T00:00:00Z").Scan(&id)
	})
	if err != nil {
		t.Fatalf("seeding user %q: %v", email, err)
	}
	return id
}

// mintSession inserts a live session row for userID and returns its token —
// the real-session equivalent of stamping WithUserID by hand.
func mintSession(t *testing.T, database *db.DB, userID int64) string {
	t.Helper()
	return mintSessionExpiring(t, database, userID, time.Now().UTC().Add(sessionTTL))
}

func mintSessionExpiring(t *testing.T, database *db.DB, userID int64, expiresAt time.Time) string {
	t.Helper()
	token, err := newSessionToken()
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}
	err = database.InTx(context.Background(), func(tx db.Tx) error {
		return insertSession(context.Background(), tx, token, userID,
			time.Now().UTC().Format(time.RFC3339), expiresAt.Format(time.RFC3339), "test", "127.0.0.1")
	})
	if err != nil {
		t.Fatalf("minting session: %v", err)
	}
	return token
}
