package watches

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The bulk mark. Separate from postWatch because its answer is a count and a
// recomputed show state, not the single watch row's created flag.
func (h *Handler) postBulkWatch(w http.ResponseWriter, r *http.Request) {
	var p BulkWatchParams
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := h.svc.BulkMarkWatched(r.Context(), httpx.UserIDFrom(r.Context()), p)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}
