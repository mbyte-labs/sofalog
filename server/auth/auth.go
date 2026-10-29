// Package auth is the auth feature: users, argon2id passwords, server-side
// sessions, the login/setup/logout handlers and the session middleware.
// Cross-cutting, so it stays top-level rather than under features/ (D-014).
//
// The layering follows the feature convention — handlers.go → service*.go →
// store_*.go — with middleware.go alongside handlers at the HTTP layer.
// Sessions are rows in the sessions table; the cookie carries the opaque
// token verbatim. OIDC and API tokens are M5: their tables exist (D-007) but
// nothing here reads them yet.
package auth
