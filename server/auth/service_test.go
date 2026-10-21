package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

var testNow = time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)

func testService(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	database := testDB(t)
	svc := NewService(database, nil, "UTC")
	svc.now = func() time.Time { return testNow }
	return svc, database
}

func TestSetup(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()

	user, sess, err := svc.Setup(ctx, "Mihai@Example.test", "Mihai B.", "hunter2hunter2", "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if user.Email != "mihai@example.test" {
		t.Errorf("email = %q, want lowercased", user.Email)
	}
	if user.DisplayName != "Mihai B." {
		t.Errorf("display name = %q", user.DisplayName)
	}
	if !user.IsAdmin {
		t.Error("first user is not admin")
	}
	if sess.Token == "" {
		t.Error("no session token")
	}
	if want := testNow.Add(sessionTTL); !sess.ExpiresAt.Equal(want) {
		t.Errorf("expires at %v, want %v", sess.ExpiresAt, want)
	}

	var ip, ua string
	err = database.ReadRow(ctx, `SELECT ip, user_agent FROM sessions WHERE token = ?`, sess.Token).Scan(&ip, &ua)
	if err != nil {
		t.Fatalf("reading session row: %v", err)
	}
	if ip != "10.0.0.1" || ua != "test-agent" {
		t.Errorf("session row ip=%q ua=%q", ip, ua)
	}

	// Setup works exactly once per instance.
	if _, _, err := svc.Setup(ctx, "second@example.test", "", "hunter2hunter2", "", ""); !errors.Is(err, httpx.ErrConflict) {
		t.Errorf("second setup err = %v, want ErrConflict", err)
	}
}

func TestSetupValidation(t *testing.T) {
	t.Parallel()
	svc, _ := testService(t)
	tests := []struct {
		name, email, password string
	}{
		{"empty email", "", "hunter2hunter2"},
		{"no at sign", "mihai", "hunter2hunter2"},
		{"nothing before the at", "@example.test", "hunter2hunter2"},
		{"nothing after the at", "mihai@", "hunter2hunter2"},
		{"whitespace inside", "mi hai@example.test", "hunter2hunter2"},
		{"password too short", "mihai@example.test", "short"},
		{"password too long", "mihai@example.test", string(make([]byte, 129))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := svc.Setup(context.Background(), tt.email, "", tt.password, "", ""); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", true)
	seedUserWithPassword(t, database, "oidc-only@example.test", "", false) // NULL password_hash

	// Uppercase input is normalised, so the address is not case-sensitive.
	user, sess, err := svc.Login(ctx, "MIHAI@EXAMPLE.TEST", "hunter2hunter2", "10.0.0.2", "agent")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if user.Email != "mihai@example.test" || !user.IsAdmin {
		t.Errorf("user = %+v", user)
	}
	var expiresAt string
	err = database.ReadRow(ctx, `SELECT expires_at FROM sessions WHERE token = ?`, sess.Token).Scan(&expiresAt)
	if err != nil {
		t.Fatalf("reading session row: %v", err)
	}
	if want := testNow.Add(sessionTTL).Format(time.RFC3339); expiresAt != want {
		t.Errorf("expires_at = %q, want %q", expiresAt, want)
	}

	// Every failure shape returns the byte-identical error, so neither the
	// message nor the shape reveals which usernames exist. (The dummy-hash
	// timing leveling is the code-level facet of the same rule.)
	failures := []struct {
		name, email, password string
	}{
		{"unknown email", "nobody@example.test", "hunter2hunter2"},
		{"wrong password", "mihai@example.test", "wrong password"},
		{"no local password", "oidc-only@example.test", "hunter2hunter2"},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.Login(ctx, tt.email, tt.password, "", "")
			if !errors.Is(err, httpx.ErrUnauthorized) {
				t.Fatalf("err = %v, want ErrUnauthorized", err)
			}
			if err.Error() != errInvalidCredentials.Error() {
				t.Errorf("message %q differs from the shared invalid-credentials error", err)
			}
		})
	}
}

