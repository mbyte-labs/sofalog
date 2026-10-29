package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return getUnder(t, "/", path)
}

func getUnder(t *testing.T, basePath, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler(basePath).ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestHandler(t *testing.T) {
	t.Parallel()

	t.Run("unknown api path returns JSON 404, never HTML", func(t *testing.T) {
		t.Parallel()
		rec := get(t, "/api/v1/nothing")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want JSON", ct)
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "not_found" {
			t.Fatalf("body = %s, want the not_found envelope", rec.Body)
		}
	})

	t.Run("root serves index.html with no-cache", func(t *testing.T) {
		t.Parallel()
		rec := get(t, "/")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "sofalog") {
			t.Error("body does not look like the stub index.html")
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Cache-Control = %q, want no-cache", cc)
		}
	})

	t.Run("client-side route falls back to index.html", func(t *testing.T) {
		t.Parallel()
		rec := get(t, "/movies/42")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "sofalog") {
			t.Error("fallback did not serve index.html")
		}
	})

	t.Run("missing file with extension is a plain 404", func(t *testing.T) {
		t.Parallel()
		if rec := get(t, "/nope.js"); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	// The bundle's URLs are all relative, so <base href> is what makes them
	// resolve against the app root instead of the current route. Without the
	// rewrite, /video/history would request /video/assets/... and 404.
	t.Run("base href is rewritten to the configured base path", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			basePath string
			wantHref string
		}{
			{"root deployment", "/", `<base href="/"`},
			{"sub-path deployment", "/sofalog/", `<base href="/sofalog/"`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				// A deep client-side route is the case that regressed.
				for _, path := range []string{"/", "/video/history"} {
					rec := getUnder(t, tt.basePath, path)
					if rec.Code != http.StatusOK {
						t.Fatalf("%s: status = %d, want 200", path, rec.Code)
					}
					body := rec.Body.String()
					if !strings.Contains(body, tt.wantHref) {
						t.Errorf("%s: body missing %s", path, tt.wantHref)
					}
					if strings.Count(body, "<base href=") != 1 {
						t.Errorf("%s: want exactly one base tag, got %d",
							path, strings.Count(body, "<base href="))
					}
				}
			})
		}
	})
}
