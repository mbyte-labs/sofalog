package tmdbaccount

import (
	"net/http"

	"mbyte.ro/sofalog/features/video/common"
)

type Handler struct {
	svc *Service
	// cookie is what the TMDB link flow's cookie carries (D-029): BASE_URL's
	// path and Secure flag, handed in from main.
	cookie common.CookieSettings
}

func NewHandler(svc *Service, cookie common.CookieSettings) *Handler {
	return &Handler{svc: svc, cookie: cookie}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/video/tmdb/link", h.getTMDBLink)
	mux.HandleFunc("GET /api/v1/video/tmdb/link/start", h.getTMDBLinkStart)
	mux.HandleFunc("GET /api/v1/video/tmdb/link/callback", h.getTMDBLinkCallback)
	mux.HandleFunc("DELETE /api/v1/video/tmdb/link", h.deleteTMDBLink)
	mux.HandleFunc("POST /api/v1/video/tmdb/link/setup", h.postTMDBLinkSetup)
	mux.HandleFunc("PUT /api/v1/video/tmdb/link/sync", h.putTMDBLinkSync)
}
