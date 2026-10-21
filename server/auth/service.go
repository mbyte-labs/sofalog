package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/tz"
)

const (
	// sessionTTL is how long a session lives without use.
	sessionTTL = 30 * 24 * time.Hour
	// refreshAfter is how much of the TTL must elapse before an authenticated
	// request slides the expiry forward — at most one UPDATE per session per
	// day instead of one per request.
	refreshAfter = 24 * time.Hour
)

// errInvalidCredentials is one shared value so unknown-email,
// wrong-password and no-local-password failures are byte-identical.
var errInvalidCredentials = fmt.Errorf("%w: invalid email or password", httpx.ErrUnauthorized)

type Service struct {
	now  func() time.Time
	db   *db.DB
	oidc *OIDC
	// defaultTimezone is DEFAULT_TIMEZONE (D-038): what a user who never
	// chose a zone is reported as being in.
	defaultTimezone string
}

// NewService builds the auth service. oidc is nil when no provider is
// configured, which is what keeps local authentication enabled.
// defaultTimezone is the already-validated server default zone.
func NewService(database *db.DB, oidc *OIDC, defaultTimezone string) *Service {
	return &Service{now: time.Now, db: database, oidc: oidc, defaultTimezone: defaultTimezone}
}

// present fills the derived EffectiveTimezone on a User about to leave the
// service — the user's own choice, else the server default. Every path that
// hands a User to a client goes through it, so the SPA never derives the
// fallback itself.
func (s *Service) present(u User) User {
	if u.Timezone != nil {
		u.EffectiveTimezone = *u.Timezone
	} else {
		u.EffectiveTimezone = s.defaultTimezone
	}
	return u
}

// localAuthEnabled reports whether username/password authentication is
// available. Configuring an IdP switches it off completely (D-018).
func (s *Service) localAuthEnabled() bool { return s.oidc == nil }

// errLocalAuthDisabled is returned by every local-auth entry point on an
// SSO-only instance. ErrNotFound (404) rather than 403: the capability does
// not exist in this configuration, and the message says why for whoever
// curls it.
func (s *Service) errLocalAuthDisabled() error {
	return fmt.Errorf("%w: local authentication is disabled; this instance signs in through %s",
		httpx.ErrNotFound, s.oidc.ProviderName())
}

// authMethods describes the sign-in options for the boot probe.
func (s *Service) authMethods() AuthMethods {
	if s.oidc == nil {
		return AuthMethods{Local: true}
	}
	return AuthMethods{OIDC: true, OIDCName: s.oidc.ProviderName()}
}

// User is the public shape of an account — what /auth/session and login
// return. Never carries the password hash.
type User struct {
	ID int64 `json:"id"`
	// Email is the login identifier.
	Email string `json:"email"`
	// DisplayName is the optional human label; empty until set. Clients fall
	// back to the email's local part rather than rendering nothing.
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	// MatchWatchlist is this account's consent to be picked in other users'
	// "Matching watchlist" filter (D-028). Only the account itself flips it,
	// and it travels on the session probe so the Settings switch renders the
	// stored state rather than a remembered one.
	MatchWatchlist bool `json:"match_watchlist"`
	// Timezone is the IANA zone this account chose (D-038); nil means it
	// follows the server's DEFAULT_TIMEZONE. EffectiveTimezone is the zone
	// actually in force — the one the SPA renders instants in and the server
	// measures this user's "today" in — so a client never resolves the
	// fallback on its own.
	Timezone          *string `json:"timezone"`
	EffectiveTimezone string  `json:"effective_timezone"`
}

// UserListEntry is one row of the admin user list. Deliberately separate from
// User: the session payload stays minimal, and this one carries the join date
// the list renders.
type UserListEntry struct {
	ID int64 `json:"id"`
	// Email is both the login identifier and what an OIDC sign-in matches
	// against to attach to an existing account.
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	CreatedAt   string `json:"created_at"`
	// Linked reports whether this account already has an SSO identity.
	Linked bool `json:"linked"`
}

