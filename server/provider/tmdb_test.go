package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"mbyte.ro/sofalog/httpx"
)

// stubDoer records every request and replays a scripted response. No test in this
// package may reach the network; a test that does is a broken test.
type stubDoer struct {
	calls    int
	requests []*http.Request
	status   int
	body     string
	err      error
}

func (s *stubDoer) Do(req *http.Request) (*http.Response, error) {
	s.calls++
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

const configBody = `{"images":{"secure_base_url":"https://image.tmdb.org/t/p/",` +
	`"poster_sizes":["w185","w500"],"still_sizes":["w185"],"backdrop_sizes":["w300","w780"]}}`

func TestTMDBConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		key       string
		stub      stubDoer
		wantErr   string // substring; empty means success
		wantCalls int
	}{
		{
			name:      "success returns the image base url",
			key:       "v3key",
			stub:      stubDoer{body: configBody},
			wantCalls: 1,
		},
		{
			name:      "no credential fails without issuing a request",
			key:       "",
			stub:      stubDoer{body: configBody},
			wantErr:   "set TMDB_API_KEY_V3 or TMDB_API_KEY_V4",
			wantCalls: 0,
		},
		{
			name:      "401 names the key as the problem",
			key:       "wrongkey",
			stub:      stubDoer{status: http.StatusUnauthorized, body: `{"status_message":"Invalid API key"}`},
			wantErr:   "rejected the API key (401)",
			wantCalls: 1,
		},
		{
			name:      "500 is reported with its status",
			key:       "v3key",
			stub:      stubDoer{status: http.StatusInternalServerError, body: "boom"},
			wantErr:   "500 Internal Server Error",
			wantCalls: 1,
		},
		{
			name:      "transport failure is reported",
			key:       "v3key",
			stub:      stubDoer{err: errors.New("dial tcp: no route to host")},
			wantErr:   "no route to host",
			wantCalls: 1,
		},
		{
			name:      "unparseable body is reported",
			key:       "v3key",
			stub:      stubDoer{body: "<html>not json</html>"},
			wantErr:   "decoding tmdb /configuration response",
			wantCalls: 1,
		},
		{
			name:      "a response with no image base url is rejected",
			key:       "v3key",
			stub:      stubDoer{body: `{"images":{}}`},
			wantErr:   "no image base url",
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := tt.stub
			c := NewTMDB(TMDBCredentials{APIKeyV3: tt.key}, WithHTTPDoer(&stub))

			cfg, err := c.Configuration(context.Background())

			if stub.calls != tt.wantCalls {
				t.Errorf("issued %d requests, want %d", stub.calls, tt.wantCalls)
			}
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tt.wantErr)
				}
				// Every failure in this package must map to a 503, so callers
				// never need to know which provider broke.
				if !errors.Is(err, httpx.ErrProviderUnavailable) {
					t.Errorf("error %v does not wrap ErrProviderUnavailable", err)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.SecureBaseURL != "https://image.tmdb.org/t/p/" {
				t.Errorf("SecureBaseURL = %q", cfg.SecureBaseURL)
			}
			if len(cfg.PosterSizes) != 2 || cfg.PosterSizes[0] != "w185" {
				t.Errorf("PosterSizes = %v", cfg.PosterSizes)
			}
		})
	}
}

func TestTMDBConfigurationCaching(t *testing.T) {
	t.Parallel()

	t.Run("a successful fetch is cached", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: configBody}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		for i := range 3 {
			if _, err := c.Configuration(context.Background()); err != nil {
				t.Fatalf("call %d: %v", i, err)
			}
		}
		if stub.calls != 1 {
			t.Errorf("issued %d requests, want 1 — /configuration must be fetched once", stub.calls)
		}
	})

	t.Run("a failed fetch is not cached", func(t *testing.T) {
		t.Parallel()
		// Caching a failure would mean a provider blip at startup permanently
		// disabled adding items until the process restarted.
		stub := &stubDoer{status: http.StatusInternalServerError, body: "boom"}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		if _, err := c.Configuration(context.Background()); err == nil {
			t.Fatal("want an error")
		}
		stub.status = http.StatusOK
		stub.body = configBody
		cfg, err := c.Configuration(context.Background())
		if err != nil {
			t.Fatalf("retry after a failure must work: %v", err)
		}
		if cfg.SecureBaseURL == "" {
			t.Error("retry returned an empty config")
		}
		if stub.calls != 2 {
			t.Errorf("issued %d requests, want 2", stub.calls)
		}
	})
}

