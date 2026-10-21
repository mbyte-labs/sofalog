package media

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func doGet(t *testing.T, mux *http.ServeMux, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	return rec
}

func TestGetPoster(t *testing.T) {
	t.Parallel()

	const imagePath = "/t/p/w185/abc123.jpg"

	t.Run("fetches once, serves from cache after", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{imagePath: "IMAGEBYTES"}}
		mux := searchTestMux(t, stub)

		for i := range 2 {
			rec := doGet(t, mux, "/api/v1/video/posters/w185/abc123.jpg")
			if rec.Code != http.StatusOK {
				t.Fatalf("request %d: status = %d, want 200: %s", i, rec.Code, rec.Body)
			}
			if rec.Body.String() != "IMAGEBYTES" {
				t.Errorf("request %d: body = %q", i, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
				t.Errorf("request %d: Content-Type = %q, want image/jpeg", i, ct)
			}
			if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
				t.Errorf("request %d: Cache-Control = %q, want a max-age", i, cc)
			}
		}
		// The second request must be a cache hit — the whole point of the
		// proxy is that TMDB sees each poster once.
		if n := stub.CallCount(imagePath); n != 1 {
			t.Errorf("TMDB image fetched %d times, want 1", n)
		}
	})

	t.Run("png gets its own content type", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/t/p/w500/x.png": "PNGBYTES"}}
		rec := doGet(t, searchTestMux(t, stub), "/api/v1/video/posters/w500/x.png")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("Content-Type = %q, want image/png", ct)
		}
	})

	t.Run("the person portrait's h632 variant is whitelisted, an unlisted size is not", func(t *testing.T) {
		t.Parallel()
		// TMDB's profile ladder is w45/w185/h632: the person page's full
		// portrait is h632 (D-027). Any size nothing renders stays a 422 — the
		// whitelist is what keeps the cache from filling with variants.
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/t/p/h632/bp.jpg": "PORTRAITBYTES"}}
		mux := searchTestMux(t, stub)
		rec := doGet(t, mux, "/api/v1/video/posters/h632/bp.jpg")
		if rec.Code != http.StatusOK || rec.Body.String() != "PORTRAITBYTES" {
			t.Fatalf("h632: status = %d body = %q, want 200 PORTRAITBYTES", rec.Code, rec.Body)
		}
		if rec := doGet(t, mux, "/api/v1/video/posters/w780/bp.jpg"); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("w780: status = %d, want 422", rec.Code)
		}
	})

	t.Run("the backdrop variants proxy through the same route", func(t *testing.T) {
		t.Parallel()
		// Backdrops share the proxy but not the size ladder: the media
		// section's galleries only render if both variants are whitelisted.
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/t/p/w300/bd.jpg":  "THUMBBYTES",
			"/t/p/w1280/bd.jpg": "FULLBYTES",
		}}
		mux := searchTestMux(t, stub)

		for _, tt := range []struct{ size, want string }{
			{"w300", "THUMBBYTES"},
			{"w1280", "FULLBYTES"},
		} {
			rec := doGet(t, mux, "/api/v1/video/posters/"+tt.size+"/bd.jpg")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status = %d, want 200: %s", tt.size, rec.Code, rec.Body)
			}
			if rec.Body.String() != tt.want {
				t.Errorf("%s: body = %q, want %q", tt.size, rec.Body, tt.want)
			}
		}
		// Same filename at two sizes must not collide in the cache.
		if n := stub.CallCount("/t/p/w300/bd.jpg"); n != 1 {
			t.Errorf("w300 fetched %d times, want 1", n)
		}
	})

	t.Run("a base64url filename is served, not rejected", func(t *testing.T) {
		t.Parallel()
		// TMDB filenames are base64url-ish hashes, so "-" and "_" appear in
		// them. Rejecting those turned a real image into a 422.
		const file = "a-b_c123.jpg"
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/t/p/w185/" + file: "HYPHENBYTES"}}
		rec := doGet(t, searchTestMux(t, stub), "/api/v1/video/posters/w185/"+file)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if rec.Body.String() != "HYPHENBYTES" {
			t.Errorf("body = %q", rec.Body)
		}
	})

	t.Run("an upstream 404 is a 404", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Statuses: map[string]int{imagePath: http.StatusNotFound}}
		rec := doGet(t, searchTestMux(t, stub), "/api/v1/video/posters/w185/abc123.jpg")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "not_found" {
			t.Errorf("code = %q, want not_found", code)
		}
	})

	rejected := []struct {
		name   string
		target string
		want   int
	}{
		{"unknown size", "/api/v1/video/posters/w999/a.jpg", http.StatusUnprocessableEntity},
		// Widening the whitelist for backdrops must not have opened it.
		{"original size", "/api/v1/video/posters/original/a.jpg", http.StatusUnprocessableEntity},
		{"non-image extension", "/api/v1/video/posters/w185/a.txt", http.StatusUnprocessableEntity},
		{"encoded traversal", "/api/v1/video/posters/w185/..%2Fa.jpg", http.StatusUnprocessableEntity},
		// An extra path segment does not match the route at all.
		{"extra segment", "/api/v1/video/posters/w185/a/b.jpg", http.StatusNotFound},
	}
	for _, tt := range rejected {
		t.Run(tt.name+" is rejected", func(t *testing.T) {
			t.Parallel()
			stub := &videotest.ProviderStub{Bodies: map[string]string{}}
			rec := doGet(t, searchTestMux(t, stub), tt.target)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
			// Nothing invalid may reach TMDB — only /configuration may ever
			// have been fetched.
			stub.Mu.Lock()
			defer stub.Mu.Unlock()
			for path, n := range stub.Calls {
				if path != "/3/configuration" && n > 0 {
					t.Errorf("provider path %s was called %d times", path, n)
				}
			}
		})
	}
}
