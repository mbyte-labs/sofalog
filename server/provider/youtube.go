package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"mbyte.ro/sofalog/httpx"
)

const (
	// youtubeThumbBaseURL is YouTube's still-frame CDN. It is not the YouTube
	// Data API: no key, no quota, no account — just images, addressed by video id.
	youtubeThumbBaseURL = "https://i.ytimg.com/vi"

	// youtubeThumbVariant is the frame fetched. TMDB gives us a video id and
	// nothing else, so a thumbnail has to come from YouTube; mqdefault is
	// 320x180 — the only small variant that is genuinely 16:9. hqdefault is 4:3
	// with black bars and maxresdefault 404s for anything but big releases, so
	// there is no reliable second variant to fetch and video thumbnails store a
	// single size (D-017, the same reasoning as achievement icons in D-005).
	youtubeThumbVariant = "mqdefault.jpg"

	// youtubeInterval paces the CDN. YouTube publishes no limit for it, but
	// opening one details page can queue a dozen thumbnails, so they go out
	// spaced rather than in a burst.
	youtubeInterval = 50 * time.Millisecond

	// youtubeTimeout bounds a single fetch; provider slowness must never become
	// server slowness.
	youtubeTimeout = 10 * time.Second

	// youtubeMaxImage caps a downloaded thumbnail. An mqdefault frame is a few
	// tens of KB; anything near this limit is not a thumbnail.
	youtubeMaxImage = 4 << 20
)

// YouTube fetches video still frames from YouTube's thumbnail CDN. Safe for
// concurrent use.
//
// It exists only so the browser never requests an image from YouTube itself:
// TMDB lists videos by id and hosts no frames, so without a proxy the SPA would
// have to embed a provider URL — which the local-first rules forbid, and which
// would send a third-party request (and cookies) from every details page. The
// frames land in data/cache like TMDB posters do (D-017).
//
// The zero value is not usable; construct with NewYouTube.
type YouTube struct {
	baseURL string
	http    Doer
	limit   *limiter
}

// YouTubeOption customises a client. Both options exist for tests — production
// passes none.
type YouTubeOption func(*YouTube)

// WithYouTubeHTTPDoer replaces the HTTP client. Tests inject a stub; nothing
// else should need this.
func WithYouTubeHTTPDoer(d Doer) YouTubeOption {
	return func(c *YouTube) { c.http = d }
}

// WithYouTubeBaseURL points the client at a different host, e.g. an httptest
// server.
func WithYouTubeBaseURL(u string) YouTubeOption {
	return func(c *YouTube) { c.baseURL = u }
}

// NewYouTube builds a thumbnail client. There is nothing to configure and no
// credential to check, so unlike NewTMDB it can never be half-usable.
func NewYouTube(opts ...YouTubeOption) *YouTube {
	c := &YouTube{
		baseURL: youtubeThumbBaseURL,
		http:    &http.Client{Timeout: youtubeTimeout},
		limit:   newLimiter(youtubeInterval),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Thumbnail fetches one video's still frame. key is a YouTube video id as TMDB
// reports it; callers validate its shape before calling, because it goes into a
// URL path. An unknown or frameless id returns httpx.ErrNotFound — a stale key
// is the caller's stale snapshot, not a provider outage.
func (c *YouTube) Thumbnail(ctx context.Context, key string) ([]byte, error) {
	if err := c.limit.wait(ctx); err != nil {
		return nil, fmt.Errorf("%w: youtube thumbnail %s: %s", httpx.ErrProviderUnavailable, key, redact(err.Error()))
	}

	target := c.baseURL + "/" + key + "/" + youtubeThumbVariant
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: youtube thumbnail %s: %s", httpx.ErrProviderUnavailable, key, redact(err.Error()))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: youtube thumbnail %s: %s", httpx.ErrProviderUnavailable, key, redact(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: youtube has no thumbnail for video %s", httpx.ErrNotFound, key)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: youtube thumbnail %s returned %s", httpx.ErrProviderUnavailable, key, redact(resp.Status))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, youtubeMaxImage))
	if err != nil {
		return nil, fmt.Errorf("%w: reading youtube thumbnail %s: %s", httpx.ErrProviderUnavailable, key, redact(err.Error()))
	}
	return data, nil
}
