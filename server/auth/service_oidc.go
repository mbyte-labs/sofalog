package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

// flowTTL bounds how long a started sign-in may take to come back. Ten
// minutes is generous for typing a password and doing MFA, and short enough
// that a stale flow cookie is not a lingering credential.
const flowTTL = 10 * time.Minute

// OIDCSettings is what main.go hands the auth feature — config.OIDCConfig
// flattened, so this package does not import config.
type OIDCSettings struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
	AdminClaim   string
	AdminValue   string
	ProviderName string
}

// OIDC owns the provider connection. Discovery is attempted once at startup
// and, if the IdP is not up yet, retried lazily on first use — an IdP in the
// same compose stack often starts after this app does, and crash-looping on
// that would be worse than a first sign-in that fails.
type OIDC struct {
	settings OIDCSettings

	mu       sync.Mutex
	provider *oidc.Provider
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func NewOIDC(settings OIDCSettings) *OIDC { return &OIDC{settings: settings} }

func (o *OIDC) ProviderName() string { return o.settings.ProviderName }

// RedirectURI is exposed so main.go can log it: pasting the wrong callback
// into the IdP is the single most common setup mistake.
func (o *OIDC) RedirectURI() string { return o.settings.RedirectURI }

// Discover connects to the provider. Safe to call repeatedly; the first
// success is cached.
func (o *OIDC) Discover(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider != nil {
		return nil
	}
	provider, err := oidc.NewProvider(ctx, o.settings.Issuer)
	if err != nil {
		return fmt.Errorf("oidc discovery for %s: %w", o.settings.Issuer, err)
	}
	o.provider = provider
	o.oauth = &oauth2.Config{
		ClientID:     o.settings.ClientID,
		ClientSecret: o.settings.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  o.settings.RedirectURI,
		Scopes:       o.settings.Scopes,
	}
	o.verifier = provider.Verifier(&oidc.Config{ClientID: o.settings.ClientID})
	return nil
}

func (o *OIDC) ready(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	if err := o.Discover(ctx); err != nil {
		// The IdP is unreachable or misconfigured — the same class as a
		// provider outage, and the operator needs to see it as such.
		return nil, nil, fmt.Errorf("%w: %v", httpx.ErrProviderUnavailable, err)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.oauth, o.verifier, nil
}

// Flow is the per-sign-in state handed to the browser in a short-lived
// cookie: it is what proves the callback belongs to a sign-in this server
// actually started.
type Flow struct {
	State    string
	Nonce    string
	Verifier string
	Next     string
	Expires  time.Time
}

// StartLogin builds the authorize URL and the flow state to remember. PKCE is
// used even though this is a confidential client — it costs nothing and
// removes the authorization-code interception class of attack entirely.
func (s *Service) StartLogin(ctx context.Context, next string) (string, Flow, error) {
	if s.oidc == nil {
		return "", Flow{}, fmt.Errorf("%w: single sign-on is not configured", httpx.ErrNotFound)
	}
	conf, _, err := s.oidc.ready(ctx)
	if err != nil {
		return "", Flow{}, err
	}
	state, err := randomToken()
	if err != nil {
		return "", Flow{}, err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", Flow{}, err
	}
	verifier := oauth2.GenerateVerifier()

	flow := Flow{
		State:    state,
		Nonce:    nonce,
		Verifier: verifier,
		Next:     next,
		Expires:  s.now().UTC().Add(flowTTL),
	}
	url := conf.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	)
	return url, flow, nil
}

// CompleteLogin exchanges the code, verifies the ID token, resolves the
// account and starts a session — the same session rows and cookie local login
// produces, because OIDC is an alternate sign-in method and not a second auth
// architecture (SPEC §Authentication).
func (s *Service) CompleteLogin(ctx context.Context, flow Flow, state, code, ip, userAgent string) (User, Session, error) {
	if s.oidc == nil {
		return User{}, Session{}, fmt.Errorf("%w: single sign-on is not configured", httpx.ErrNotFound)
	}
	if flow.State == "" || s.now().UTC().After(flow.Expires) {
		return User{}, Session{}, fmt.Errorf("%w: the sign-in took too long, please try again", httpx.ErrValidation)
	}
	// Constant-time is unnecessary here (the state is single-use and both
	// halves are ours) but the comparison must still happen before anything
	// touches the code: this is the CSRF guard for the whole flow.
	if state == "" || state != flow.State {
		return User{}, Session{}, fmt.Errorf("%w: sign-in state did not match, please try again", httpx.ErrValidation)
	}
	if code == "" {
		return User{}, Session{}, fmt.Errorf("%w: the provider returned no authorization code", httpx.ErrValidation)
	}

	conf, verifier, err := s.oidc.ready(ctx)
	if err != nil {
		return User{}, Session{}, err
	}
	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return User{}, Session{}, fmt.Errorf("%w: exchanging the authorization code failed", httpx.ErrUnauthorized)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		return User{}, Session{}, fmt.Errorf("%w: the provider returned no ID token", httpx.ErrUnauthorized)
	}
	idToken, err := verifier.Verify(ctx, rawID)
	if err != nil {
		return User{}, Session{}, fmt.Errorf("%w: the ID token failed verification", httpx.ErrUnauthorized)
	}
	if idToken.Nonce != flow.Nonce {
		return User{}, Session{}, fmt.Errorf("%w: the ID token nonce did not match", httpx.ErrUnauthorized)
	}

	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		return User{}, Session{}, fmt.Errorf("reading ID token claims: %w", err)
	}

	user, err := s.resolveIdentity(ctx, idToken.Subject, claims)
	if err != nil {
		return User{}, Session{}, err
	}

	sess, err := newSession(s.now())
	if err != nil {
		return User{}, Session{}, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	err = s.db.InTx(ctx, func(tx db.Tx) error {
		if err := deleteExpiredSessions(ctx, tx, now); err != nil {
			return err
		}
		return insertSession(ctx, tx, sess.Token, user.ID, now,
			sess.ExpiresAt.Format(time.RFC3339), userAgent, ip)
	})
	if err != nil {
		return User{}, Session{}, err
	}
	return s.present(user), sess, nil
}

