package auth

import (
	"errors"
	"net/http"
	"strings"

	"mbyte.ro/sofalog/httpx"
)

// publicAPI lists the only API operations reachable without a session. Keyed
// "METHOD path" — everything else under /api/ fails closed with a 401, so a
// future route that forgets auth is inaccessible rather than exposed.
var publicAPI = map[string]bool{
	"GET /api/v1/health":       true,
	"GET /api/v1/auth/session": true,
	"POST /api/v1/auth/setup":  true,
	"POST /api/v1/auth/login":  true,
	"POST /api/v1/auth/logout": true,
	// The sign-in round trip: both legs happen before any session exists.
	"GET /api/v1/auth/oidc/start":    true,
	"GET /api/v1/auth/oidc/callback": true,
}

// Sessions is the auth middleware. It resolves the session cookie into a user
// id on the context (any path), slides the session expiry when due, and
// enforces authentication on every /api/ route not in publicAPI. Non-API
// paths always pass — the SPA shell must load so the login page can render.
func Sessions(svc *Service, cookie CookieConfig) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token := cookieToken(r); token != "" {
				user, refreshed, err := svc.Authenticate(r.Context(), token)
				switch {
				case err == nil:
					r = r.WithContext(httpx.WithUserID(r.Context(), user.ID))
					if refreshed != nil {
						// Re-issue before next writes the response header.
						setSessionCookie(w, cookie, token)
					}
				case errors.Is(err, httpx.ErrUnauthorized):
					// Invalid or expired token: drop the cookie so the
					// browser stops sending it, then continue as anonymous —
					// enforcement below decides whether that is enough.
					clearSessionCookie(w, cookie)
				default:
					httpx.WriteServiceError(w, r, err)
					return
				}
			}

			if strings.HasPrefix(r.URL.Path, "/api/") &&
				httpx.UserIDFrom(r.Context()) == 0 &&
				!publicAPI[r.Method+" "+r.URL.Path] {
				httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
