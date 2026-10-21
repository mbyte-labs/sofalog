package lists

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// getLists serves the Lists hub's summary: one entry per list with its counts
// and a few backdrops. No parameters; the user id comes from the session
// middleware.
func (h *Handler) getLists(w http.ResponseWriter, r *http.Request) {
	lists, err := h.svc.Lists(r.Context(), httpx.UserIDFrom(r.Context()))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, lists)
}
