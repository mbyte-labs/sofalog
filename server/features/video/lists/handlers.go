package lists

import "net/http"

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers the feature's /api/v1 routes on mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/video/watchlist", h.getWatchlist)
	mux.HandleFunc("GET /api/v1/video/watchlist/match-users", h.getWatchlistMatchUsers)
	mux.HandleFunc("POST /api/v1/video/watchlist", h.postWatchlist)
	mux.HandleFunc("DELETE /api/v1/video/watchlist/{target_type}/{target_id}", h.deleteWatchlist)
	mux.HandleFunc("GET /api/v1/video/lists", h.getLists)
	mux.HandleFunc("GET /api/v1/video/favorites", h.getFavorites)
	mux.HandleFunc("POST /api/v1/video/favorites", h.postFavorite)
	mux.HandleFunc("DELETE /api/v1/video/favorites/{target_type}/{target_id}", h.deleteFavorite)
	mux.HandleFunc("GET /api/v1/video/dropped", h.getDropped)
}