func TestLoginPurgesExpiredSessions(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	expired := mintSessionExpiring(t, database, userID, testNow.Add(-time.Hour))
	live := mintSessionExpiring(t, database, userID, testNow.Add(time.Hour))

	if _, _, err := svc.Login(ctx, "mihai@example.test", "hunter2hunter2", "", ""); err != nil {
		t.Fatalf("Login: %v", err)
	}

	var n int
	if err := database.ReadRow(ctx, `SELECT COUNT(*) FROM sessions WHERE token = ?`, expired).Scan(&n); err != nil || n != 0 {
		t.Errorf("expired session still present (n=%d, err=%v)", n, err)
	}
	if err := database.ReadRow(ctx, `SELECT COUNT(*) FROM sessions WHERE token = ?`, live).Scan(&n); err != nil || n != 1 {
		t.Errorf("live session was purged (n=%d, err=%v)", n, err)
	}
}

func TestLogoutIdempotent(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	token := mintSession(t, database, userID)

	for i := 0; i < 2; i++ {
		if err := svc.Logout(ctx, token); err != nil {
			t.Fatalf("Logout #%d: %v", i+1, err)
		}
	}
	if err := svc.Logout(ctx, ""); err != nil {
		t.Fatalf("Logout(empty): %v", err)
	}
	if _, _, err := svc.Authenticate(ctx, token); !errors.Is(err, httpx.ErrUnauthorized) {
		t.Errorf("session survived logout: %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)

	t.Run("fresh session is not refreshed", func(t *testing.T) {
		token := mintSessionExpiring(t, database, userID, testNow.Add(sessionTTL))
		user, refreshed, err := svc.Authenticate(ctx, token)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		if user.ID != userID {
			t.Errorf("user id = %d, want %d", user.ID, userID)
		}
		if refreshed != nil {
			t.Errorf("fresh session was refreshed to %v", refreshed)
		}
	})

	t.Run("old session slides forward", func(t *testing.T) {
		// More than refreshAfter of the TTL already burnt.
		token := mintSessionExpiring(t, database, userID, testNow.Add(sessionTTL-refreshAfter-time.Hour))
		_, refreshed, err := svc.Authenticate(ctx, token)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
		if refreshed == nil {
			t.Fatal("old session was not refreshed")
		}
		if want := testNow.Add(sessionTTL); !refreshed.Equal(want) {
			t.Errorf("refreshed to %v, want %v", refreshed, want)
		}
		var stored string
		if err := database.ReadRow(ctx, `SELECT expires_at FROM sessions WHERE token = ?`, token).Scan(&stored); err != nil {
			t.Fatalf("reading session: %v", err)
		}
		if want := testNow.Add(sessionTTL).Format(time.RFC3339); stored != want {
			t.Errorf("stored expires_at = %q, want %q", stored, want)
		}
	})

	t.Run("expired session is unauthorized", func(t *testing.T) {
		token := mintSessionExpiring(t, database, userID, testNow.Add(-time.Minute))
		if _, _, err := svc.Authenticate(ctx, token); !errors.Is(err, httpx.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("garbage token is unauthorized", func(t *testing.T) {
		if _, _, err := svc.Authenticate(ctx, "not-a-token"); !errors.Is(err, httpx.ErrUnauthorized) {
			t.Errorf("err = %v, want ErrUnauthorized", err)
		}
	})
}

func TestCreateUser(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "hunter2hunter2", false)

	user, err := svc.CreateUser(ctx, adminID, "NewUser@example.test", "", "hunter2hunter2")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Email != "newuser@example.test" || user.IsAdmin {
		t.Errorf("created user = %+v, want a non-admin newuser", user)
	}

	if _, err := svc.CreateUser(ctx, memberID, "another@example.test", "", "hunter2hunter2"); !errors.Is(err, httpx.ErrForbidden) {
		t.Errorf("non-admin create err = %v, want ErrForbidden", err)
	}
	if _, err := svc.CreateUser(ctx, adminID, "newuser@example.test", "", "hunter2hunter2"); !errors.Is(err, httpx.ErrConflict) {
		t.Errorf("duplicate email err = %v, want ErrConflict", err)
	}
	if _, err := svc.CreateUser(ctx, adminID, "ok@example.test", "", "short"); !errors.Is(err, httpx.ErrValidation) {
		t.Errorf("short password err = %v, want ErrValidation", err)
	}
}

// The multi-user invariant: one user's session never authenticates as, or
// affects, another user.
func TestTwoUserIsolation(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	aliceID := seedUserWithPassword(t, database, "alice@example.test", "hunter2hunter2", false)
	bobID := seedUserWithPassword(t, database, "bob@example.test", "hunter2hunter2", false)
	aliceToken := mintSession(t, database, aliceID)
	bobToken := mintSession(t, database, bobID)

	aliceUser, _, err := svc.Authenticate(ctx, aliceToken)
	if err != nil {
		t.Fatalf("Authenticate(alice): %v", err)
	}
	if aliceUser.ID != aliceID {
		t.Errorf("alice's token resolved to user %d", aliceUser.ID)
	}

	if err := svc.Logout(ctx, aliceToken); err != nil {
		t.Fatalf("Logout(alice): %v", err)
	}
	bobUser, _, err := svc.Authenticate(ctx, bobToken)
	if err != nil {
		t.Fatalf("bob's session broke when alice logged out: %v", err)
	}
	if bobUser.ID != bobID {
		t.Errorf("bob's token resolved to user %d", bobUser.ID)
	}
}

func TestStatus(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()

	st, err := svc.Status(ctx, "")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "setup_required" {
		t.Errorf("empty instance status = %q, want setup_required", st.Status)
	}

	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", true)
	for name, token := range map[string]string{"no cookie": "", "garbage": "nope"} {
		st, err = svc.Status(ctx, token)
		if err != nil {
			t.Fatalf("Status(%s): %v", name, err)
		}
		if st.Status != "unauthenticated" {
			t.Errorf("Status(%s) = %q, want unauthenticated", name, st.Status)
		}
	}

	st, err = svc.Status(ctx, mintSession(t, database, userID))
	if err != nil {
		t.Fatalf("Status(session): %v", err)
	}
	if st.Status != "authenticated" || st.User == nil || st.User.Email != "mihai@example.test" {
		t.Errorf("Status(session) = %+v", st)
	}
}

func TestListUsers(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "zadmin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "alice@example.test", "hunter2hunter2", false)

	users, err := svc.ListUsers(ctx, adminID)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
	// Ordered by email, so "alice" precedes "zadmin" despite being newer.
	if users[0].Email != "alice@example.test" || users[1].Email != "zadmin@example.test" {
		t.Errorf("wrong order: %q, %q", users[0].Email, users[1].Email)
	}
	if users[1].IsAdmin != true || users[0].IsAdmin != false {
		t.Errorf("admin flags wrong: %+v", users)
	}
	if users[0].CreatedAt == "" {
		t.Error("created_at not populated")
	}

	if _, err := svc.ListUsers(ctx, memberID); !errors.Is(err, httpx.ErrForbidden) {
		t.Errorf("non-admin list err = %v, want ErrForbidden", err)
	}
}

func TestSetPasswordAdminResetsOther(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "oldpassword", false)

	// An admin resets someone else's password without supplying the old one.
	if err := svc.SetPassword(ctx, adminID, memberID, "", "brandnewpassword", "admin-token"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if _, _, err := svc.Login(ctx, "member@example.test", "oldpassword", "", ""); !errors.Is(err, httpx.ErrUnauthorized) {
		t.Error("old password still works after a reset")
	}
	if _, _, err := svc.Login(ctx, "member@example.test", "brandnewpassword", "", ""); err != nil {
		t.Errorf("new password does not work: %v", err)
	}
}

func TestSetPasswordSelf(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	memberID := seedUserWithPassword(t, database, "member@example.test", "oldpassword", false)
	otherID := seedUserWithPassword(t, database, "other@example.test", "hunter2hunter2", false)

	t.Run("wrong current password is a validation error, never unauthorized", func(t *testing.T) {
		err := svc.SetPassword(ctx, memberID, memberID, "notmypassword", "brandnewpassword", "tok")
		if !errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		// 401 would make the SPA's shared client call clearSession() and log
		// the user out for a typo. Guard the status, not just the failure.
		if errors.Is(err, httpx.ErrUnauthorized) {
			t.Error("wrong current password must not map to 401")
		}
	})

	t.Run("correct current password succeeds", func(t *testing.T) {
		if err := svc.SetPassword(ctx, memberID, memberID, "oldpassword", "brandnewpassword", "tok"); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
		if _, _, err := svc.Login(ctx, "member@example.test", "brandnewpassword", "", ""); err != nil {
			t.Errorf("new password does not work: %v", err)
		}
	})

	t.Run("a non-admin cannot change someone else's password", func(t *testing.T) {
		err := svc.SetPassword(ctx, memberID, otherID, "", "brandnewpassword", "tok")
		if !errors.Is(err, httpx.ErrForbidden) {
			t.Errorf("err = %v, want ErrForbidden", err)
		}
	})

	t.Run("the new password must pass the length rule", func(t *testing.T) {
		err := svc.SetPassword(ctx, memberID, memberID, "brandnewpassword", "short", "tok")
		if !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("err = %v, want ErrValidation", err)
		}
	})
}

// A password change must revoke the target's OTHER sessions — that is what
// makes a reset effective — while leaving the requesting device signed in.
func TestSetPasswordRevokesOtherSessions(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "oldpassword", false)

	t.Run("self change keeps this device", func(t *testing.T) {
		keep := mintSession(t, database, memberID)
		drop := mintSession(t, database, memberID)
		if err := svc.SetPassword(ctx, memberID, memberID, "oldpassword", "brandnewpassword", keep); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
		if _, _, err := svc.Authenticate(ctx, keep); err != nil {
			t.Errorf("requesting device was signed out: %v", err)
		}
		if _, _, err := svc.Authenticate(ctx, drop); !errors.Is(err, httpx.ErrUnauthorized) {
			t.Error("other device survived the password change")
		}
	})

	t.Run("admin reset kills every session of the target", func(t *testing.T) {
		a := mintSession(t, database, memberID)
		b := mintSession(t, database, memberID)
		adminSession := mintSession(t, database, adminID)
		if err := svc.SetPassword(ctx, adminID, memberID, "", "resetpassword", adminSession); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
		for name, tok := range map[string]string{"a": a, "b": b} {
			if _, _, err := svc.Authenticate(ctx, tok); !errors.Is(err, httpx.ErrUnauthorized) {
				t.Errorf("target session %s survived an admin reset", name)
			}
		}
		if _, _, err := svc.Authenticate(ctx, adminSession); err != nil {
			t.Errorf("the admin's own session was revoked: %v", err)
		}
	})
}

func TestDeleteUser(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "hunter2hunter2", false)

	if err := svc.DeleteUser(ctx, memberID, adminID); !errors.Is(err, httpx.ErrForbidden) {
		t.Errorf("non-admin delete err = %v, want ErrForbidden", err)
	}
	if err := svc.DeleteUser(ctx, adminID, adminID); !errors.Is(err, httpx.ErrValidation) {
		t.Errorf("self-delete err = %v, want ErrValidation", err)
	}
	if err := svc.DeleteUser(ctx, adminID, 9999); !errors.Is(err, httpx.ErrNotFound) {
		t.Errorf("unknown target err = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteUser(ctx, adminID, memberID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := getUserByID(ctx, database, memberID); !errors.Is(err, httpx.ErrNotFound) {
		t.Errorf("user still present after delete: %v", err)
	}
}

// Deleting a user must take all of that user's state with it, and none of
// anybody else's — the cascade half of the multi-user invariant.
func TestDeleteUserCascadesOnlyThatUser(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	doomedID := seedUserWithPassword(t, database, "doomed@example.test", "hunter2hunter2", false)
	keeperID := seedUserWithPassword(t, database, "keeper@example.test", "hunter2hunter2", false)

	// Same movie id for both users, and an episode sharing the id of the
	// movie — the polymorphic pair must carry target_type (D-013).
	seedState := func(userID int64) {
		t.Helper()
		err := database.InTx(ctx, func(tx db.Tx) error {
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO user_watch_history (user_id, target_type, target_id, watched_at, watch_type) VALUES (?, 'movie', 5, '2026-08-01T00:00:00Z', 'watch')`, nil},
				{`INSERT INTO user_watch_history (user_id, target_type, target_id, watched_at, watch_type) VALUES (?, 'episode', 5, '2026-08-01T00:00:00Z', 'watch')`, nil},
				{`INSERT INTO user_video_ratings (user_id, target_type, target_id, rating, updated_at) VALUES (?, 'movie', 5, 8, '2026-08-01T00:00:00Z')`, nil},
				{`INSERT INTO user_video_watchlist (user_id, target_type, target_id, added_at) VALUES (?, 'movie', 5, '2026-08-01T00:00:00Z')`, nil},
			} {
				if _, err := tx.Exec(ctx, q.sql, userID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("seeding state for %d: %v", userID, err)
		}
	}
	seedState(doomedID)
	seedState(keeperID)
	keeperToken := mintSession(t, database, keeperID)
	mintSession(t, database, doomedID)

	if err := svc.DeleteUser(ctx, adminID, doomedID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	count := func(table string, userID int64) int {
		t.Helper()
		var n int
		q := "SELECT COUNT(*) FROM " + table + " WHERE user_id = ?"
		if err := database.ReadRow(ctx, q, userID).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		return n
	}
	for _, table := range []string{"user_watch_history", "user_video_ratings", "user_video_watchlist", "sessions"} {
		if n := count(table, doomedID); n != 0 {
			t.Errorf("%s: %d rows survived the cascade", table, n)
		}
		if n := count(table, keeperID); n == 0 {
			t.Errorf("%s: the other user's rows were deleted too", table)
		}
	}
	if _, _, err := svc.Authenticate(ctx, keeperToken); err != nil {
		t.Errorf("the other user's session broke: %v", err)
	}
}

func TestSetDisplayName(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "hunter2hunter2", false)
	otherID := seedUserWithPassword(t, database, "other@example.test", "hunter2hunter2", false)

	// Your own label needs no password: it grants nothing.
	if err := svc.SetDisplayName(ctx, memberID, memberID, "  Mihai B.  "); err != nil {
		t.Fatalf("self rename: %v", err)
	}
	row, err := getUserByID(ctx, database, memberID)
	if err != nil {
		t.Fatal(err)
	}
	if row.DisplayName.String != "Mihai B." {
		t.Errorf("display name = %q, want it trimmed", row.DisplayName.String)
	}

	// Labels are not identities (D-019): a duplicate is perfectly legal.
	if err := svc.SetDisplayName(ctx, otherID, otherID, "Mihai B."); err != nil {
		t.Errorf("duplicate display name rejected: %v", err)
	}

	// Empty clears it, and the row goes back to NULL rather than "".
	if err := svc.SetDisplayName(ctx, memberID, memberID, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if row, _ := getUserByID(ctx, database, memberID); row.DisplayName.Valid {
		t.Errorf("cleared display name is %q, want NULL", row.DisplayName.String)
	}

	if err := svc.SetDisplayName(ctx, adminID, memberID, "Renamed By Admin"); err != nil {
		t.Errorf("admin rename: %v", err)
	}
	if err := svc.SetDisplayName(ctx, memberID, otherID, "Nope"); !errors.Is(err, httpx.ErrForbidden) {
		t.Errorf("renaming someone else = %v, want ErrForbidden", err)
	}
	if err := svc.SetDisplayName(ctx, memberID, memberID, strings.Repeat("x", 65)); !errors.Is(err, httpx.ErrValidation) {
		t.Errorf("over-long name = %v, want ErrValidation", err)
	}
}

func TestSetEmail(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "oldpassword", false)
	oidcOnly := seedUserWithPassword(t, database, "sso@example.test", "", false) // NULL password_hash

	t.Run("changing your own address requires the current password", func(t *testing.T) {
		err := svc.SetEmail(ctx, memberID, memberID, "new@example.test", "wrong")
		if !errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		// 401 would make the SPA client sign the user out over a typo.
		if errors.Is(err, httpx.ErrUnauthorized) {
			t.Error("a wrong password must not map to 401 here")
		}
		if err := svc.SetEmail(ctx, memberID, memberID, "New@Example.test", "oldpassword"); err != nil {
			t.Fatalf("self change: %v", err)
		}
		// The new address is the login identifier, normalised.
		if _, _, err := svc.Login(ctx, "new@example.test", "oldpassword", "", ""); err != nil {
			t.Errorf("cannot sign in with the new address: %v", err)
		}
		if _, _, err := svc.Login(ctx, "member@example.test", "oldpassword", "", ""); err == nil {
			t.Error("the old address still signs in")
		}
	})

	t.Run("an admin needs no password for someone else", func(t *testing.T) {
		if err := svc.SetEmail(ctx, adminID, memberID, "moved@example.test", ""); err != nil {
			t.Errorf("admin change: %v", err)
		}
	})

	t.Run("an SSO-only account cannot change its own", func(t *testing.T) {
		err := svc.SetEmail(ctx, oidcOnly, oidcOnly, "elsewhere@example.test", "")
		if !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("err = %v, want ErrValidation naming the provider", err)
		}
		// ...but an admin still can.
		if err := svc.SetEmail(ctx, adminID, oidcOnly, "elsewhere@example.test", ""); err != nil {
			t.Errorf("admin override: %v", err)
		}
	})

	t.Run("taking another account's address conflicts", func(t *testing.T) {
		if err := svc.SetEmail(ctx, adminID, memberID, "admin@example.test", ""); !errors.Is(err, httpx.ErrConflict) {
			t.Errorf("err = %v, want ErrConflict", err)
		}
	})

	t.Run("it cannot be cleared", func(t *testing.T) {
		if err := svc.SetEmail(ctx, adminID, memberID, "", ""); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("err = %v, want ErrValidation — email is the identifier", err)
		}
	})

	t.Run("a non-admin cannot change someone else's", func(t *testing.T) {
		if err := svc.SetEmail(ctx, memberID, adminID, "x@example.test", ""); !errors.Is(err, httpx.ErrForbidden) {
			t.Errorf("err = %v, want ErrForbidden", err)
		}
	})
}

func TestSetMatchWatchlist(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "hunter2hunter2", false)

	// Off by default: sharing is consent, and consent is given, not assumed.
	row, err := getUserByID(ctx, database, memberID)
	if err != nil {
		t.Fatal(err)
	}
	if row.MatchWatchlist {
		t.Fatal("match_watchlist defaults to on, want off")
	}

	if err := svc.SetMatchWatchlist(ctx, memberID, memberID, true); err != nil {
		t.Fatalf("self opt-in: %v", err)
	}
	// The flag travels on the session probe, so the Settings switch renders
	// the stored state.
	token := mintSession(t, database, memberID)
	status, err := svc.Status(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if status.User == nil || !status.User.MatchWatchlist {
		t.Errorf("session user = %+v, want match_watchlist true", status.User)
	}

	// Idempotent, and off again is off.
	if err := svc.SetMatchWatchlist(ctx, memberID, memberID, true); err != nil {
		t.Errorf("repeat opt-in: %v", err)
	}
	if err := svc.SetMatchWatchlist(ctx, memberID, memberID, false); err != nil {
		t.Fatalf("opt-out: %v", err)
	}
	if row, _ := getUserByID(ctx, database, memberID); row.MatchWatchlist {
		t.Error("opt-out did not stick")
	}

	// SELF ONLY: unlike the display name, an admin cannot flip it for someone
	// else in either direction.
	if err := svc.SetMatchWatchlist(ctx, adminID, memberID, true); !errors.Is(err, httpx.ErrForbidden) {
		t.Errorf("admin opting another in = %v, want ErrForbidden", err)
	}
	if err := svc.SetMatchWatchlist(ctx, memberID, adminID, true); !errors.Is(err, httpx.ErrForbidden) {
		t.Errorf("member acting on admin = %v, want ErrForbidden", err)
	}
}

func TestSetTimezone(t *testing.T) {
	t.Parallel()
	svc, database := testService(t)
	svc.defaultTimezone = "Europe/Bucharest"
	ctx := context.Background()
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "hunter2hunter2", false)
	token := mintSession(t, database, memberID)

	probe := func() *User {
		t.Helper()
		status, err := svc.Status(ctx, token)
		if err != nil || status.User == nil {
			t.Fatalf("probe: %+v, %v", status, err)
		}
		return status.User
	}

	// Unset: the choice is null and the server default is what is in force.
	if u := probe(); u.Timezone != nil || u.EffectiveTimezone != "Europe/Bucharest" {
		t.Errorf("unset = %v / %q, want nil / the server default", u.Timezone, u.EffectiveTimezone)
	}

	ny := " America/New_York "
	if err := svc.SetTimezone(ctx, memberID, memberID, &ny); err != nil {
		t.Fatalf("set: %v", err)
	}
	if u := probe(); u.Timezone == nil || *u.Timezone != "America/New_York" || u.EffectiveTimezone != "America/New_York" {
		t.Errorf("after set = %v / %q, want the trimmed choice in force", u.Timezone, u.EffectiveTimezone)
	}

	for _, bad := range []string{"Europe/Atlantis", "", "Local"} {
		if err := svc.SetTimezone(ctx, memberID, memberID, &bad); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("SetTimezone(%q) = %v, want ErrValidation", bad, err)
		}
	}

	// SELF ONLY, as the matching consent: an admin cannot move someone's day.
	if err := svc.SetTimezone(ctx, adminID, memberID, &ny); !errors.Is(err, httpx.ErrForbidden) {
		t.Errorf("admin on another = %v, want ErrForbidden", err)
	}

	// null clears back to the default.
	if err := svc.SetTimezone(ctx, memberID, memberID, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if u := probe(); u.Timezone != nil || u.EffectiveTimezone != "Europe/Bucharest" {
		t.Errorf("after clear = %v / %q, want nil / the server default", u.Timezone, u.EffectiveTimezone)
	}
}