// SessionStatus is the SPA boot probe result. Status is one of
// "setup_required", "unauthenticated", "authenticated"; User is set only when
// authenticated.
type SessionStatus struct {
	Status string `json:"status"`
	User   *User  `json:"user,omitempty"`
	// Auth tells the SPA which sign-in methods this instance offers, so the
	// login page renders a password form or an SSO button rather than
	// guessing. Additive: existing clients ignore it.
	Auth AuthMethods `json:"auth"`
}

// AuthMethods mirrors the deployment's auth configuration. Local and OIDC are
// mutually exclusive today (D-018): configuring a provider disables local
// authentication entirely.
type AuthMethods struct {
	Local    bool   `json:"local"`
	OIDC     bool   `json:"oidc"`
	OIDCName string `json:"oidc_name,omitempty"`
}

// Session is a freshly created session as handlers need it: the cookie value
// plus its expiry.
type Session struct {
	Token     string
	ExpiresAt time.Time
}

// Status answers the SPA boot probe: does the instance need first-run setup,
// and if not, who does this token belong to? Never an auth error — the probe
// is public and always 200.
func (s *Service) Status(ctx context.Context, token string) (SessionStatus, error) {
	methods := s.authMethods()

	// On an SSO instance there is no first-run form to show: the first
	// sign-in provisions the first account, so an empty database is simply
	// "unauthenticated".
	if s.localAuthEnabled() {
		n, err := countUsers(ctx, s.db)
		if err != nil {
			return SessionStatus{}, err
		}
		if n == 0 {
			return SessionStatus{Status: "setup_required", Auth: methods}, nil
		}
	}
	if token == "" {
		return SessionStatus{Status: "unauthenticated", Auth: methods}, nil
	}
	user, _, err := getSessionUser(ctx, s.db, token, s.now().UTC().Format(time.RFC3339))
	if errors.Is(err, httpx.ErrUnauthorized) {
		return SessionStatus{Status: "unauthenticated", Auth: methods}, nil
	}
	if err != nil {
		return SessionStatus{}, err
	}
	user = s.present(user)
	return SessionStatus{Status: "authenticated", User: &user, Auth: methods}, nil
}

// Setup creates the first account, as admin, and logs it in. Guarded so it
// works exactly once per instance: any existing user means ErrConflict.
func (s *Service) Setup(ctx context.Context, email, displayName, password, ip, userAgent string) (User, Session, error) {
	if !s.localAuthEnabled() {
		return User{}, Session{}, s.errLocalAuthDisabled()
	}
	email, err := validateEmail(email)
	if err != nil {
		return User{}, Session{}, err
	}
	displayName, err = validateDisplayName(displayName)
	if err != nil {
		return User{}, Session{}, err
	}
	if err := validatePassword(password); err != nil {
		return User{}, Session{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, Session{}, err
	}
	sess, err := newSession(s.now())
	if err != nil {
		return User{}, Session{}, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	var userID int64
	err = s.db.InTx(ctx, func(tx db.Tx) error {
		userID, err = createFirstAdmin(ctx, tx, email, displayName, hash, now)
		if err != nil {
			return err
		}
		return insertSession(ctx, tx, sess.Token, userID, now,
			sess.ExpiresAt.Format(time.RFC3339), userAgent, ip)
	})
	if err != nil {
		return User{}, Session{}, err
	}
	return s.present(User{ID: userID, Email: email, DisplayName: displayName, IsAdmin: true}), sess, nil
}

// Login verifies the password and starts a session. Every failure shape —
// unknown username, wrong password, user without a local password — costs one
// argon2 evaluation and returns the identical error, so neither timing nor
// message reveals which usernames exist.
func (s *Service) Login(ctx context.Context, email, password, ip, userAgent string) (User, Session, error) {
	if !s.localAuthEnabled() {
		return User{}, Session{}, s.errLocalAuthDisabled()
	}
	email = strings.ToLower(strings.TrimSpace(email))
	row, err := getUserByEmail(ctx, s.db, email)
	switch {
	case errors.Is(err, httpx.ErrNotFound):
		verifyPassword(dummyHash, password)
		return User{}, Session{}, errInvalidCredentials
	case err != nil:
		return User{}, Session{}, err
	case !row.PasswordHash.Valid:
		verifyPassword(dummyHash, password)
		return User{}, Session{}, errInvalidCredentials
	}
	ok, err := verifyPassword(row.PasswordHash.String, password)
	if err != nil {
		return User{}, Session{}, fmt.Errorf("verifying password for user %d: %w", row.ID, err)
	}
	if !ok {
		return User{}, Session{}, errInvalidCredentials
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
		return insertSession(ctx, tx, sess.Token, row.ID, now,
			sess.ExpiresAt.Format(time.RFC3339), userAgent, ip)
	})
	if err != nil {
		return User{}, Session{}, err
	}
	return s.present(userFromRow(row)), sess, nil
}

// Logout revokes the session. Idempotent: a missing or already-expired row is
// success, so an expired cookie can still "log out".
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.db.InTx(ctx, func(tx db.Tx) error {
		return deleteSession(ctx, tx, token)
	})
}

