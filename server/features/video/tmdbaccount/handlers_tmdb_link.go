package tmdbaccount

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// tmdbLinkCookieName carries the in-progress link between start and callback.
// A cookie rather than a table: per-browser, short-lived, self-cleaning.
const tmdbLinkCookieName = "sofalog_tmdb_link"

func (h *Handler) getTMDBLink(w http.ResponseWriter, r *http.Request) {
	status, err := h.svc.TMDBLinkStatus(r.Context(), httpx.UserIDFrom(r.Context()))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

// getTMDBLinkStart asks the service for the approval URL, pins the flow in a
// cookie and sends the browser off. A failure lands on the result page with a
// code, like the callback's failures.
func (h *Handler) getTMDBLinkStart(w http.ResponseWriter, r *http.Request) {
	target, flow, err := h.svc.StartTMDBLink(r.Context(), httpx.UserIDFrom(r.Context()))
	if err != nil {
		httpx.LogServiceError(r, err)
		h.tmdbLinkResult(w, r, tmdbLinkResultCode(err))
		return
	}
	setTMDBLinkCookie(w, h.cookie, flow)
	http.Redirect(w, r, target, http.StatusFound)
}

// getTMDBLinkCallback is where TMDB returns the browser: request_token plus
// approved=true or denied=true in the query.
func (h *Handler) getTMDBLinkCallback(w http.ResponseWriter, r *http.Request) {
	flow, err := readTMDBLinkCookie(r)
	clearTMDBLinkCookie(w, h.cookie)
	if err != nil {
		h.tmdbLinkResult(w, r, "expired")
		return
	}
	q := r.URL.Query()
	approved := q.Get("approved") == "true" && q.Get("denied") != "true"
	err = h.svc.CompleteTMDBLink(r.Context(), httpx.UserIDFrom(r.Context()), flow, q.Get("request_token"), approved)
	if err != nil {
		if !errors.Is(err, errTMDBLinkDenied) {
			httpx.LogServiceError(r, err)
		}
		h.tmdbLinkResult(w, r, tmdbLinkResultCode(err))
		return
	}
	h.tmdbLinkResult(w, r, "ok")
}

// postTMDBLinkSetup is the one-time setup after linking (D-036): per kind,
// pull, push or skip, plus the ongoing switch. Answers 202 with the status —
// a pull runs on as a job the SPA polls through GET /video/tmdb/link.
func (h *Handler) postTMDBLinkSetup(w http.ResponseWriter, r *http.Request) {
	var p TMDBSetupParams
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	status, err := h.svc.SetupTMDBSync(r.Context(), httpx.UserIDFrom(r.Context()), p)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, status)
}

// putTMDBLinkSync writes the three ongoing-sync switches (D-036).
func (h *Handler) putTMDBLinkSync(w http.ResponseWriter, r *http.Request) {
	var k TMDBSyncKinds
	if err := httpx.DecodeJSON(r, &k); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	status, err := h.svc.SetTMDBSyncKinds(r.Context(), httpx.UserIDFrom(r.Context()), k)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (h *Handler) deleteTMDBLink(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.UnlinkTMDB(r.Context(), httpx.UserIDFrom(r.Context())); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tmdbLinkResult lands the browser on the Settings page that started the
// flow, with the outcome in the query. Relative to the cookie path, which is
// BASE_URL's path — never built from the request's Host.
func (h *Handler) tmdbLinkResult(w http.ResponseWriter, r *http.Request, code string) {
	dest := h.cookie.Path + "settings/tmdb?tmdb_link=" + url.QueryEscape(code)
	http.Redirect(w, r, dest, http.StatusFound)
}

// tmdbLinkResultCode maps a service error onto a short, stable string for the
// SPA.
func tmdbLinkResultCode(err error) string {
	switch {
	case errors.Is(err, errTMDBLinkDenied), errors.Is(err, provider.ErrSessionInvalid):
		return "denied"
	case errors.Is(err, httpx.ErrValidation):
		return "expired"
	case errors.Is(err, httpx.ErrProviderUnavailable):
		return "provider_unavailable"
	default:
		return "failed"
	}
}

func setTMDBLinkCookie(w http.ResponseWriter, cfg common.CookieSettings, flow TMDBLinkFlow) {
	raw, err := json.Marshal(flow)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     tmdbLinkCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     cfg.Path,
		MaxAge:   int(tmdbLinkFlowTTL.Seconds()),
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func readTMDBLinkCookie(r *http.Request) (TMDBLinkFlow, error) {
	c, err := r.Cookie(tmdbLinkCookieName)
	if err != nil {
		return TMDBLinkFlow{}, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return TMDBLinkFlow{}, err
	}
	var flow TMDBLinkFlow
	if err := json.Unmarshal(raw, &flow); err != nil {
		return TMDBLinkFlow{}, err
	}
	return flow, nil
}

func clearTMDBLinkCookie(w http.ResponseWriter, cfg common.CookieSettings) {
	http.SetCookie(w, &http.Cookie{
		Name:     tmdbLinkCookieName,
		Value:    "",
		Path:     cfg.Path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}
