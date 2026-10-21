package history

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The whole feed and every narrowing of it — one item, one season, one show —
// are one query over the watch log; the service owns defaulting, validation,
// ordering and paging. The user id comes from the session middleware.
func (h *Handler) getHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entries, err := h.svc.History(r.Context(), httpx.UserIDFrom(r.Context()), HistoryParams{
		Order:      q.Get("order"),
		Type:       q.Get("type"),
		TargetType: q.Get("target_type"),
		TargetID:   q.Get("target_id"),
		SeasonID:   q.Get("season_id"),
		ShowID:     q.Get("show_id"),
		Limit:      q.Get("limit"),
		Offset:     q.Get("offset"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entries)
}
