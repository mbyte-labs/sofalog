package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"mbyte.ro/sofalog/httpx"
)

// jellyfinStub answers /Items by item type and page, and /System/Info/Public
// with a fixed server, recording every request. No test reaches the network.
type jellyfinStub struct {
	requests []*http.Request
	status   int
	err      error
	// pages maps includeItemTypes -> the pages to serve, in order; a request
	// past the last page gets an empty one.
	pages map[string][]string
}

func (s *jellyfinStub) Do(req *http.Request) (*http.Response, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	body := `{"ServerName":"Living room","Version":"10.10.3","Id":"abc"}`
	if req.URL.Path == "/Items" {
		kind := req.URL.Query().Get("includeItemTypes")
		start, _ := strconv.Atoi(req.URL.Query().Get("startIndex"))
		page := start / jellyfinPageSize
		body = `{"Items":[],"TotalRecordCount":0}`
		if pages := s.pages[kind]; page < len(pages) {
			body = pages[page]
		}
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// fullPage builds one page of jellyfinPageSize movie items so the pager has
// to ask for a second one.
func fullPage(firstTMDB int64) string {
	var b strings.Builder
	b.WriteString(`{"Items":[`)
	for i := 0; i < jellyfinPageSize; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"Id":"m%d","ProviderIds":{"Tmdb":"%d"}}`, i, firstTMDB+int64(i))
	}
	b.WriteString(`],"TotalRecordCount":600}`)
	return b.String()
}

func TestJellyfinPing(t *testing.T) {
	t.Parallel()

	t.Run("reports the server and sends the token header, not a query key", func(t *testing.T) {
		t.Parallel()
		stub := &jellyfinStub{}
		c := NewJellyfin("http://jf.test", "s3cret", WithJellyfinHTTPDoer(stub))
		info, err := c.Ping(context.Background())
		if err != nil {
			t.Fatalf("Ping: %v", err)
		}
		if info.Name != "Living room" || info.Version != "10.10.3" {
			t.Errorf("info = %+v", info)
		}
		req := stub.requests[0]
		if req.URL.String() != "http://jf.test/System/Info/Public" {
			t.Errorf("url = %s", req.URL)
		}
		if got := req.Header.Get("Authorization"); !strings.Contains(got, `Token="s3cret"`) {
			t.Errorf("Authorization = %q, want the MediaBrowser token", got)
		}
	})

	t.Run("unconfigured refuses without a request", func(t *testing.T) {
		t.Parallel()
		stub := &jellyfinStub{}
		c := NewJellyfin("", "", WithJellyfinHTTPDoer(stub))
		if c.Configured() {
			t.Fatal("Configured() = true with no server")
		}
		if _, err := c.Ping(context.Background()); !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Errorf("err = %v, want ErrProviderUnavailable", err)
		}
		if len(stub.requests) != 0 {
			t.Errorf("%d requests sent, want 0", len(stub.requests))
		}
	})

	t.Run("401 and 403 are the key, not an outage", func(t *testing.T) {
		t.Parallel()
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			stub := &jellyfinStub{status: status}
			c := NewJellyfin("http://jf.test", "bad", WithJellyfinHTTPDoer(stub))
			_, err := c.Ping(context.Background())
			if !errors.Is(err, ErrJellyfinAuth) {
				t.Errorf("status %d: err = %v, want ErrJellyfinAuth", status, err)
			}
			if errors.Is(err, httpx.ErrProviderUnavailable) {
				t.Errorf("status %d: an auth failure must not count as an outage", status)
			}
		}
	})

	t.Run("a transport error is an outage with the token redacted", func(t *testing.T) {
		t.Parallel()
		stub := &jellyfinStub{err: errors.New(`Get "http://jf.test/Items": Authorization: MediaBrowser Token="s3cret" refused`)}
		c := NewJellyfin("http://jf.test", "s3cret", WithJellyfinHTTPDoer(stub))
		_, err := c.Ping(context.Background())
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want ErrProviderUnavailable", err)
		}
		if strings.Contains(err.Error(), "s3cret") {
			t.Errorf("error leaks the token: %v", err)
		}
	})
}

func TestJellyfinLibrary(t *testing.T) {
	t.Parallel()

	t.Run("movies page to the end and keep only parsable TMDB ids", func(t *testing.T) {
		t.Parallel()
		stub := &jellyfinStub{pages: map[string][]string{
			"Movie": {
				fullPage(1000),
				`{"Items":[
					{"Id":"a","ProviderIds":{"Tmdb":"603","Imdb":"tt0133093"}},
					{"Id":"b","ProviderIds":{"Imdb":"tt1"}},
					{"Id":"c","ProviderIds":{"Tmdb":"not-a-number"}},
					{"Id":"d","ProviderIds":{"Tmdb":"604"}}
				],"TotalRecordCount":600}`,
			},
		}}
		c := NewJellyfin("http://jf.test", "k", WithJellyfinHTTPDoer(stub))
		movies, err := c.Movies(context.Background())
		if err != nil {
			t.Fatalf("Movies: %v", err)
		}
		if len(movies) != jellyfinPageSize+2 {
			t.Fatalf("len = %d, want %d", len(movies), jellyfinPageSize+2)
		}
		last := movies[len(movies)-2:]
		if last[0] != (JellyfinItem{ID: "a", TMDBID: 603}) || last[1] != (JellyfinItem{ID: "d", TMDBID: 604}) {
			t.Errorf("tail = %+v", last)
		}
		// Two pages: the first was full, the second short.
		if len(stub.requests) != 2 {
			t.Fatalf("%d requests, want 2", len(stub.requests))
		}
		q := stub.requests[0].URL.Query()
		for k, want := range map[string]string{
			"includeItemTypes": "Movie", "recursive": "true", "hasTmdbId": "true",
			"fields": "ProviderIds", "enableImages": "false", "startIndex": "0",
			"limit": strconv.Itoa(jellyfinPageSize),
		} {
			if q.Get(k) != want {
				t.Errorf("query %s = %q, want %q", k, q.Get(k), want)
			}
		}
		if got := stub.requests[1].URL.Query().Get("startIndex"); got != strconv.Itoa(jellyfinPageSize) {
			t.Errorf("second page startIndex = %q", got)
		}
	})

	t.Run("episodes drop virtual placeholders and unsorted files", func(t *testing.T) {
		t.Parallel()
		stub := &jellyfinStub{pages: map[string][]string{
			"Episode": {`{"Items":[
				{"Id":"e1","SeriesId":"s1","ParentIndexNumber":1,"IndexNumber":3,"LocationType":"FileSystem"},
				{"Id":"e2","SeriesId":"s1","ParentIndexNumber":2,"IndexNumber":1,"LocationType":"Virtual"},
				{"Id":"e3","SeriesId":"s1","IndexNumber":4,"LocationType":"FileSystem"},
				{"Id":"e4","ParentIndexNumber":1,"IndexNumber":1,"LocationType":"FileSystem"},
				{"Id":"e5","SeriesId":"s2","ParentIndexNumber":0,"LocationType":"FileSystem"}
			],"TotalRecordCount":5}`},
		}}
		c := NewJellyfin("http://jf.test", "k", WithJellyfinHTTPDoer(stub))
		eps, err := c.Episodes(context.Background())
		if err != nil {
			t.Fatalf("Episodes: %v", err)
		}
		want := []JellyfinEpisode{{SeriesID: "s1", Season: 1, Episode: 3}, {SeriesID: "s2", Season: 0}}
		if len(eps) != len(want) {
			t.Fatalf("episodes = %+v, want %+v", eps, want)
		}
		for i := range want {
			if eps[i] != want[i] {
				t.Errorf("episode %d = %+v, want %+v", i, eps[i], want[i])
			}
		}
		if got := stub.requests[0].URL.Query().Get("isMissing"); got != "false" {
			t.Errorf("isMissing = %q, want false", got)
		}
	})

	t.Run("a 5xx mid-walk is an outage", func(t *testing.T) {
		t.Parallel()
		stub := &jellyfinStub{status: http.StatusBadGateway}
		c := NewJellyfin("http://jf.test", "k", WithJellyfinHTTPDoer(stub))
		if _, err := c.Series(context.Background()); !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Errorf("err = %v, want ErrProviderUnavailable", err)
		}
	})
}

func TestRedactJellyfinToken(t *testing.T) {
	t.Parallel()
	in := `MediaBrowser Token="abc123", Client="x"; X-Emby-Token: zzz; ?api_key=k1`
	got := redact(in)
	for _, secret := range []string{"abc123", "zzz", "k1"} {
		if strings.Contains(got, secret) {
			t.Errorf("redact left %q in %q", secret, got)
		}
	}
}