// Authenticate resolves a session token for the middleware. When the session
// is more than refreshAfter old (measured against its expiry) it slides
// expires_at forward and returns the new expiry so the cookie gets re-issued.
func (s *Service) Authenticate(ctx context.Context, token string) (User, *time.Time, error) {
	now := s.now().UTC()
	user, expiresAtStr, err := getSessionUser(ctx, s.db, token, now.Format(time.RFC3339))
	if err != nil {
		return User{}, nil, err
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		return User{}, nil, fmt.Errorf("parsing session expiry %q: %w", expiresAtStr, err)
	}
	if expiresAt.Sub(now) >= sessionTTL-refreshAfter {
		return user, nil, nil
	}
	refreshed := now.Add(sessionTTL)
	err = s.db.InTx(ctx, func(tx db.Tx) error {
		return touchSession(ctx, tx, token, refreshed.Format(time.RFC3339))
	})
	if err != nil {
		return User{}, nil, err
	}
	return user, &refreshed, nil
}

// CreateUser lets an admin add an account. Created users are never admins —
// there is no promotion path in M1.
func (s *Service) CreateUser(ctx context.Context, actorID int64, email, displayName, password string) (User, error) {
	if !s.localAuthEnabled() {
		// Accounts are provisioned by the IdP on first sign-in.
		return User{}, s.errLocalAuthDisabled()
	}
	actor, err := getUserByID(ctx, s.db, actorID)
	if err != nil {
		return User{}, err
	}
	if !actor.IsAdmin {
		return User{}, fmt.Errorf("%w: only an admin can create users", httpx.ErrForbidden)
	}
	email, err = validateEmail(email)
	if err != nil {
		return User{}, err
	}
	displayName, err = validateDisplayName(displayName)
	if err != nil {
		return User{}, err
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	var userID int64
	err = s.db.InTx(ctx, func(tx db.Tx) error {
		userID, err = createUser(ctx, tx, email, displayName, hash, false, now)
		return err
	})
	if err != nil {
		return User{}, err
	}
	return s.present(User{ID: userID, Email: email, DisplayName: displayName, IsAdmin: false}), nil
}

// SetEmail changes an account's login identifier. An admin may change anyone
// else's without knowing their password; changing your OWN requires the
// current one — the same rule SetPassword uses, for the same reason.
//
// An account with no local password cannot self-change: on an SSO instance
// the address belongs to the identity provider, and an admin is the only one
// who may override it.
func (s *Service) SetEmail(ctx context.Context, actorID, targetID int64, email, currentPassword string) error {
	actor, err := getUserByID(ctx, s.db, actorID)
	if err != nil {
		return err
	}
	self := actorID == targetID
	if !self && !actor.IsAdmin {
		return fmt.Errorf("%w: only an admin can change another account's email", httpx.ErrForbidden)
	}
	target := actor
	if !self {
		if target, err = getUserByID(ctx, s.db, targetID); err != nil {
			return err
		}
	}

	if self {
		if !target.PasswordHash.Valid {
			return fmt.Errorf("%w: this account signs in through the identity provider, which owns its email address", httpx.ErrValidation)
		}
		// A wrong password is a bad value in an authenticated request, not a
		// failed authentication — 401 here would make the SPA log the user
		// out for a typo.
		ok, err := verifyPassword(target.PasswordHash.String, currentPassword)
		if err != nil {
			return fmt.Errorf("verifying current password for user %d: %w", target.ID, err)
		}
		if !ok {
			return fmt.Errorf("%w: current password is incorrect", httpx.ErrValidation)
		}
	}

	email, err = validateEmail(email)
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx db.Tx) error {
		return updateUserEmail(ctx, tx, target.ID, email)
	})
}

