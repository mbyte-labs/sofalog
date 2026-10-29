package discover

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The user id comes from the session middleware — it decides which library the
// list is marked against. Both lists are served live and store nothing
// (D-034), so these GETs cause no write at all.
func (h *Handler) getTrending(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := h.svc.Trending(r.Context(), httpx.UserIDFrom(r.Context()), TrendingParams{
		Window: q.Get("window"),
		Type:   q.Get("type"),
		Page:   q.Get("page"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) getDiscover(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := h.svc.Discover(r.Context(), httpx.UserIDFrom(r.Context()), DiscoverParams{
		Type:     q.Get("type"),
		Sort:     q.Get("sort"),
		Page:     q.Get("page"),
		VoteMin:  q.Get("vote_min"),
		VoteMax:  q.Get("vote_max"),
		VotesMin: q.Get("votes_min"),
		Language: q.Get("language"),
		YearFrom: q.Get("year_from"),
		YearTo:   q.Get("year_to"),
		// Repeated, not comma-joined: `?genre=28&genre=35`.
		Genres:    q["genre"],
		Providers: q["provider"],
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

// The filter panel's option lists — shared by every user, so no user id.
func (h *Handler) getDiscoverOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := h.svc.DiscoverOptions(r.Context())
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, opts)
}
