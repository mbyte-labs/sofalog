package media

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

func (h *Handler) getYouTubeThumb(w http.ResponseWriter, r *http.Request) {
	data, contentType, err := h.svc.YouTubeThumb(r.Context(), r.PathValue("key"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	// A YouTube video id never changes its frame, so the browser may keep it
	// five days without revalidating — same reasoning as a TMDB filename.
	w.Header().Set("Cache-Control", "public, max-age=432000")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
