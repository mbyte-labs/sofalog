package discover

import "net/http"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/video/search", h.searchVideo)
	mux.HandleFunc("GET /api/v1/video/trending", h.getTrending)
	mux.HandleFunc("GET /api/v1/video/discover", h.getDiscover)
	mux.HandleFunc("GET /api/v1/video/discover/options", h.getDiscoverOptions)
}
