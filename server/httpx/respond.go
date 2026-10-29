package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// maxBodyBytes caps JSON request bodies; nothing in the API needs more.
const maxBodyBytes = 1 << 20

// errorEnvelope is the one error shape every endpoint returns:
//
//	{ "error": { "code": "invalid_body", "message": "…" } }
//
// code is a stable machine string the SPA can branch on; message is human text.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON encodes v as the response body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encoding response", "err", err)
	}
}

// WriteError writes the error envelope. Never pass a raw Go error string as
// message on a 500 path — use WriteServiceError.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

// DecodeJSON decodes a request body into dst, rejecting unknown fields,
// trailing garbage and bodies over 1 MiB.
func DecodeJSON(r *http.Request, dst any) error {
	return DecodeJSONLimit(r, dst, maxBodyBytes)
}

// DecodeJSONLimit is DecodeJSON with the body cap chosen by the caller. The
// only route that needs more than the default is the backup restore, whose
// body is a whole export file; everything else keeps the 1 MiB default so a
// bigger cap is an explicit, visible choice at the one call site that wants it.
func DecodeJSONLimit(r *http.Request, dst any, maxBytes int64) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if dec.More() {
		return errors.New("invalid JSON body: unexpected trailing data")
	}
	return nil
}

// LogServiceError records an error on a path that cannot answer with the
// error envelope — the OIDC callback redirects the browser instead, and
// without this the cause would be lost entirely.
func LogServiceError(r *http.Request, err error) {
	slog.Error("service error",
		"err", err,
		"request_id", RequestIDFrom(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
	)
}

// WriteServiceError is the single sentinel→status mapping. Unrecognized
// errors become a 500: logged with the request id, message never leaked.
func WriteServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		WriteError(w, http.StatusUnprocessableEntity, "validation", err.Error())
	case errors.Is(err, ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrConflict):
		WriteError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, ErrForbidden):
		WriteError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, ErrUnauthorized):
		WriteError(w, http.StatusUnauthorized, "unauthorized", err.Error())
	case errors.Is(err, ErrProviderUnavailable):
		WriteError(w, http.StatusServiceUnavailable, "provider_unavailable", err.Error())
	default:
		slog.Error("internal error",
			"err", err,
			"request_id", RequestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
		)
		WriteError(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}
