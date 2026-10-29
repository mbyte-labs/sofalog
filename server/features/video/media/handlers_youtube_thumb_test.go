package media

import (
	"net/http"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestGetYouTubeThumb(t *testing.T) {
	t.Parallel()

	// The stub keys on the request path, and YouTube's thumbnail CDN addresses
	// a frame as /vi/<key>/<variant>.
	const key = "vKQi3bBA1y8"
	const framePath = "/vi/" + key + "/mqdefault.jpg"

	t.Run("fetches once, serves from cache after", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{framePath: "FRAMEBYTES"}}
		mux := searchTestMux(t, stub)

		for i := range 2 {
			rec := doGet(t, mux, "/api/v1/video/youtube-thumbs/"+key)
			if rec.Code != http.StatusOK {
				t.Fatalf("request %d: status = %d, want 200: %s", i, rec.Code, rec.Body)
			}
			if rec.Body.String() != "FRAMEBYTES" {
				t.Errorf("request %d: body = %q", i, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
				t.Errorf("request %d: Content-Type = %q, want image/jpeg", i, ct)
			}
			if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
				t.Errorf("request %d: Cache-Control = %q, want a max-age", i, cc)
			}
		}
		// The point of the proxy: YouTube sees each frame once, and the browser
		// never sees YouTube at all.
		if n := stub.CallCount(framePath); n != 1 {
			t.Errorf("frame fetched %d times, want 1", n)
		}
	})

	t.Run("a video with no frame is a 404", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Statuses: map[string]int{framePath: http.StatusNotFound}}
		rec := doGet(t, searchTestMux(t, stub), "/api/v1/video/youtube-thumbs/"+key)
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
		// A YouTube id is exactly eleven base64url characters. Anything else is
		// not a key TMDB could have given us.
		{"too short", "/api/v1/video/youtube-thumbs/short", http.StatusUnprocessableEntity},
		{"too long", "/api/v1/video/youtube-thumbs/vKQi3bBA1y8x", http.StatusUnprocessableEntity},
		{"dot in the key", "/api/v1/video/youtube-thumbs/vKQi3bBA1.8", http.StatusUnprocessableEntity},
		{"encoded traversal", "/api/v1/video/youtube-thumbs/..%2Fa%2Fb", http.StatusUnprocessableEntity},
		// An extra path segment does not match the route at all.
		{"extra segment", "/api/v1/video/youtube-thumbs/vKQi3bBA1y8/x", http.StatusNotFound},
	}
	for _, tt := range rejected {
		t.Run(tt.name+" is rejected", func(t *testing.T) {
			t.Parallel()
			stub := &videotest.ProviderStub{Bodies: map[string]string{}}
			rec := doGet(t, searchTestMux(t, stub), tt.target)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
			// Nothing invalid may reach a provider — only /configuration may
			// ever have been fetched.
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
