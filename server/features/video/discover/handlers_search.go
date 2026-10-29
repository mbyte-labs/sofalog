package discover

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// Unlike up-next and history this serves real TMDB data, not dummies —
// search has no per-user state to fake.
func (h *Handler) searchVideo(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	results, err := h.svc.Search(r.Context(), SearchParams{
		Q:    q.Get("q"),
		Type: q.Get("type"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, results)
}
