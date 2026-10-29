package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"mbyte.ro/sofalog/httpx"
)

const (
	// jellyfinInterval paces calls to the user's own server. There is no
	// published limit — it is their hardware — but a scan pages through the
	// whole library, and a home server should not be hammered for it.
	jellyfinInterval = 50 * time.Millisecond

	// jellyfinTimeout bounds one page. Larger than TMDB's: a page of 500 items
	// from a NAS over Wi-Fi is legitimately slow, and the scan is a background
	// job with nobody waiting on it.
	jellyfinTimeout = 30 * time.Second

	// jellyfinMaxBody caps one page's body. Five hundred items with only the
	// fields asked for is well under a megabyte; this is a safety net.
	jellyfinMaxBody = 16 << 20

	// jellyfinPageSize is how many items one /Items call asks for. Jellyfin
	// has no hard maximum, but a bounded page keeps memory flat on a library
	// of tens of thousands of episodes.
	jellyfinPageSize = 500
)

// ErrJellyfinAuth reports that the Jellyfin server refused the API key. It
// deliberately does not wrap httpx.ErrProviderUnavailable: a wrong key is the
// operator's configuration, not an outage, and the scan report names it so.
var ErrJellyfinAuth = errors.New("jellyfin rejected the api key")

// Jellyfin reads the household's Jellyfin server's library (D-033). It is not a
// metadata provider: nothing it returns is stored as catalog data. It answers
// exactly one question — which movies and which seasons are on that server —
// and the video feature's scan turns the answer into the `jellyfin` copy kind.
// It is never read on a render path.
//
// Safe for concurrent use. The zero value is not usable; construct with
// NewJellyfin.
type Jellyfin struct {
	baseURL string
	apiKey  string
	http    Doer
	limit   *limiter
}

// JellyfinOption customises a client. Both options exist for tests —
// production passes none.
type JellyfinOption func(*Jellyfin)

// WithJellyfinHTTPDoer replaces the HTTP client. Tests inject a stub; nothing
// else should need this.
func WithJellyfinHTTPDoer(d Doer) JellyfinOption {
	return func(c *Jellyfin) { c.http = d }
}