// SetDisplayName sets or clears the human label. Self or admin; no password
// guard, because it is cosmetic and grants nothing.
func (s *Service) SetDisplayName(ctx context.Context, actorID, targetID int64, name string) error {
	actor, err := getUserByID(ctx, s.db, actorID)
	if err != nil {
		return err
	}
	if actorID != targetID && !actor.IsAdmin {
		return fmt.Errorf("%w: only an admin can rename another account", httpx.ErrForbidden)
	}
	if actorID != targetID {
		if _, err := getUserByID(ctx, s.db, targetID); err != nil {
			return err
		}
	}
	name, err = validateDisplayName(name)
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx db.Tx) error {
		return updateDisplayName(ctx, tx, targetID, name)
	})
}

// userFromRow is the one place a stored row becomes the public shape.
func userFromRow(row userRow) User {
	return User{
		ID:             row.ID,
		Email:          row.Email,
		DisplayName:    row.DisplayName.String,
		IsAdmin:        row.IsAdmin,
		MatchWatchlist: row.MatchWatchlist,
		Timezone:       nullableString(row.Timezone),
	}
}

// nullableString is a NULL-able column as an optional JSON string.
func nullableString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

// SetTimezone sets or clears this account's zone (D-038); nil clears it back
// to the server default. SELF ONLY like the D-028 flag: it is a personal
// preference that moves the account's own day, not something to manage on
// someone's behalf. The name is checked by tz.Load, the same rule
// DEFAULT_TIMEZONE passes at startup, so anything stored here loads later.
func (s *Service) SetTimezone(ctx context.Context, actorID, targetID int64, zone *string) error {
	if actorID != targetID {
		return fmt.Errorf("%w: only the account owner can change this setting", httpx.ErrForbidden)
	}
	if zone != nil {
		name := strings.TrimSpace(*zone)
		if _, err := tz.Load(name); err != nil {
			return fmt.Errorf("%w: %q is not a known time zone", httpx.ErrValidation, name)
		}
		zone = &name
	}
	if _, err := getUserByID(ctx, s.db, actorID); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx db.Tx) error {
		return setTimezone(ctx, tx, targetID, zone)
	})
}

// SetMatchWatchlist flips the D-028 consent flag. SELF ONLY — unlike the
// display name and email, there is no admin bypass: the flag is a consent to
// be visible to other users, and consent is not something an admin gives on
// someone's behalf. The target's existence is implied by the actor's session.
func (s *Service) SetMatchWatchlist(ctx context.Context, actorID, targetID int64, on bool) error {
	if actorID != targetID {
		return fmt.Errorf("%w: only the account owner can change this setting", httpx.ErrForbidden)
	}
	if _, err := getUserByID(ctx, s.db, actorID); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx db.Tx) error {
		return setMatchWatchlist(ctx, tx, targetID, on)
	})
}

// validateEmail normalises the login identifier. It is required — unlike
// before, an empty address is not "clear it" but a missing identifier. The
// check is deliberately shallow: an address is proven by a provider verifying
// it or by the owner signing in, not by a regex here.
func validateEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", fmt.Errorf("%w: an email address is required", httpx.ErrValidation)
	}
	at := strings.Index(email, "@")
	if at <= 0 || at == len(email)-1 || strings.ContainsAny(email, " \t") {
		return "", fmt.Errorf("%w: that does not look like an email address", httpx.ErrValidation)
	}
	return email, nil
}

