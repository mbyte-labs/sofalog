package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

// fakeIDP is a complete OpenID provider in-process: discovery, JWKS and a
// token endpoint, signing real RS256 ID tokens with a key generated per test.
// go-oidc talks to it exactly as it would to Keycloak, so the whole flow is
// exercised without a network — the "no network in tests, ever" rule holds.
//
// The JWT is assembled by hand rather than with a JOSE library: it is twenty
// lines, and it keeps the test suite from promoting a transitive dependency
// into a direct one.
type fakeIDP struct {
	server *httptest.Server
	key    *rsa.PrivateKey

	// claims is what the next issued ID token carries, minus iss/aud/exp/sub,
	// which are filled in automatically. Tests mutate it between sign-ins.
	claims  map[string]any
	subject string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating idp key: %v", err)
	}
	idp := &fakeIDP{key: key, claims: map[string]any{}, subject: "subject-1"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                idp.issuer(),
			"authorization_endpoint":                idp.issuer() + "/authorize",
			"token_endpoint":                        idp.issuer() + "/token",
			"jwks_uri":                              idp.issuer() + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		pub := idp.key.Public().(*rsa.PublicKey)
		writeJSON(w, map[string]any{"keys": []map[string]any{{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": "test-key",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		// PKCE must actually be in flight; asserting it here is what keeps a
		// regression that silently drops the verifier from going unnoticed.
		if r.Form.Get("code_verifier") == "" {
			http.Error(w, "missing code_verifier", http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{
			"access_token": "access-token",
			"token_type":   "Bearer",
			"id_token":     idp.signIDToken(t),
		})
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *fakeIDP) issuer() string { return f.server.URL }

func (f *fakeIDP) signIDToken(t *testing.T) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss": f.issuer(),
		"aud": "sofalog",
		"sub": f.subject,
		"exp": now.Add(time.Hour).Unix(),
		"iat": now.Unix(),
	}
	for k, v := range f.claims {
		claims[k] = v
	}
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test-key"}

	seg := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshalling jwt segment: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signing := seg(header) + "." + seg(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("signing id token: %v", err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// oidcService wires a Service against the fake IdP.
func oidcService(t *testing.T, idp *fakeIDP, adminClaim, adminValue string) (*Service, *fakeIDP) {
	t.Helper()
	database := testDB(t)
	client := NewOIDC(OIDCSettings{
		Issuer:       idp.issuer(),
		ClientID:     "sofalog",
		ClientSecret: "s3cret",
		RedirectURI:  "https://media.example.com/api/v1/auth/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		AdminClaim:   adminClaim,
		AdminValue:   adminValue,
		ProviderName: "Keycloak",
	})
	svc := NewService(database, client, "UTC")
	return svc, idp
}

// echoNonce mirrors what a real provider does: the nonce travels to the IdP
// on the authorize request and comes back inside the ID token, which is what
// binds the token to this particular sign-in.
func (f *fakeIDP) echoNonce(t *testing.T, authURL string) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing the authorize url: %v", err)
	}
	f.claims["nonce"] = u.Query().Get("nonce")
}

// signIn drives a full start→callback round trip.
func signIn(t *testing.T, svc *Service, idp *fakeIDP) (User, Session, error) {
	t.Helper()
	ctx := context.Background()
	authURL, flow, err := svc.StartLogin(ctx, "/")
	if err != nil {
		return User{}, Session{}, err
	}
	idp.echoNonce(t, authURL)
	return svc.CompleteLogin(ctx, flow, flow.State, "auth-code", "10.0.0.9", "test-agent")
}

func TestOIDCProvisionsAndSignsIn(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	idp.claims = map[string]any{"name": "Mihai B.", "email": "mihai@example.com", "email_verified": true}
	svc, _ := oidcService(t, idp, "", "")

	user, sess, err := signIn(t, svc, idp)
	if err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if user.Email != "mihai@example.com" {
		t.Errorf("email = %q", user.Email)
	}
	// Taken verbatim — a label needs no sanitising (D-019).
	if user.DisplayName != "Mihai B." {
		t.Errorf("display name = %q, want the claim as-is", user.DisplayName)
	}
	if user.IsAdmin {
		t.Error("admin granted with no admin claim configured")
	}
	if sess.Token == "" {
		t.Fatal("no session created")
	}
	// The session must be the ordinary kind — OIDC is another way in, not a
	// second auth architecture.
	if _, _, err := svc.Authenticate(context.Background(), sess.Token); err != nil {
		t.Errorf("the OIDC session does not authenticate: %v", err)
	}

	// Signing in again reuses the account via oauth_identities.
	if _, _, err := signIn(t, svc, idp); err != nil {
		t.Fatalf("second sign-in: %v", err)
	}
	var n int
	if err := svc.db.ReadRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d users after two sign-ins by the same subject, want 1", n)
	}
}

func TestOIDCLinksByVerifiedEmailOnly(t *testing.T) {
	t.Parallel()

	t.Run("verified email attaches to the existing account", func(t *testing.T) {
		t.Parallel()
		idp := newFakeIDP(t)
		idp.claims = map[string]any{"name": "Someone Else", "email": "mihai@example.com", "email_verified": true}
		svc, _ := oidcService(t, idp, "", "")
		existing := seedUserWithPassword(t, svc.db, "mihai@example.com", "hunter2hunter2", false)

		user, _, err := signIn(t, svc, idp)
		if err != nil {
			t.Fatalf("sign-in: %v", err)
		}
		if user.ID != existing {
			t.Errorf("signed in as user %d, want the existing account %d", user.ID, existing)
		}
	})

	t.Run("unverified email is refused outright", func(t *testing.T) {
		t.Parallel()
		idp := newFakeIDP(t)
		// Same address, unverified. Previously this provisioned a separate
		// account; now email IS the identity, so there is nothing to key on
		// and honouring the claim would let anyone assert any address.
		idp.claims = map[string]any{"name": "Impostor", "email": "mihai@example.com", "email_verified": false}
		svc, _ := oidcService(t, idp, "", "")
		existing := seedUserWithPassword(t, svc.db, "mihai@example.com", "hunter2hunter2", false)

		if _, _, err := signIn(t, svc, idp); !errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		var n int
		if err := svc.db.ReadRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%d users after a refused sign-in, want just the existing one", n)
		}
		row, err := getUserByID(context.Background(), svc.db, existing)
		if err != nil || row.Email != "mihai@example.com" {
			t.Errorf("the existing account was disturbed: %+v %v", row, err)
		}
	})

	t.Run("a provider that sends no email at all is refused", func(t *testing.T) {
		t.Parallel()
		idp := newFakeIDP(t)
		// The usual cause is a missing "email" scope on the client.
		idp.claims = map[string]any{"name": "No Address"}
		svc, _ := oidcService(t, idp, "", "")
		_, _, err := signIn(t, svc, idp)
		if !errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		if !strings.Contains(err.Error(), "email") {
			t.Errorf("error should name the missing scope, got %v", err)
		}
	})
}

// The IdP stays authoritative: group membership is re-read on every sign-in.
func TestOIDCAdminClaimIsReevaluated(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	idp.claims = map[string]any{"name": "Boss", "email": "boss@example.com", "email_verified": true,
		"groups": []any{"staff", "sofalog-admins"}}
	svc, _ := oidcService(t, idp, "groups", "sofalog-admins")

	user, _, err := signIn(t, svc, idp)
	if err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if !user.IsAdmin {
		t.Fatal("group member was not granted admin")
	}

	// Removed from the group at the IdP → demoted on the next sign-in.
	idp.claims["groups"] = []any{"staff"}
	user, _, err = signIn(t, svc, idp)
	if err != nil {
		t.Fatalf("second sign-in: %v", err)
	}
	if user.IsAdmin {
		t.Error("still admin after being removed from the group")
	}
	row, err := getUserByID(context.Background(), svc.db, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.IsAdmin {
		t.Error("is_admin was not persisted as demoted")
	}
}

// Display names are labels, not identifiers (D-019): two accounts may share
// one. This replaces the old username-collision test, whose suffixing machinery
// no longer exists.
func TestOIDCDuplicateDisplayNamesAreFine(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	idp.claims = map[string]any{"name": "Mihai", "email": "mihai.one@example.com", "email_verified": true}
	svc, _ := oidcService(t, idp, "", "")
	seedUserWithPassword(t, svc.db, "mihai.two@example.com", "hunter2hunter2", false)
	seedDisplayName(t, svc, 1, "Mihai")

	user, _, err := signIn(t, svc, idp)
	if err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if user.DisplayName != "Mihai" {
		t.Errorf("display name = %q, want the duplicate accepted as-is", user.DisplayName)
	}
	if user.Email != "mihai.one@example.com" {
		t.Errorf("email = %q", user.Email)
	}
}

func TestOIDCFlowSecurity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("state mismatch is refused", func(t *testing.T) {
		t.Parallel()
		svc, _ := oidcService(t, newFakeIDP(t), "", "")
		_, flow, err := svc.StartLogin(ctx, "/")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := svc.CompleteLogin(ctx, flow, "not-the-state", "code", "", ""); err == nil {
			t.Fatal("a mismatched state was accepted")
		}
	})

	t.Run("an expired flow is refused", func(t *testing.T) {
		t.Parallel()
		svc, _ := oidcService(t, newFakeIDP(t), "", "")
		_, flow, err := svc.StartLogin(ctx, "/")
		if err != nil {
			t.Fatal(err)
		}
		flow.Expires = time.Now().UTC().Add(-time.Minute)
		if _, _, err := svc.CompleteLogin(ctx, flow, flow.State, "code", "", ""); err == nil {
			t.Fatal("an expired flow was accepted")
		}
	})

	t.Run("a missing flow is refused", func(t *testing.T) {
		t.Parallel()
		svc, _ := oidcService(t, newFakeIDP(t), "", "")
		if _, _, err := svc.CompleteLogin(ctx, Flow{}, "state", "code", "", ""); err == nil {
			t.Fatal("a callback with no flow was accepted")
		}
	})

	t.Run("nonce mismatch is refused", func(t *testing.T) {
		t.Parallel()
		idp := newFakeIDP(t)
		svc, _ := oidcService(t, idp, "", "")
		_, flow, err := svc.StartLogin(ctx, "/")
		if err != nil {
			t.Fatal(err)
		}
		// A token minted for a different sign-in must not be replayable here.
		idp.claims["nonce"] = "someone-elses-nonce"
		if _, _, err := svc.CompleteLogin(ctx, flow, flow.State, "code", "", ""); err == nil {
			t.Fatal("a token with the wrong nonce was accepted")
		}
	})
}

// An open redirect on a login callback is a phishing primitive.
func TestSafeNext(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"/video/history", "/video/history"},
		{"", "/"},
		{"//evil.example", "/"},
		{"https://evil.example", "/"},
		{"http://evil.example/x", "/"},
		{`/\evil.example`, "/"},
		{"video/history", "/"}, // not rooted
	}
	for _, tt := range tests {
		if got := safeNext(tt.in, "/"); got != tt.want {
			t.Errorf("safeNext(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// With a provider configured, every local-auth entry point is closed (D-018).
func TestLocalAuthDisabledUnderOIDC(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	svc, _ := oidcService(t, idp, "", "")
	ctx := context.Background()

	if _, _, err := svc.Setup(ctx, "mihai", "", "hunter2hunter2", "", ""); err == nil {
		t.Error("setup is still reachable")
	}
	if _, _, err := svc.Login(ctx, "mihai", "hunter2hunter2", "", ""); err == nil {
		t.Error("password login is still reachable")
	}
	admin := seedUserWithPassword(t, svc.db, "admin@example.test", "hunter2hunter2", true)
	if _, err := svc.CreateUser(ctx, admin, "kid", "", "hunter2hunter2"); err == nil {
		t.Error("local user creation is still reachable")
	}
	if err := svc.SetPassword(ctx, admin, admin, "hunter2hunter2", "brandnewpass", ""); err == nil {
		t.Error("password change is still reachable")
	}

	// Account management is NOT authentication and stays available.
	if _, err := svc.ListUsers(ctx, admin); err != nil {
		t.Errorf("listing users broke under OIDC: %v", err)
	}
	if err := svc.SetEmail(ctx, admin, admin, "admin@example.com", "hunter2hunter2"); err != nil {
		t.Errorf("setting an email broke under OIDC: %v", err)
	}

	// And the boot probe never asks for local setup.
	status, err := svc.Status(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.Status == "setup_required" {
		t.Error("the SSO instance asked for local first-run setup")
	}
	if !status.Auth.OIDC || status.Auth.Local || status.Auth.OIDCName != "Keycloak" {
		t.Errorf("auth methods = %+v", status.Auth)
	}
}

// The client secret must never reach an error string or the probe payload.
func TestOIDCSecretNeverLeaks(t *testing.T) {
	t.Parallel()
	idp := newFakeIDP(t)
	svc, _ := oidcService(t, idp, "", "")
	ctx := context.Background()

	status, err := svc.Status(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Error("the client secret is in the session probe payload")
	}

	// A failing exchange must not echo the secret either.
	_, flow, err := svc.StartLogin(ctx, "/")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompleteLogin(ctx, flow, "wrong-state", "code", "", ""); err != nil {
		if strings.Contains(err.Error(), "s3cret") {
			t.Errorf("the client secret leaked into an error: %v", err)
		}
	}
}

// seedDisplayName sets a label directly, standing in for a user having set
// one in the UI.
func seedDisplayName(t *testing.T, svc *Service, userID int64, name string) {
	t.Helper()
	err := svc.db.InTx(context.Background(), func(tx db.Tx) error {
		return updateDisplayName(context.Background(), tx, userID, name)
	})
	if err != nil {
		t.Fatalf("seeding display name: %v", err)
	}
}
