package lists

import (
	"net/http"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// The favorites routes (D-030): the watchlist's three, minus match-users. The
// service owns defaulting, validation, ordering and paging; the user id comes
// from the session middleware.
func (h *Handler) getFavorites(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entries, err := h.svc.Favorites(r.Context(), httpx.UserIDFrom(r.Context()), FavoritesParams{
		OrderBy: q.Get("order_by"),
		Order:   q.Get("order"),
		Type:    q.Get("type"),
		Limit:   q.Get("limit"),
		Offset:  q.Get("offset"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entries)
}

func (h *Handler) postFavorite(w http.ResponseWriter, r *http.Request) {
	var p common.TrackParams
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := h.svc.FavoriteAdd(r.Context(), httpx.UserIDFrom(r.Context()), p)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) deleteFavorite(w http.ResponseWriter, r *http.Request) {
	err := h.svc.FavoriteRemove(r.Context(), httpx.UserIDFrom(r.Context()),
		r.PathValue("target_type"), r.PathValue("target_id"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
