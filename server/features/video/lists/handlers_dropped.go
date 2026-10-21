package lists

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// getDropped serves the dropped shows list. The service owns defaulting,
// validation, ordering and paging; the user id comes from the session
// middleware.
func (h *Handler) getDropped(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entries, err := h.svc.Dropped(r.Context(), httpx.UserIDFrom(r.Context()), DroppedParams{
		OrderBy: q.Get("order_by"),
		Order:   q.Get("order"),
		Limit:   q.Get("limit"),
		Offset:  q.Get("offset"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entries)
}
