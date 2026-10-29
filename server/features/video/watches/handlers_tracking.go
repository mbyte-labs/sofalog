package watches

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The tracking writes. The user id comes from the session middleware.

func (h *Handler) postWatch(w http.ResponseWriter, r *http.Request) {
	var p WatchParams
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := h.svc.MarkWatched(r.Context(), httpx.UserIDFrom(r.Context()), p)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) deleteWatch(w http.ResponseWriter, r *http.Request) {
	err := h.svc.DeleteWatch(r.Context(), httpx.UserIDFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// The drop pair. Show-only, so the media type is in the path and there is no
// polymorphic pair to decode; the body is empty in both directions.
func (h *Handler) postShowDrop(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.DropShow(r.Context(), httpx.UserIDFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

// Answers 200 with the recomputed state rather than the 204 the other removals
// use: un-dropping recomputes from the watch log, and the status it lands on is
// not derivable on the client (D-010).
func (h *Handler) deleteShowDrop(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.UndropShow(r.Context(), httpx.UserIDFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}
