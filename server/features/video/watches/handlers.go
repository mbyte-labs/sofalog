package watches

import "net/http"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/video/watches", h.postWatch)
	mux.HandleFunc("POST /api/v1/video/watches/bulk", h.postBulkWatch)
	mux.HandleFunc("DELETE /api/v1/video/watches/{id}", h.deleteWatch)
	mux.HandleFunc("POST /api/v1/video/shows/{id}/drop", h.postShowDrop)
	mux.HandleFunc("DELETE /api/v1/video/shows/{id}/drop", h.deleteShowDrop)
}
