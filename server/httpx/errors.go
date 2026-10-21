// Package httpx holds the HTTP plumbing shared by every feature: middleware,
// the error sentinels, the JSON error envelope and the respond helpers.
package httpx

import "errors"

// Sentinel errors wrapped by service and store code (with %w) and mapped to
// HTTP statuses in exactly one place, WriteServiceError. Handlers never pick
// a status for a domain condition themselves.
var (
	// ErrValidation: well-formed request, invalid values (rating 11, bad date). 422.
	ErrValidation = errors.New("invalid input")
	// ErrNotFound: no such row, or the row belongs to another user. 404.
	ErrNotFound = errors.New("not found")
	// ErrConflict: uniqueness violation (username taken, duplicate entry). 409.
	ErrConflict = errors.New("conflict")
	// ErrForbidden: authenticated but not permitted. 403.
	ErrForbidden = errors.New("forbidden")
	// ErrUnauthorized: no or invalid session/token; usually from middleware. 401.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrProviderUnavailable: TMDB/Steam unreachable or key
	// missing. 503 — only reachable from search, add, the live details views
	// (an untracked item's first view, D-015; any person view, D-027) and the
	// trending and discover lists (D-034); any other read path returning this
	// means the no-provider-on-render-path rule was violated.
	ErrProviderUnavailable = errors.New("provider unavailable")
)
