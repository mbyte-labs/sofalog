package jellyfin

import (
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// The Jellyfin library scan's two routes (D-033): the settings page's status
// read, and its "scan now". Neither touches the server itself — the read is
// in-memory state, and the POST only wakes the loop.

func (h *Handler) getJellyfin(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, h.svc.JellyfinStatus())
}

func (h *Handler) postJellyfinScan(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.StartJellyfinScan(); err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, h.svc.JellyfinStatus())
}
