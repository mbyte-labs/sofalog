package stats

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// getStats serves the whole stats page for one period. The parameter passes
// through as received; the service defaults and validates it, so no domain
// rule lives here.
func (h *Handler) getStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.svc.Stats(r.Context(), httpx.UserIDFrom(r.Context()), StatsParams{
		Period: r.URL.Query().Get("period"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
}
