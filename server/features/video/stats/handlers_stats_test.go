package stats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// statsTestMux wires the routes over one followed show and one watched movie
// with a backdrop. The service carries NO provider client, so a render path
// reaching TMDB would panic here rather than pass.
func statsTestMux(t *testing.T) (*http.ServeMux, int64) {
	t.Helper()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	m := videotest.SeedMovie(t, svc.DB, 603, "The Matrix", videotest.Ptr("1999-03-30"), videotest.Ptr("/m.jpg"), `{}`).ID
	setMovieBackdrop(t, svc.DB, m, "/b.jpg")
	videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-03-01T20:00:00Z")
	show := videotest.SeedShow(t, svc.DB, 10, "Show")
	e := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
	videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-01-02"))
	videotest.SeedWatch(t, svc.DB, user, "episode", e, "2026-03-02T20:00:00Z")
	videotest.SeedShowStatus(t, svc.DB, user, show, "watching")

	mux := http.NewServeMux()
	allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)
	return mux, user
}

func doStats(t *testing.T, mux *http.ServeMux, userID int64, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/video/stats"+query, nil)
	req = req.WithContext(httpx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// wantNull asserts a key is PRESENT and null — the SPA branches on null, and a
// missing key is a contract break.
func wantNull(t *testing.T, obj map[string]any, key string) {
	t.Helper()
	v, present := obj[key]
	if !present {
		t.Errorf("%s key absent, want explicit null", key)
	} else if v != nil {
		t.Errorf("%s = %v, want null", key, v)
	}
}

func decodeStats(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
	var obj map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &obj); err != nil {
		t.Fatalf("body is not a JSON object: %v", err)
	}
	return obj
}

func TestGetStats(t *testing.T) {
	t.Parallel()

	t.Run("default period is the current year, sections hidden as null", func(t *testing.T) {
		t.Parallel()
		mux, user := statsTestMux(t)
		obj := decodeStats(t, doStats(t, mux, user, ""))
		if obj["period"] != "2026" || obj["today"] != "2026-08-18" {
			t.Fatalf("period = %v, today = %v", obj["period"], obj["today"])
		}
		if years, ok := obj["years"].([]any); !ok || len(years) != 1 || years[0] != float64(2026) {
			t.Fatalf("years = %v", obj["years"])
		}
		for _, key := range []string{"ratings", "rating_trend", "crowd", "completion"} {
			wantNull(t, obj, key)
		}
		headline := obj["headline"].(map[string]any)
		if headline["plays"] != float64(2) || headline["movies"] != float64(1) || headline["episodes"] != float64(1) {
			t.Fatalf("headline = %v", headline)
		}
		unrated := obj["unrated_movies"].(map[string]any)
		movie := unrated["recent"].([]any)[0].(map[string]any)["movie"].(map[string]any)
		if movie["kind"] != "movie" || movie["year"] != float64(1999) ||
			movie["poster"] != "api/v1/video/posters/w185/m.jpg" || movie["backdrop"] != "api/v1/video/posters/w300/b.jpg" ||
			movie["poster_full"] != "api/v1/video/posters/w500/m.jpg" || movie["backdrop_full"] != "api/v1/video/posters/w1280/b.jpg" {
			t.Fatalf("movie title ref = %v", movie)
		}
		activity := obj["activity"].(map[string]any)
		wantNull(t, activity, "tracking_days")
		if _, ok := activity["heatmap"].(map[string]any)["levels"].([]any); !ok {
			t.Fatalf("heatmap = %v", activity["heatmap"])
		}
		if tr, ok := obj["top_rated"].([]any); !ok || len(tr) != 0 {
			t.Fatalf("top_rated = %v, want []", obj["top_rated"])
		}
	})

	t.Run("all time carries the completion section", func(t *testing.T) {
		t.Parallel()
		mux, user := statsTestMux(t)
		obj := decodeStats(t, doStats(t, mux, user, "?period=all"))
		completion, ok := obj["completion"].(map[string]any)
		if !ok || completion["tracked"] != float64(1) || completion["in_progress"] != float64(1) {
			t.Fatalf("completion = %v", obj["completion"])
		}
		if obj["activity"].(map[string]any)["tracking_days"] == nil {
			t.Fatal("tracking_days null in all-time mode")
		}
	})

	t.Run("a malformed period is a 422", func(t *testing.T) {
		t.Parallel()
		mux, user := statsTestMux(t)
		rec := doStats(t, mux, user, "?period=nope")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "validation" {
			t.Fatalf("error code = %q", code)
		}
	})

	t.Run("an unknown user gets the empty shape", func(t *testing.T) {
		t.Parallel()
		mux, _ := statsTestMux(t)
		obj := decodeStats(t, doStats(t, mux, 999, ""))
		if obj["period"] != "all" || len(obj["years"].([]any)) != 0 {
			t.Fatalf("period = %v, years = %v", obj["period"], obj["years"])
		}
	})
}
