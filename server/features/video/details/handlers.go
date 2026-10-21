package details

import "net/http"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/video/movies/details", h.getMovieDetails)
	mux.HandleFunc("GET /api/v1/video/shows/details", h.getShowDetails)
	mux.HandleFunc("GET /api/v1/video/seasons/details", h.getSeasonDetails)
	mux.HandleFunc("GET /api/v1/video/episodes/details", h.getEpisodeDetails)
}