// TestTMDBCredentialResolution is the heart of the v3/v4 split: which credential
// gets used, how it is presented, and what happens to a value in the wrong field.
func TestTMDBCredentialResolution(t *testing.T) {
	t.Parallel()

	const jwt = "eyJhbGciOiJIUzI1NiJ9.payload.sig"

	tests := []struct {
		name         string
		creds        TMDBCredentials
		wantAuth     string
		wantQuery    string // expected api_key query value; empty means absent
		wantHeader   string // expected Authorization header; empty means absent
		wantWarnings int
		wantErr      string // set when no usable credential survives
	}{
		{
			name:      "v3 key only goes in the query string",
			creds:     TMDBCredentials{APIKeyV3: "abc123"},
			wantAuth:  "v3 api_key",
			wantQuery: "abc123",
		},
		{
			name:       "v4 token only goes in the Authorization header",
			creds:      TMDBCredentials{APIKeyV4: jwt},
			wantAuth:   "v4 bearer",
			wantHeader: "Bearer " + jwt,
		},
		{
			name:       "both set: the v4 token wins and the v3 key is never sent",
			creds:      TMDBCredentials{APIKeyV3: "abc123", APIKeyV4: jwt},
			wantAuth:   "v4 bearer",
			wantHeader: "Bearer " + jwt,
		},
		{
			name:      "surrounding whitespace is trimmed",
			creds:     TMDBCredentials{APIKeyV3: "  abc123\n"},
			wantAuth:  "v3 api_key",
			wantQuery: "abc123",
		},
		{
			name:         "a v4 token in the v3 field is ignored, not repurposed",
			creds:        TMDBCredentials{APIKeyV3: jwt},
			wantAuth:     "none",
			wantWarnings: 1,
			wantErr:      "set TMDB_API_KEY_V3 or TMDB_API_KEY_V4",
		},
		{
			name:         "a v3 key in the v4 field is ignored, leaving the real v3 key",
			creds:        TMDBCredentials{APIKeyV3: "abc123", APIKeyV4: "not-a-jwt"},
			wantAuth:     "v3 api_key",
			wantQuery:    "abc123",
			wantWarnings: 1,
		},
		{
			name:         "both fields swapped leaves nothing usable",
			creds:        TMDBCredentials{APIKeyV3: jwt, APIKeyV4: "abc123"},
			wantAuth:     "none",
			wantWarnings: 2,
			wantErr:      "set TMDB_API_KEY_V3 or TMDB_API_KEY_V4",
		},
		{
			name:     "neither field set",
			creds:    TMDBCredentials{},
			wantAuth: "none",
			wantErr:  "set TMDB_API_KEY_V3 or TMDB_API_KEY_V4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := &stubDoer{body: configBody}
			c := NewTMDB(tt.creds, WithHTTPDoer(stub))

			if got := c.AuthMode(); got != tt.wantAuth {
				t.Errorf("AuthMode() = %q, want %q", got, tt.wantAuth)
			}
			if got := len(c.CredentialWarnings()); got != tt.wantWarnings {
				t.Errorf("CredentialWarnings() = %d entries, want %d: %v",
					got, tt.wantWarnings, c.CredentialWarnings())
			}
			// Every warning must name the variable to fix, or it is not actionable.
			for _, w := range c.CredentialWarnings() {
				if !strings.Contains(w, "TMDB_API_KEY_V3") && !strings.Contains(w, "TMDB_API_KEY_V4") {
					t.Errorf("warning %q names neither variable", w)
				}
			}

			_, err := c.Configuration(context.Background())

			if tt.wantErr != "" {
				if c.Configured() {
					t.Error("Configured() = true, want false with no usable credential")
				}
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err, tt.wantErr)
				}
				// An unusable credential must not produce a request that can
				// only be rejected.
				if stub.calls != 0 {
					t.Errorf("issued %d requests, want 0", stub.calls)
				}
				return
			}

			if !c.Configured() {
				t.Fatal("Configured() = false, want true")
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			req := stub.requests[0]
			if got := req.URL.Query().Get("api_key"); got != tt.wantQuery {
				t.Errorf("api_key query = %q, want %q", got, tt.wantQuery)
			}
			if got := req.Header.Get("Authorization"); got != tt.wantHeader {
				t.Errorf("Authorization header = %q, want %q", got, tt.wantHeader)
			}
			if got := req.URL.Path; got != "/3/configuration" {
				t.Errorf("path = %q, want /3/configuration", got)
			}
		})
	}
}

