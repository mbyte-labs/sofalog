package lists

import (
	"net/http"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

func (h *Handler) postWatchlist(w http.ResponseWriter, r *http.Request) {
	var p common.TrackParams
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := h.svc.WatchlistAdd(r.Context(), httpx.UserIDFrom(r.Context()), p)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) deleteWatchlist(w http.ResponseWriter, r *http.Request) {
	err := h.svc.WatchlistRemove(r.Context(), httpx.UserIDFrom(r.Context()),
		r.PathValue("target_type"), r.PathValue("target_id"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// The watchlist page is one query over the watchlist table; the service owns
// defaulting, validation, ordering and paging. The user id comes from the
// session middleware.
func (h *Handler) getWatchlist(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entries, err := h.svc.Watchlist(r.Context(), httpx.UserIDFrom(r.Context()), WatchlistParams{
		OrderBy: q.Get("order_by"),
		Order:   q.Get("order"),
		Type:    q.Get("type"),
		Limit:   q.Get("limit"),
		Offset:  q.Get("offset"),
		// Repeated, not comma-joined: `?match_user=3&match_user=7`.
		MatchUsers: q["match_user"],
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entries)
}

// getWatchlistMatchUsers lists the accounts the caller may intersect their
// watchlist with — those that opted in (D-028). Not the admin user list: this
// carries id and label only.
func (h *Handler) getWatchlistMatchUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.WatchlistMatchUsers(r.Context(), httpx.UserIDFrom(r.Context()))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, users)
}
