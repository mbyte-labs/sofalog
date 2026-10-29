package ratings

import "net/http"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /api/v1/video/ratings", h.putRating)
	mux.HandleFunc("DELETE /api/v1/video/ratings/{target_type}/{target_id}", h.deleteRating)
}