// NewJellyfin builds a client for the server at baseURL (no trailing slash,
// as config hands it over) authenticating with apiKey. Both empty means "not
// configured": Configured reports false and every call fails without a
// request, so the video feature can hold a client unconditionally.
func NewJellyfin(baseURL, apiKey string, opts ...JellyfinOption) *Jellyfin {
	c := &Jellyfin{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: jellyfinTimeout},
		limit:   newLimiter(jellyfinInterval),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Configured reports whether a server is set. The caller decides what an
// unconfigured client means (for the video feature: the jellyfin copy is a
// hand-set label); the client itself only refuses to send.
func (c *Jellyfin) Configured() bool {
	return c.baseURL != "" && c.apiKey != ""
}

// JellyfinServerInfo is what /System/Info/Public reports — enough to name the
// server on the settings page and to prove the URL points at a Jellyfin.
type JellyfinServerInfo struct {
	Name    string `json:"ServerName"`
	Version string `json:"Version"`
	ID      string `json:"Id"`
}

// JellyfinItem is one movie or series in the library, reduced to what the scan
// matches on: Jellyfin's own id and the TMDB id its metadata carries.
type JellyfinItem struct {
	ID     string
	TMDBID int64
}

// JellyfinEpisode is one episode file in the library, addressed the only way
// the scan needs — which series it belongs to and which season number it
// sits in. The episode number is carried for the report, not the match: a
// season counts as present on any episode (D-033).
type JellyfinEpisode struct {
	SeriesID string
	Season   int
	Episode  int
}

// Ping fetches the server's public info — a credential-free reachability
// probe, used at startup and at the top of every scan.
func (c *Jellyfin) Ping(ctx context.Context) (JellyfinServerInfo, error) {
	var info JellyfinServerInfo
	if err := c.get(ctx, "/System/Info/Public", nil, &info); err != nil {
		return JellyfinServerInfo{}, err
	}
	return info, nil
}

// Movies lists every movie in the library that carries a TMDB id. A movie
// Jellyfin never identified is invisible to the scan — there is nothing to
// match it on.
func (c *Jellyfin) Movies(ctx context.Context) ([]JellyfinItem, error) {
	return c.itemsWithTMDBID(ctx, "Movie")
}

// Series lists every series in the library that carries a TMDB id, so the
// episodes' SeriesId can be resolved to a show.
func (c *Jellyfin) Series(ctx context.Context) ([]JellyfinItem, error) {
	return c.itemsWithTMDBID(ctx, "Series")
}

// jellyfinPage is the slice of Jellyfin's BaseItemDtoQueryResult the scan
// reads. Everything else in the (very wide) DTO is ignored by the decoder.
type jellyfinPage struct {
	Items []struct {
		ID                string            `json:"Id"`
		ProviderIDs       map[string]string `json:"ProviderIds"`
		SeriesID          string            `json:"SeriesId"`
		ParentIndexNumber *int              `json:"ParentIndexNumber"`
		IndexNumber       *int              `json:"IndexNumber"`
		LocationType      string            `json:"LocationType"`
	} `json:"Items"`
	TotalRecordCount int `json:"TotalRecordCount"`
}

func (c *Jellyfin) itemsWithTMDBID(ctx context.Context, kind string) ([]JellyfinItem, error) {
	query := url.Values{
		"includeItemTypes": {kind},
		"recursive":        {"true"},
		"hasTmdbId":        {"true"},
		"fields":           {"ProviderIds"},
		"enableImages":     {"false"},
		"enableUserData":   {"false"},
	}
	var out []JellyfinItem
	err := c.pageItems(ctx, query, func(page jellyfinPage) {
		for _, it := range page.Items {
			// Jellyfin stores provider ids as strings; a value it could not
			// parse into a number is not an id sofalog can match on.
			id, err := strconv.ParseInt(it.ProviderIDs["Tmdb"], 10, 64)
			if err != nil || id <= 0 {
				continue
			}
			out = append(out, JellyfinItem{ID: it.ID, TMDBID: id})
		}
	})
	return out, err
}

// Episodes lists every episode FILE in the library with a season number.
// Jellyfin can display placeholder entries for episodes it knows exist but
// does not hold ("missing" episodes, LocationType Virtual); those must not
// light a season, so the query excludes them and the loop drops any that
// slip through.
func (c *Jellyfin) Episodes(ctx context.Context) ([]JellyfinEpisode, error) {
	query := url.Values{
		"includeItemTypes": {"Episode"},
		"recursive":        {"true"},
		"isMissing":        {"false"},
		"enableImages":     {"false"},
		"enableUserData":   {"false"},
	}
	var out []JellyfinEpisode
	err := c.pageItems(ctx, query, func(page jellyfinPage) {
		for _, it := range page.Items {
			if it.SeriesID == "" || it.ParentIndexNumber == nil || it.LocationType == "Virtual" {
				continue
			}
			ep := JellyfinEpisode{SeriesID: it.SeriesID, Season: *it.ParentIndexNumber}
			if it.IndexNumber != nil {
				ep.Episode = *it.IndexNumber
			}
			out = append(out, ep)
		}
	})
	return out, err
}

// pageItems walks /Items from the first page to the last, handing each page to
// visit. Stops on a short page; a server that reports fewer items than it
// then serves is tolerated by trusting the pages, not the count.
func (c *Jellyfin) pageItems(ctx context.Context, query url.Values, visit func(jellyfinPage)) error {
	for start := 0; ; start += jellyfinPageSize {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		q.Set("limit", strconv.Itoa(jellyfinPageSize))
		q.Set("startIndex", strconv.Itoa(start))
		var page jellyfinPage
		if err := c.get(ctx, "/Items", q, &page); err != nil {
			return err
		}
		visit(page)
		if len(page.Items) < jellyfinPageSize {
			return nil
		}
	}
}

// get performs one rate-limited, authenticated GET and decodes the JSON body.
// The key travels in the Authorization header (Jellyfin's documented form),
// never in the query, so no URL an error could quote carries it; redact is
// the second line of defence for whatever net/http puts in an error.
//
// Status mapping: 401 and 403 are ErrJellyfinAuth (the key, not the server);
// anything else non-2xx, and any transport failure, is an outage.
func (c *Jellyfin) get(ctx context.Context, path string, query url.Values, out any) error {
	if !c.Configured() {
		return fmt.Errorf("%w: no Jellyfin server configured — set JELLYFIN_URL and JELLYFIN_API_KEY", httpx.ErrProviderUnavailable)
	}
	if err := c.limit.wait(ctx); err != nil {
		return fmt.Errorf("%w: jellyfin %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("%w: jellyfin %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", `MediaBrowser Token="`+c.apiKey+`", Client="sofalog", Device="server", DeviceId="sofalog", Version="1"`)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: jellyfin %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w (%d) on %s", ErrJellyfinAuth, resp.StatusCode, path)
		}
		return fmt.Errorf("%w: jellyfin %s returned %s: %s", httpx.ErrProviderUnavailable, path, resp.Status, redact(string(raw)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, jellyfinMaxBody)).Decode(out); err != nil {
		return fmt.Errorf("%w: decoding jellyfin %s response: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	return nil
}
