package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"mbyte.ro/sofalog/httpx"
)

// flowCookieName carries the in-progress sign-in. It is deliberately NOT a
// database row: the flow is per-browser, short-lived and self-cleaning, so a
// cookie needs no table and no sweeper. SameSite=Lax still reaches us on the
// top-level GET redirect back from the IdP.
const flowCookieName = "sofalog_oidc"

// getOIDCStart sends the browser to the provider. A redirect rather than JSON
// because this is browser navigation, not an API call the SPA can follow.
func (h *Handler) getOIDCStart(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"), h.cookie.Path)

	url, flow, err := h.svc.StartLogin(r.Context(), next)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	setFlowCookie(w, h.cookie, flow)
	http.Redirect(w, r, url, http.StatusFound)
}

// getOIDCCallback completes the flow and lands the browser back in the app.
// Errors redirect to the app with ?sso_error= rather than rendering JSON at
// the user: they arrived here by clicking a button, and a bare error envelope
// in the address bar is a dead end.
func (h *Handler) getOIDCCallback(w http.ResponseWriter, r *http.Request) {
	flow, err := readFlowCookie(r)
	clearFlowCookie(w, h.cookie)
	if err != nil {
		h.ssoFailure(w, r, "expired")
		return
	}

	q := r.URL.Query()
	// The provider reports its own refusals here (access_denied when someone
	// cancels at the consent screen, for instance).
	if e := q.Get("error"); e != "" {
		h.ssoFailure(w, r, e)
		return
	}

	user, sess, err := h.svc.CompleteLogin(r.Context(), flow, q.Get("state"), q.Get("code"),
		clientIP(r), r.UserAgent())
	if err != nil {
		httpx.LogServiceError(r, err)
		h.ssoFailure(w, r, ssoErrorCode(err))
		return
	}

	setSessionCookie(w, h.cookie, sess.Token)
	_ = user
	http.Redirect(w, r, flow.Next, http.StatusFound)
}

func (h *Handler) ssoFailure(w http.ResponseWriter, r *http.Request, code string) {
	dest := h.cookie.Path + "?sso_error=" + url.QueryEscape(code)
	http.Redirect(w, r, dest, http.StatusFound)
}

// ssoErrorCode maps a service error onto a short, stable string for the SPA.
func ssoErrorCode(err error) string {
	switch {
	case errors.Is(err, httpx.ErrProviderUnavailable):
		return "provider_unavailable"
	case errors.Is(err, httpx.ErrValidation):
		return "expired"
	case errors.Is(err, httpx.ErrUnauthorized):
		return "rejected"
	case errors.Is(err, httpx.ErrConflict):
		return "conflict"
	default:
		return "failed"
	}
}

// safeNext keeps the post-login redirect inside this app. Anything absolute,
// scheme-relative ("//evil.example") or otherwise foreign falls back to the
// app root — an open redirect on a login callback is a phishing primitive.
func safeNext(next, basePath string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return basePath
	}
	if strings.Contains(next, "\\") || strings.Contains(next, "://") {
		return basePath
	}
	return next
}

func setFlowCookie(w http.ResponseWriter, cfg CookieConfig, flow Flow) {
	raw, err := json.Marshal(flow)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     cfg.Path,
		MaxAge:   int(flowTTL.Seconds()),
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func readFlowCookie(r *http.Request) (Flow, error) {
	c, err := r.Cookie(flowCookieName)
	if err != nil {
		return Flow{}, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return Flow{}, err
	}
	var flow Flow
	if err := json.Unmarshal(raw, &flow); err != nil {
		return Flow{}, err
	}
	return flow, nil
}

func clearFlowCookie(w http.ResponseWriter, cfg CookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     flowCookieName,
		Value:    "",
		Path:     cfg.Path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}
