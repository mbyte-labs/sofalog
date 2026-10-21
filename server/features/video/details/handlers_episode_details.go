package details

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// Like the season handler this GET can cause a write, because it resolves the
// season's snapshot row through the same path (the recorded D-015 season
// deviation) — the episode itself stores nothing.
func (h *Handler) getEpisodeDetails(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	details, err := h.svc.EpisodeDetails(r.Context(), httpx.UserIDFrom(r.Context()), EpisodeDetailsParams{
		SeasonID: q.Get("season_id"),
		TMDBID:   q.Get("tmdb_id"),
		Season:   q.Get("season"),
		Episode:  q.Get("episode"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, details)
}
