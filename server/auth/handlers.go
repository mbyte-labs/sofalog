package auth

import (
	"errors"
	"net/http"
	"strconv"

	"mbyte.ro/sofalog/httpx"
)

// sessionCookieName is the one cookie the app sets. Its value is the opaque
// sessions.token; everything about the user lives server-side.
const sessionCookieName = "sofalog_session"

// CookieConfig carries the two deployment-dependent cookie attributes: Path
// comes from BASE_URL's sub-path and Secure from its scheme (never from the
// listener or X-Forwarded-Proto).
type CookieConfig struct {
	Path   string
	Secure bool
}

type Handler struct {
	svc    *Service
	cookie CookieConfig
}

func NewHandler(svc *Service, cookie CookieConfig) *Handler {
	return &Handler{svc: svc, cookie: cookie}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/session", h.getSession)
	mux.HandleFunc("POST /api/v1/auth/setup", h.postSetup)
	mux.HandleFunc("POST /api/v1/auth/login", h.postLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", h.postLogout)
	mux.HandleFunc("POST /api/v1/auth/users", h.postUsers)
	mux.HandleFunc("GET /api/v1/auth/users", h.getUsers)
	mux.HandleFunc("PUT /api/v1/auth/users/{id}/password", h.putUserPassword)
	mux.HandleFunc("DELETE /api/v1/auth/users/{id}", h.deleteUser)
	mux.HandleFunc("PUT /api/v1/auth/users/{id}/email", h.putUserEmail)
	mux.HandleFunc("PUT /api/v1/auth/users/{id}/display-name", h.putUserDisplayName)
	mux.HandleFunc("PUT /api/v1/auth/users/{id}/match-watchlist", h.putUserMatchWatchlist)
	mux.HandleFunc("PUT /api/v1/auth/users/{id}/timezone", h.putUserTimezone)

	// Browser-navigation endpoints: these answer with redirects, not JSON.
	mux.HandleFunc("GET /api/v1/auth/oidc/start", h.getOIDCStart)
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", h.getOIDCCallback)
}

// getSession is the SPA boot probe: public, always 200, answers both "does
// this instance need first-run setup?" and "who am I?" in one round-trip.
func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.Status(r.Context(), cookieToken(r))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

// credentialsRequest is the body for setup, login and user creation. Email is
// the identifier; display_name is an optional label ignored on login.
type credentialsRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func (h *Handler) postSetup(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	user, sess, err := h.svc.Setup(r.Context(), req.Email, req.DisplayName, req.Password, clientIP(r), r.UserAgent())
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	setSessionCookie(w, h.cookie, sess.Token)
	httpx.WriteJSON(w, http.StatusCreated, map[string]User{"user": user})
}

func (h *Handler) postLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	user, sess, err := h.svc.Login(r.Context(), req.Email, req.Password, clientIP(r), r.UserAgent())
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	setSessionCookie(w, h.cookie, sess.Token)
	httpx.WriteJSON(w, http.StatusOK, map[string]User{"user": user})
}

// postLogout is public and idempotent: an expired or missing session still
// gets its cookie cleared instead of a 401.
func (h *Handler) postLogout(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Logout(r.Context(), cookieToken(r)); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	clearSessionCookie(w, h.cookie)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) postUsers(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	user, err := h.svc.CreateUser(r.Context(), httpx.UserIDFrom(r.Context()), req.Email, req.DisplayName, req.Password)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]User{"user": user})
}

// cookieToken returns the session token from the request cookie, or "".
func cookieToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// clientIP renders the middleware-resolved client IP for the sessions row.
func clientIP(r *http.Request) string {
	ip := httpx.ClientIPFrom(r.Context())
	if !ip.IsValid() {
		return ""
	}
	return ip.String()
}

func setSessionCookie(w http.ResponseWriter, cfg CookieConfig, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     cfg.Path,
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, cfg CookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     cfg.Path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) getUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers(r.Context(), httpx.UserIDFrom(r.Context()))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]UserListEntry{"users": users})
}

func (h *Handler) putUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	// cookieToken is what keeps THIS device signed in while the target's
	// other sessions are revoked.
	err = h.svc.SetPassword(r.Context(), httpx.UserIDFrom(r.Context()), id,
		req.CurrentPassword, req.NewPassword, cookieToken(r))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := h.svc.DeleteUser(r.Context(), httpx.UserIDFrom(r.Context()), id); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, errors.New("user id must be an integer")
	}
	return id, nil
}

// putUserEmail sets or clears an account's email. Available in both modes:
// on a local instance it prepares accounts for a future OIDC switch, and on
// an SSO instance it is how an unlinked account gets an address to match.
func (h *Handler) putUserEmail(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req struct {
		Email string `json:"email"`
		// Required when changing your OWN address, ignored when an admin
		// changes someone else's — the same rule the password change uses.
		CurrentPassword string `json:"current_password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.svc.SetEmail(r.Context(), httpx.UserIDFrom(r.Context()), id, req.Email, req.CurrentPassword); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putUserDisplayName sets or clears the human label. Self or admin, and no
// password guard: it is cosmetic and changes nothing about how you sign in.
func (h *Handler) putUserDisplayName(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.svc.SetDisplayName(r.Context(), httpx.UserIDFrom(r.Context()), id, req.DisplayName); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putUserMatchWatchlist flips the "Allow matching watchlist" consent (D-028).
// Self only — the service refuses an admin acting on another account.
func (h *Handler) putUserMatchWatchlist(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req struct {
		MatchWatchlist bool `json:"match_watchlist"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.svc.SetMatchWatchlist(r.Context(), httpx.UserIDFrom(r.Context()), id, req.MatchWatchlist); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// putUserTimezone sets the account's IANA zone, or clears it back to the
// server default with null (D-038). Self only, like the matching consent.
func (h *Handler) putUserTimezone(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req struct {
		Timezone *string `json:"timezone"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.svc.SetTimezone(r.Context(), httpx.UserIDFrom(r.Context()), id, req.Timezone); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
