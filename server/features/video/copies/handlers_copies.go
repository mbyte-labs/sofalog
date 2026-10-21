package copies

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The copies writes. The user id comes from the session middleware.

func (h *Handler) postCopy(w http.ResponseWriter, r *http.Request) {
	var p CopyParams
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := h.svc.CopyAdd(r.Context(), httpx.UserIDFrom(r.Context()), p)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) deleteCopy(w http.ResponseWriter, r *http.Request) {
	err := h.svc.CopyRemove(r.Context(), httpx.UserIDFrom(r.Context()),
		r.PathValue("target_type"), r.PathValue("target_id"), r.PathValue("kind"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
