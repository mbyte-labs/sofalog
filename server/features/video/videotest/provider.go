package videotest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"mbyte.ro/sofalog/provider"
)

// The scan's fake server lives in this handlers_ test file rather than beside
// the service tests because /verify's layering grep forbids net/http in any
// service*.go — tests included.
// JellyfinFake plays a Jellyfin server: one page per item type, a fixed
// public info, and an optional failure. Safe for the parallel subtests.
type JellyfinFake struct {
	Mu    sync.Mutex
	Items map[string]string // includeItemTypes -> the Items JSON array
	Fail  error
	// status forces one HTTP status on every answer (0 = 200).
	Status int
	calls  int
}

func (f *JellyfinFake) Do(req *http.Request) (*http.Response, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.calls++
	if f.Fail != nil {
		return nil, f.Fail
	}
	status := f.Status
	if status == 0 {
		status = http.StatusOK
	}
	body := `{"ServerName":"Living room","Version":"10.10.3","Id":"abc"}`
	if req.URL.Path == "/Items" {
		body = `{"Items":` + f.Items[req.URL.Query().Get("includeItemTypes")] + `}`
		if f.Items[req.URL.Query().Get("includeItemTypes")] == "" {
			body = `{"Items":[]}`
		}
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// GatedDoer blocks every request until released, or until the request's
// context ends — the way to hold a job in flight while a test looks at it.
type GatedDoer struct {
	Inner *ProviderStub
	Gate  chan struct{}
}

func (g *GatedDoer) Do(req *http.Request) (*http.Response, error) {
	select {
	case <-g.Gate:
		return g.Inner.Do(req)
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}

// PrefixGatedDoer holds requests under prefix on the gate and passes every
// other request straight to the gate's inner stub.
type PrefixGatedDoer struct {
	Gate   *GatedDoer
	Prefix string
}

func (d PrefixGatedDoer) Do(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.Path, d.Prefix) {
		return d.Gate.Do(req)
	}
	return d.Gate.Inner.Do(req)
}

// NewGatedTMDB is a TMDB client whose every request waits on the gate.
func NewGatedTMDB(gate *GatedDoer) *provider.TMDB {
	return provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"}, provider.WithHTTPDoer(gate))
}

const TMDBConfigBody = `{"images":{"secure_base_url":"https://image.tmdb.org/t/p/",` +
	`"poster_sizes":["w185","w500"],"still_sizes":["w185"],"backdrop_sizes":["w300","w780"]}}`

// ProviderStub scripts one response per TMDB URL path and counts requests.
// Safe for concurrent use; an unscripted path gets a 404, which surfaces in
// the test as an unexpected error rather than a hang.
type ProviderStub struct {
	Mu       sync.Mutex
	Bodies   map[string]string
	Statuses map[string]int
	Errs     map[string]error
	Calls    map[string]int
	queries  map[string]url.Values // the LAST query string sent to each path
}

func (s *ProviderStub) Do(req *http.Request) (*http.Response, error) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	path := req.URL.Path
	if s.Calls == nil {
		s.Calls = map[string]int{}
	}
	s.Calls[path]++
	if s.queries == nil {
		s.queries = map[string]url.Values{}
	}
	s.queries[path] = req.URL.Query()
	if err := s.Errs[path]; err != nil {
		return nil, err
	}
	status := s.Statuses[path]
	// A paged list may script each page under path?page=N; a body keyed on
	// the bare path answers every page.
	body, scripted := s.Bodies[path+"?page="+req.URL.Query().Get("page")]
	if !scripted {
		body, scripted = s.Bodies[path]
	}
	if status == 0 {
		if scripted {
			status = http.StatusOK
		} else {
			status = http.StatusNotFound
		}
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// FailAfter wraps a ProviderStub and starts failing once n requests have been
// served — the only way to exercise a mid-materialise outage, since every
// season of a show is fetched from the same path and a path-keyed stub cannot
// distinguish them.
type FailAfter struct {
	Inner *ProviderStub
	N     int
	seen  int
}

func (f *FailAfter) Do(req *http.Request) (*http.Response, error) {
	f.seen++
	if f.seen > f.N {
		return nil, errors.New("dial tcp: connection refused")
	}
	return f.Inner.Do(req)
}

// CancelAfter serves one request through the stub, then cancels the context it
// was given — the shape of a shutdown landing mid-way through a refresh pass.
// Beside FailAfter because service tests may not import net/http themselves.
type CancelAfter struct {
	Inner  *ProviderStub
	Cancel context.CancelFunc
}

func (c *CancelAfter) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.Inner.Do(req)
	c.Cancel()
	return resp, err
}

// LastQuery returns the query string of the most recent request to path.
func (s *ProviderStub) LastQuery(path string) url.Values {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.queries[path]
}

func (s *ProviderStub) CallCount(path string) int {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Calls[path]
}

func ErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %s is not an error envelope: %v", rec.Body, err)
	}
	return body.Error.Code
}
