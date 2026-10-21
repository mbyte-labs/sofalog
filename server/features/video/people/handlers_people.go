package people

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The user id comes from the session middleware — it decides which library the
// filmography is marked against. Every person view is served live from TMDB and
// stores nothing (D-027), so this GET causes no write at all.
func (h *Handler) getPersonDetails(w http.ResponseWriter, r *http.Request) {
	details, err := h.svc.PersonDetails(r.Context(), httpx.UserIDFrom(r.Context()), PersonDetailsParams{
		TMDBID: r.URL.Query().Get("tmdb_id"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, details)
}