// validateDisplayName normalises the optional label. Empty clears it, and it
// is never checked for uniqueness (D-019).
func validateDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) > 64 {
		return "", fmt.Errorf("%w: a display name may be at most 64 characters", httpx.ErrValidation)
	}
	return name, nil
}

// validatePassword is the one definition of the password rule, shared by
// account creation and every password change.
func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 128 {
		return fmt.Errorf("%w: password must be 8-128 characters", httpx.ErrValidation)
	}
	return nil
}

func newSession(now time.Time) (Session, error) {
	token, err := newSessionToken()
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: now.UTC().Add(sessionTTL)}, nil
}

// ListUsers returns every account. Admin only — a household member has no
// business enumerating the others.
func (s *Service) ListUsers(ctx context.Context, actorID int64) ([]UserListEntry, error) {
	actor, err := getUserByID(ctx, s.db, actorID)
	if err != nil {
		return nil, err
	}
	if !actor.IsAdmin {
		return nil, fmt.Errorf("%w: only an admin can list users", httpx.ErrForbidden)
	}
	return listUsers(ctx, s.db)
}

// SetPassword changes targetID's password. Two callers, one rule set:
// an admin may reset anyone else's password without knowing it, but changing
// your OWN password always requires the current one — admin included.
//
// currentToken is the requesting session's token: the target's other sessions
// are revoked, so a reset actually locks the old password's holder out while
// the device making the request stays signed in.
func (s *Service) SetPassword(ctx context.Context, actorID, targetID int64, currentPassword, newPassword, currentToken string) error {
	if !s.localAuthEnabled() {
		return s.errLocalAuthDisabled()
	}
	actor, err := getUserByID(ctx, s.db, actorID)
	if err != nil {
		return err
	}
	self := actorID == targetID
	if !self && !actor.IsAdmin {
		return fmt.Errorf("%w: only an admin can change another user's password", httpx.ErrForbidden)
	}

	target := actor
	if !self {
		if target, err = getUserByID(ctx, s.db, targetID); err != nil {
			return err
		}
	}

	if self {
		// A wrong current password is a bad VALUE in an authenticated
		// request, not a failed authentication — deliberately not
		// ErrUnauthorized, because the SPA's client treats every 401 as
		// session expiry and would log the user out for a typo.
		if !target.PasswordHash.Valid {
			return fmt.Errorf("%w: this account has no local password to change", httpx.ErrValidation)
		}
		ok, err := verifyPassword(target.PasswordHash.String, currentPassword)
		if err != nil {
			return fmt.Errorf("verifying current password for user %d: %w", target.ID, err)
		}
		if !ok {
			return fmt.Errorf("%w: current password is incorrect", httpx.ErrValidation)
		}
	}

	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx db.Tx) error {
		if err := updatePasswordHash(ctx, tx, target.ID, hash); err != nil {
			return err
		}
		return deleteUserSessions(ctx, tx, target.ID, currentToken)
	})
}

// DeleteUser removes an account and, by cascade, everything that user
// tracked. Admin only, and never yourself: that single rule also keeps the
// instance administerable, since the actor must be an admin and survives the
// delete, so the last admin can never be removed.
func (s *Service) DeleteUser(ctx context.Context, actorID, targetID int64) error {
	actor, err := getUserByID(ctx, s.db, actorID)
	if err != nil {
		return err
	}
	if !actor.IsAdmin {
		return fmt.Errorf("%w: only an admin can delete users", httpx.ErrForbidden)
	}
	if actorID == targetID {
		return fmt.Errorf("%w: you cannot delete your own account", httpx.ErrValidation)
	}
	if _, err := getUserByID(ctx, s.db, targetID); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx db.Tx) error {
		return deleteUserRow(ctx, tx, targetID)
	})
}
