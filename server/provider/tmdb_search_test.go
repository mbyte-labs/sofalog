package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"mbyte.ro/sofalog/httpx"
)

// routeDoer replays a scripted response per URL path. Needed where one client
// call fans out to several endpoints — DownloadImage always fetches
// /configuration first, which the single-response stubDoer cannot script.
type routeDoer struct {
	requests []*http.Request
	routes   map[string]stubDoer // stubDoer reused as a response script
}

func (d *routeDoer) Do(req *http.Request) (*http.Response, error) {
	d.requests = append(d.requests, req)
	script, ok := d.routes[req.URL.Path]
	if !ok {
		return nil, fmt.Errorf("unscripted path %s", req.URL.Path)
	}
	if script.err != nil {
		return nil, script.err
	}
	status := script.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(script.body)),
		Header:     make(http.Header),
	}, nil
}

// pathsOf lists the requested URL paths, for asserting call counts per endpoint.
func (d *routeDoer) pathsOf(path string) []*http.Request {
	var out []*http.Request
	for _, r := range d.requests {
		if r.URL.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func TestTMDBSearch(t *testing.T) {
	t.Parallel()

	t.Run("movies decode and send the right query", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: `{"results":[{"id":603,"title":"The Matrix",` +
			`"release_date":"1999-03-30","overview":"A hacker.","poster_path":"/m.jpg",` +
			`"popularity":83.5,"vote_average":8.2}]}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		got, err := c.SearchMovies(context.Background(), "the matrix")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		m := got[0]
		if m.ID != 603 || m.Title != "The Matrix" || m.ReleaseDate != "1999-03-30" ||
			m.PosterPath != "/m.jpg" || m.Popularity != 83.5 || m.VoteAverage != 8.2 {
			t.Errorf("decoded %+v", m)
		}

		req := stub.requests[0]
		if req.URL.Path != "/3/search/movie" {
			t.Errorf("path = %q, want /3/search/movie", req.URL.Path)
		}
		q := req.URL.Query()
		if q.Get("query") != "the matrix" {
			t.Errorf("query param = %q", q.Get("query"))
		}
		if q.Get("include_adult") != "false" {
			t.Errorf("include_adult = %q, want false", q.Get("include_adult"))
		}
	})

	t.Run("tv decodes the tv field names", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: `{"results":[{"id":1396,"name":"Breaking Bad",` +
			`"first_air_date":"2008-01-20","overview":"A teacher.","poster_path":"/b.jpg",` +
			`"popularity":245.1,"vote_average":8.9}]}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		got, err := c.SearchTV(context.Background(), "breaking bad")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		s := got[0]
		if s.ID != 1396 || s.Name != "Breaking Bad" || s.FirstAirDate != "2008-01-20" {
			t.Errorf("decoded %+v", s)
		}
		if stub.requests[0].URL.Path != "/3/search/tv" {
			t.Errorf("path = %q, want /3/search/tv", stub.requests[0].URL.Path)
		}
	})

	t.Run("a provider failure wraps ErrProviderUnavailable", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{status: http.StatusInternalServerError, body: "boom"}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		if _, err := c.SearchMovies(context.Background(), "x"); !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Errorf("error %v does not wrap ErrProviderUnavailable", err)
		}
	})
}

func TestTMDBDownloadImage(t *testing.T) {
	t.Parallel()

	const imagePath = "/t/p/w185/abc.jpg" // secure_base_url path + size + file

	newDoer := func(image stubDoer) *routeDoer {
		return &routeDoer{routes: map[string]stubDoer{
			"/3/configuration": {body: configBody},
			imagePath:          image,
		}}
	}

	t.Run("success builds the URL from configuration and sends no credential", func(t *testing.T) {
		t.Parallel()
		doer := newDoer(stubDoer{body: "IMAGEBYTES"})
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(doer))

		data, err := c.DownloadImage(context.Background(), "w185", "/abc.jpg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != "IMAGEBYTES" {
			t.Errorf("data = %q", data)
		}

		reqs := doer.pathsOf(imagePath)
		if len(reqs) != 1 {
			t.Fatalf("image requested %d times, want 1", len(reqs))
		}
		// The CDN is public: the API credential must not ride along, neither
		// as a query parameter (v3) nor as a header (v4).
		if q := reqs[0].URL.RawQuery; q != "" {
			t.Errorf("image request carries a query %q, want none", q)
		}
		if h := reqs[0].Header.Get("Authorization"); h != "" {
			t.Errorf("image request carries Authorization %q, want none", h)
		}
		if host := reqs[0].URL.Host; host != "image.tmdb.org" {
			t.Errorf("host = %q, want the configured image host", host)
		}
	})

	t.Run("an upstream 404 maps to ErrNotFound", func(t *testing.T) {
		t.Parallel()
		doer := newDoer(stubDoer{status: http.StatusNotFound})
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(doer))

		_, err := c.DownloadImage(context.Background(), "w185", "/abc.jpg")
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("error %v does not wrap ErrNotFound", err)
		}
	})

	t.Run("any other upstream failure maps to ErrProviderUnavailable", func(t *testing.T) {
		t.Parallel()
		doer := newDoer(stubDoer{status: http.StatusInternalServerError, body: "boom"})
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(doer))

		_, err := c.DownloadImage(context.Background(), "w185", "/abc.jpg")
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Errorf("error %v does not wrap ErrProviderUnavailable", err)
		}
	})

	t.Run("a transport error never leaks the key", func(t *testing.T) {
		t.Parallel()
		const key = "supersecretv3"
		doer := newDoer(stubDoer{err: fmt.Errorf(`Get "https://image.tmdb.org/t/p/w185/abc.jpg?api_key=%s": dial tcp: timeout`, key)})
		c := NewTMDB(TMDBCredentials{APIKeyV3: key}, WithHTTPDoer(doer))

		_, err := c.DownloadImage(context.Background(), "w185", "/abc.jpg")
		if err == nil {
			t.Fatal("want an error")
		}
		if strings.Contains(err.Error(), key) {
			t.Fatalf("the API key leaked into the error: %q", err)
		}
		if !strings.Contains(err.Error(), "REDACTED") {
			t.Errorf("error %q was not redacted", err)
		}
	})

	t.Run("an unconfigured client fails without a request", func(t *testing.T) {
		t.Parallel()
		doer := newDoer(stubDoer{body: "IMAGEBYTES"})
		c := NewTMDB(TMDBCredentials{}, WithHTTPDoer(doer))

		_, err := c.DownloadImage(context.Background(), "w185", "/abc.jpg")
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Errorf("error %v does not wrap ErrProviderUnavailable", err)
		}
		if len(doer.requests) != 0 {
			t.Errorf("issued %d requests, want 0", len(doer.requests))
		}
	})
}
