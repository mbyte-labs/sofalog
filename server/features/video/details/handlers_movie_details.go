package details

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The user id comes from the session middleware. An untracked tmdb_id is served live from TMDB and stores nothing; a
// tracked movie's stale snapshot refresh is the one write this GET can cause
// (D-015).
func (h *Handler) getMovieDetails(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	details, err := h.svc.MovieDetails(r.Context(), httpx.UserIDFrom(r.Context()), MovieDetailsParams{
		TMDBID: q.Get("tmdb_id"),
		ID:     q.Get("id"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, details)
}
