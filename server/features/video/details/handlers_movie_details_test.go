package details

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func doMovieDetails(t *testing.T, mux *http.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/video/movies/details"+query, nil))
	return rec
}

func TestGetMovieDetails(t *testing.T) {
	t.Parallel()

	t.Run("200 with every nullable field present", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			// A minimal movie: everything nullable must serialize as an
			// explicit null, arrays as [].
			"/3/movie/777": `{"id": 777, "title": "Bare"}`,
		}}
		mux := searchTestMux(t, stub)

		rec := doMovieDetails(t, mux, "?tmdb_id=777")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if body["title"] != "Bare" || body["tmdb_id"] != float64(777) {
			t.Errorf("body = %v", body)
		}
		// An untracked view is live: no catalog row, so id is an explicit null
		// and every tracking flag is false.
		if id, present := body["id"]; !present || id != nil {
			t.Errorf("id = %v, want explicit null on a live view", body["id"])
		}
		for _, key := range []string{"tracked", "watched", "watchlisted", "favorited"} {
			if v, ok := body[key].(bool); !ok || v {
				t.Errorf("%s = %v, want false", key, body[key])
			}
		}
		for _, key := range []string{
			"original_title", "release_date", "year", "runtime_min", "overview",
			"tagline", "rating", "votes", "director", "poster", "poster_full",
			"homepage", "imdb_id", "trailer_url", "original_language",
			"budget", "revenue", "popularity", "user_rating",
		} {
			v, present := body[key]
			if !present {
				t.Errorf("%s key absent, want explicit null", key)
			} else if v != nil {
				t.Errorf("%s = %v, want null", key, v)
			}
		}
		for _, key := range []string{"genres", "cast", "similar"} {
			v, ok := body[key].([]any)
			if !ok || len(v) != 0 {
				t.Errorf("%s = %v, want an empty array", key, body[key])
			}
		}
		if _, ok := body["fetched_at"].(string); !ok {
			t.Errorf("fetched_at = %v, want an RFC3339 string", body["fetched_at"])
		}
	})

	t.Run("an id miss is a 404 envelope", func(t *testing.T) {
		t.Parallel()
		rec := doMovieDetails(t, searchTestMux(t, &videotest.ProviderStub{}), "?id=42")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "not_found" {
			t.Errorf("code = %q, want not_found", code)
		}
	})

	invalid := []string{"", "?tmdb_id=603&id=1", "?tmdb_id=abc"}
	for _, query := range invalid {
		t.Run("invalid params "+query+" return 422 envelope", func(t *testing.T) {
			t.Parallel()
			rec := doMovieDetails(t, searchTestMux(t, &videotest.ProviderStub{}), query)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
			}
			if code := videotest.ErrorCode(t, rec); code != "validation" {
				t.Errorf("code = %q, want validation", code)
			}
		})
	}
}
