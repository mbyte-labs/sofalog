package media

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// Tracked artwork, straight from data/assets — the offline-capable image
// route. The cache proxy (getPoster) stays the fallback for artwork the queue
// has not landed yet and for untracked live views.
func (h *Handler) getMovieAsset(w http.ResponseWriter, r *http.Request) {
	data, contentType, err := h.svc.MovieAsset(r.Context(), r.PathValue("id"), r.PathValue("file"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	writeAsset(w, contentType, data)
}

// Tracked show artwork, including its seasons' posters and its episodes'
// stills — they live under the show's directory, so this one route serves the
// whole subtree.
func (h *Handler) getShowAsset(w http.ResponseWriter, r *http.Request) {
	data, contentType, err := h.svc.ShowAsset(r.Context(), r.PathValue("id"), r.PathValue("file"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	writeAsset(w, contentType, data)
}

func writeAsset(w http.ResponseWriter, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	// Asset files are replaced in place when artwork changes, so allow a day —
	// not the proxy's five: a filename here is a slot, not a content hash.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