// TestTMDBErrorsNeverLeakTheKey is the test that matters most in this file.
// net/http embeds the full request URL in its transport errors, so without
// redaction a failing call writes the API key into the log — which CLAUDE.md and
// SPEC.md both forbid outright.
func TestTMDBErrorsNeverLeakTheKey(t *testing.T) {
	t.Parallel()

	const v3 = "supersecretv3"
	const v4 = "eyJhbGciOiJIUzI1NiJ9.supersecretv4.sig"

	tests := []struct {
		name  string
		creds TMDBCredentials
		key   string // the secret that must not appear in the error
		stub  stubDoer
	}{
		{
			name:  "v3 key in a transport error carrying the url",
			creds: TMDBCredentials{APIKeyV3: v3},
			key:   v3,
			stub:  stubDoer{err: fmt.Errorf(`Get "https://api.themoviedb.org/3/configuration?api_key=%s": dial tcp: timeout`, v3)},
		},
		{
			name:  "v4 token echoed back in a response body",
			creds: TMDBCredentials{APIKeyV4: v4},
			key:   v4,
			stub:  stubDoer{status: http.StatusUnauthorized, body: `{"status_message":"bad token: Bearer ` + v4 + `"}`},
		},
		{
			name:  "v3 key echoed back in a response body",
			creds: TMDBCredentials{APIKeyV3: v3},
			key:   v3,
			stub:  stubDoer{status: http.StatusUnauthorized, body: `{"request":"/configuration?api_key=` + v3 + `"}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stub := tt.stub
			c := NewTMDB(tt.creds, WithHTTPDoer(&stub))

			_, err := c.Configuration(context.Background())
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), tt.key) {
				t.Fatalf("the API key leaked into the error: %q", err)
			}
			if !strings.Contains(err.Error(), "REDACTED") {
				t.Errorf("error %q was not redacted", err)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "api_key as the first query parameter",
			in:   `Get "https://api.themoviedb.org/3/configuration?api_key=secret": timeout`,
			want: `Get "https://api.themoviedb.org/3/configuration?api_key=REDACTED": timeout`,
		},
		{
			name: "api_key after another parameter, preserving what follows",
			in:   "/search/movie?query=dune&api_key=secret&page=2",
			want: "/search/movie?query=dune&api_key=REDACTED&page=2",
		},
		{
			name: "bearer token",
			in:   "Authorization: Bearer eyJhbGciOi.abc.def failed",
			want: "Authorization: Bearer REDACTED failed",
		},
		{
			name: "nothing to redact is unchanged",
			in:   "tmdb /configuration returned 500 Internal Server Error",
			want: "tmdb /configuration returned 500 Internal Server Error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := redact(tt.in); got != tt.want {
				t.Errorf("redact(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLimiterPacesCalls(t *testing.T) {
	t.Parallel()

	t.Run("a cancelled context aborts the wait", func(t *testing.T) {
		t.Parallel()
		l := newLimiter(time.Hour)
		if err := l.wait(context.Background()); err != nil {
			t.Fatalf("the first slot must be available immediately: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := l.wait(ctx); !errors.Is(err, context.Canceled) {
			t.Errorf("wait() = %v, want context.Canceled", err)
		}
	})

	t.Run("calls are spaced by the interval", func(t *testing.T) {
		t.Parallel()
		l := newLimiter(20 * time.Millisecond)
		start := time.Now()
		for i := range 3 {
			if err := l.wait(context.Background()); err != nil {
				t.Fatalf("call %d: %v", i, err)
			}
		}
		// Three calls claim slots at 0, 20ms and 40ms.
		if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
			t.Errorf("3 calls took %v, want at least 40ms", elapsed)
		}
	})
}
