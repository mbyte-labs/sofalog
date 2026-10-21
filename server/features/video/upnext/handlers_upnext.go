package upnext

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

func (h *Handler) getUpNext(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entries, err := h.svc.UpNext(r.Context(), httpx.UserIDFrom(r.Context()), UpNextParams{
		OrderBy:             q.Get("order_by"),
		Order:               q.Get("order"),
		OnlyWithReleaseDate: q.Get("only_with_release_date"),
		OnlyReleased:        q.Get("only_released"),
	})
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, entries)
}
