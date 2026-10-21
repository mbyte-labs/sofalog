package ratings

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The rating writes. PUT rather than POST because a rating is one value that
// gets replaced, not an event that accumulates — and DELETE is how the user
// takes their rating back.

func (h *Handler) putRating(w http.ResponseWriter, r *http.Request) {
	var p RateParams
	if err := httpx.DecodeJSON(r, &p); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := h.svc.Rate(r.Context(), httpx.UserIDFrom(r.Context()), p)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h *Handler) deleteRating(w http.ResponseWriter, r *http.Request) {
	err := h.svc.RatingRemove(r.Context(), httpx.UserIDFrom(r.Context()),
		r.PathValue("target_type"), r.PathValue("target_id"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
