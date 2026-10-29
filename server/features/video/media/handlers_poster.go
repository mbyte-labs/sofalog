package media

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

func (h *Handler) getPoster(w http.ResponseWriter, r *http.Request) {
	data, contentType, err := h.svc.Poster(r.Context(), r.PathValue("size"), r.PathValue("file"))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	// A TMDB filename never changes its bytes, so the browser may keep it
	// five days without revalidating.
	w.Header().Set("Cache-Control", "public, max-age=432000")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
