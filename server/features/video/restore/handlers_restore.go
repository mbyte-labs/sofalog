package restore

import (
	"encoding/json"
	"net/http"

	"mbyte.ro/sofalog/httpx"
)

// restoreMaxBodyBytes caps a restore upload. A Ticket Box export is a whole
// library — ten thousand history rows run to a few MB — so the 1 MiB default
// every other route keeps would refuse real files. The SPA checks the file size
// before uploading; this is the guard, not the UX path.
const restoreMaxBodyBytes = 32 << 20

// postTicketBoxRestore starts an import of a Ticket Box backup for the signed-in
// user. The body is decoded only as far as raw JSON here: the service owns the
// schema-version probe and the strict decode, because "this version is not
// supported" is a 422 with a reason, not a 400 about an unknown field.
func (h *Handler) postTicketBoxRestore(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := httpx.DecodeJSONLimit(r, &raw, restoreMaxBodyBytes); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	job, err := h.svc.StartTicketBoxRestore(r.Context(), httpx.UserIDFrom(r.Context()), raw)
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, job)
}

// getTicketBoxRestore reports the signed-in user's current or most recent
// restore job — the poll target while one runs, and the report afterwards.
func (h *Handler) getTicketBoxRestore(w http.ResponseWriter, r *http.Request) {
	job, err := h.svc.TicketBoxRestoreStatus(httpx.UserIDFrom(r.Context()))
	if err != nil {
		httpx.WriteServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}
