package upcoming

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// getUpcoming serves the calendar of dated things this user is waiting for.
// The parameters pass through as received; the service defaults and validates
// them, so no domain rule lives here.
func (h *Handler) getUpcoming(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entries, err := h.svc.Upcoming(r.Context(), httpx.UserIDFrom(r.Context()), UpcomingParams{
		Type: q.Get("type"),
		Days: q.Get("days"),
		From: q.Get("from"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entries)
}
