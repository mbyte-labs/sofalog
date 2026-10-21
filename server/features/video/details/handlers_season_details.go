package details

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// Unlike a pure read, this GET can cause a write — the service materialises or
// refreshes the season's snapshot row (the recorded D-015 season deviation).
func (h *Handler) getSeasonDetails(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	details, err := h.svc.SeasonDetails(r.Context(), httpx.UserIDFrom(r.Context()), SeasonDetailsParams{
		ID:     q.Get("id"),
		TMDBID: q.Get("tmdb_id"),
		Season: q.Get("season"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, details)
}
