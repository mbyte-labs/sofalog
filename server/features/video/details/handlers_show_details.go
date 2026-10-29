package details

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// A show details view serves the stored snapshot for a tracked show and goes
// live to TMDB for one that is not (D-015). Like the movie handler it can
// therefore cause a write — a stale snapshot is refreshed in place, and a
// successful refresh enqueues whatever artwork the new snapshot names — and it
// needs the user id, because a tracked show's page carries that user's status,
// progress and rating.
func (h *Handler) getShowDetails(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	details, err := h.svc.ShowDetails(r.Context(), httpx.UserIDFrom(r.Context()), ShowDetailsParams{
		ID:     q.Get("id"),
		TMDBID: q.Get("tmdb_id"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, details)
}