// resolveIdentity maps a verified ID token onto an account, in three steps:
// a known subject, an account with the same VERIFIED email, or a new one.
func (s *Service) resolveIdentity(ctx context.Context, subject string, claims map[string]any) (User, error) {
	isAdmin := s.claimGrantsAdmin(claims)
	now := s.now().UTC().Format(time.RFC3339)

	email, _ := claims["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	verified, _ := claims["email_verified"].(bool)

	// 1. A subject we have seen before. The subject is the only identifier
	// the IdP guarantees stable across email and name changes, so a returning
	// user is recognised even if their address moved.
	row, err := getUserByOIDCSubject(ctx, s.db, subject)
	if err == nil {
		return s.finishLogin(ctx, row, isAdmin, "", now, false, verifiedEmail(email, verified))
	}
	if !errors.Is(err, httpx.ErrNotFound) {
		return User{}, err
	}

	// Past this point an account has to be found or created by email, and
	// email is the identifier — so without a verified one there is nothing to
	// key on. Refuse clearly: the usual cause is a missing "email" scope, and
	// a vague failure here is maddening to debug.
	if email == "" {
		return User{}, fmt.Errorf("%w: the provider returned no email address; add the \"email\" scope to the client", httpx.ErrValidation)
	}
	if !verified {
		// Honouring an unverified address would let anyone who can set their
		// own email at the IdP seize a sofalog account.
		return User{}, fmt.Errorf("%w: the provider has not verified %s, so it cannot be used to sign in", httpx.ErrValidation, email)
	}

	// 2. Attach to an existing account with that address.
	existing, err := getUserByEmail(ctx, s.db, email)
	switch {
	case err == nil:
		return s.finishLogin(ctx, existing, isAdmin, subject, now, true, "")
	case !errors.Is(err, httpx.ErrNotFound):
		return User{}, err
	}

	// 3. Provision. No username to derive and no collision to resolve — the
	// email is already unique, which is the whole point of D-019.
	var created User
	err = s.db.InTx(ctx, func(tx db.Tx) error {
		id, err := createUser(ctx, tx, email, displayNameFromClaims(claims), "", isAdmin, now)
		if err != nil {
			return err
		}
		if err := linkOIDCIdentity(ctx, tx, id, subject, now); err != nil {
			return err
		}
		created = User{ID: id, Email: email, DisplayName: displayNameFromClaims(claims), IsAdmin: isAdmin}
		return nil
	})
	return created, err
}

// verifiedEmail returns the address only when the provider vouched for it.
func verifiedEmail(email string, verified bool) string {
	if verified {
		return email
	}
	return ""
}

// finishLogin applies the per-login side effects shared by the returning-user
// and email-link paths. Admin is recomputed from the claim every time, so
// removing someone from the IdP group demotes them on their next sign-in.
//
// syncEmail, when non-empty, re-points the account at the address the IdP now
// reports. A conflict (another account already holds it) is ignored rather
// than failing the sign-in: the subject is the stable link, so a stale
// address is a display wart, not a lockout.
func (s *Service) finishLogin(ctx context.Context, row userRow, isAdmin bool, linkSubject, now string, link bool, syncEmail string) (User, error) {
	email := row.Email
	err := s.db.InTx(ctx, func(tx db.Tx) error {
		if link {
			if err := linkOIDCIdentity(ctx, tx, row.ID, linkSubject, now); err != nil {
				return err
			}
		}
		if syncEmail != "" && syncEmail != row.Email {
			switch err := updateUserEmail(ctx, tx, row.ID, syncEmail); {
			case err == nil:
				email = syncEmail
			case errors.Is(err, httpx.ErrConflict): // taken; keep the old one
			default:
				return err
			}
		}
		return setUserAdmin(ctx, tx, row.ID, isAdmin)
	})
	if err != nil {
		return User{}, err
	}
	return User{ID: row.ID, Email: email, DisplayName: row.DisplayName.String, IsAdmin: isAdmin, MatchWatchlist: row.MatchWatchlist,
		Timezone: nullableString(row.Timezone)}, nil
}

// claimGrantsAdmin reads the configured group/role claim. It accepts both a
// list (the usual shape for "groups") and a single string, because providers
// disagree. With no claim configured nobody is an admin.
func (s *Service) claimGrantsAdmin(claims map[string]any) bool {
	claim, want := s.oidc.settings.AdminClaim, s.oidc.settings.AdminValue
	if claim == "" || want == "" {
		return false
	}
	switch v := claims[claim].(type) {
	case string:
		return v == want
	case []any:
		for _, item := range v {
			if str, ok := item.(string); ok && str == want {
				return true
			}
		}
	case []string:
		for _, str := range v {
			if str == want {
				return true
			}
		}
	}
	return false
}

// displayNameFromClaims picks a human label. No sanitising and no collision
// handling: it is a label, not an identifier (D-019), so whatever the IdP
// calls this person is fine as-is. Empty is fine too — the UI falls back to
// the email's local part.
func displayNameFromClaims(claims map[string]any) string {
	for _, key := range []string{"name", "preferred_username", "nickname"} {
		if v, ok := claims[key].(string); ok {
			if v = strings.TrimSpace(v); v != "" {
				if len(v) > 64 {
					v = v[:64]
				}
				return v
			}
		}
	}
	return ""
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
