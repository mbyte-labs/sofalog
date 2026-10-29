package media

import "net/http"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/video/posters/{size}/{file}", h.getPoster)
	mux.HandleFunc("GET /api/v1/video/youtube-thumbs/{key}", h.getYouTubeThumb)
	mux.HandleFunc("GET /api/v1/video/movies/{id}/assets/{file...}", h.getMovieAsset)
	mux.HandleFunc("GET /api/v1/video/shows/{id}/assets/{file...}", h.getShowAsset)
}
