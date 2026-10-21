package copies

import "net/http"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/video/copies", h.postCopy)
	mux.HandleFunc("DELETE /api/v1/video/copies/{target_type}/{target_id}/{kind}", h.deleteCopy)
}
