// Package provider holds the clients for sofalog's external metadata sources —
// TMDB and Steam — plus YouTube's thumbnail CDN, which is an image
// source only and carries no metadata (D-017), and the household's own Jellyfin
// server, which is not a metadata source at all but a library to compare
// against (D-033). It is shared infrastructure rather than a feature: it knows
// nothing about users, watch statuses or SQL.
//
// Provider calls are allowed in exactly eight places: search, add-item
// enrichment, user-triggered Steam sync, the one scheduled TMDB refresh, the
// details views — the movie, show and season snapshot/refresh (D-015) and
// the person page, which has no snapshot half and is served live on every
// view (D-027) — the TMDB account link with its rating outbox (D-029),
// the one WRITE to a provider: a user's own Link/Unlink, and a background
// worker pushing ratings out of a durable queue — the Jellyfin library
// scan (D-033), a background read of the user's own server that writes no
// catalog data — and the trending and discover lists (D-034), discovery
// served live on every view like the person page and never stored. Any
// other render path that reaches this package is a bug — the library must
// keep working when a provider does not.
//
// The image proxies are the one shape that looks like an exception and is not: a
// poster or a video thumbnail is fetched once on the first request for that
// filename and then served from data/cache. No page render and no read API
// response waits on a provider.
//
// Every error returned from this package wraps httpx.ErrProviderUnavailable, so
// the existing status mapping turns it into a 503 without per-call handling.
// Two deliberate exceptions: a 404 is httpx.ErrNotFound (a bogus id is the
// caller's mistake, not an outage) and a refused user session is
// ErrSessionInvalid (the link is broken, not TMDB).
package provider

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// Doer is the HTTP seam. Clients default to an *http.Client with a timeout;
// tests inject a stub, because a test that reaches TMDB is a broken test.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// limiter paces outbound calls to a single provider: one request per interval.
// Rate limiting is per provider rather than global because the budgets differ by
// an order of magnitude, and one provider's allowance must not be spent by
// another's burst.
//
// Implemented as a mutex plus a next-slot timestamp rather than a ticker
// goroutine, so a client needs no Close and no lifecycle management.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newLimiter(interval time.Duration) *limiter {
	return &limiter{interval: interval}
}

// wait blocks until this caller's slot arrives, or until ctx is done. Slots are
// claimed under the lock and waited on outside it, so a slow caller does not
// stall the queue behind it.
func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	slot := l.next
	l.next = slot.Add(l.interval)
	l.mu.Unlock()

	delay := time.Until(slot)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ErrSessionInvalid reports that TMDB refused a user's session id (or the
// request token being exchanged for one): the user's LINK is broken, not TMDB.
// It deliberately does not wrap httpx.ErrProviderUnavailable — a breaker must
// not count it as an outage, and it is never mapped to an HTTP status: the
// rating worker marks the link broken and the link flow reports "denied".
var ErrSessionInvalid = errors.New("tmdb session invalid")

// net/http embeds the full request URL in its transport errors, so an api_key
// query parameter reaches any error string built from one. CLAUDE.md forbids ever
// logging a provider key, and an error is one log line away from being logged.
// A user's TMDB session id (D-029) travels the same way and is the same kind
// of secret, so it has the same two arms: the query form and the JSON body
// form TMDB echoes on some errors.
var (
	apiKeyParamRe    = regexp.MustCompile(`(?i)([?&]api_key=)[^&\s"]+`)
	bearerTokenRe    = regexp.MustCompile(`(?i)(bearer\s+)\S+`)
	sessionParamRe   = regexp.MustCompile(`(?i)([?&]session_id=)[^&\s"]+`)
	sessionJSONRe    = regexp.MustCompile(`(?i)("session_id"\s*:\s*")[^"]+`)
	requestTokenRe   = regexp.MustCompile(`(?i)([?&]request_token=)[^&\s"]+`)
	requestTokenJSON = regexp.MustCompile(`(?i)("request_token"\s*:\s*")[^"]+`)
	// Jellyfin's API key (D-033) rides an Authorization header in the
	// MediaBrowser scheme, or legacy clients' X-Emby-Token / api_key query —
	// the last of which the first arm above already covers.
	mediaBrowserTokenRe = regexp.MustCompile(`(?i)(token=")[^"]+`)
	embyTokenRe         = regexp.MustCompile(`(?i)(x-emby-token[=:]\s*)\S+`)
)

// redact removes provider secrets from a string before it becomes an error.
// Every error leaving this package passes through it.
func redact(s string) string {
	s = apiKeyParamRe.ReplaceAllString(s, "${1}REDACTED")
	s = bearerTokenRe.ReplaceAllString(s, "${1}REDACTED")
	s = sessionParamRe.ReplaceAllString(s, "${1}REDACTED")
	s = sessionJSONRe.ReplaceAllString(s, "${1}REDACTED")
	s = requestTokenRe.ReplaceAllString(s, "${1}REDACTED")
	s = requestTokenJSON.ReplaceAllString(s, "${1}REDACTED")
	s = mediaBrowserTokenRe.ReplaceAllString(s, "${1}REDACTED")
	return embyTokenRe.ReplaceAllString(s, "${1}REDACTED")
}
