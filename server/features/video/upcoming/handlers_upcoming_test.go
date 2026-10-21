package upcoming

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// upcomingTestMux wires the routes over one entry of each arm: a followed show
// with no season row (so season_id, marker and poster are explicit nulls) and
// a watchlisted, posterless movie. The service carries NO provider client, so a
// render path reaching TMDB would panic here rather than pass.
func upcomingTestMux(t *testing.T) (*http.ServeMux, int64) {
	t.Helper()
	svc := upcomingService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	show := followedShow(t, svc, user, 100, "Alpha")
	videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-20"))
	movie := videotest.SeedMovie(t, svc.DB, 900, "Soon", videotest.Ptr("2026-08-21"), nil, `{}`)
	videotest.SeedWatchlist(t, svc.DB, user, "movie", movie.ID, "2026-08-01T00:00:00Z")

	mux := http.NewServeMux()
	allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)
	return mux, user
}

func doUpcoming(t *testing.T, mux *http.ServeMux, userID int64, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/video/upcoming"+query, nil)
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

func TestGetUpcoming(t *testing.T) {
	t.Parallel()

	t.Run("returns both arms with the other arm serialized as null", func(t *testing.T) {
		t.Parallel()
		mux, user := upcomingTestMux(t)
		rec := doUpcoming(t, mux, user, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want JSON", ct)
		}

		var entries []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
			t.Fatalf("body is not a JSON array: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("len = %d, want 2", len(entries))
		}

		ep := entries[0]
		if ep["kind"] != "episode" || ep["date"] != "2026-08-20" {
			t.Fatalf("first entry = %v, want the episode on 2026-08-20", ep)
		}
		wantNull(t, ep, "movie")
		wantNull(t, ep, "poster")
		wantNull(t, ep, "poster_full")
		episode := ep["episode"].(map[string]any)
		wantNull(t, episode, "season_id")
		wantNull(t, episode, "marker")
		if name := ep["show"].(map[string]any)["name"]; name != "Alpha" {
			t.Errorf("show = %v, want Alpha", name)
		}

		mv := entries[1]
		if mv["kind"] != "movie" || mv["date"] != "2026-08-21" {
			t.Fatalf("second entry = %v, want the movie on 2026-08-21", mv)
		}
		wantNull(t, mv, "show")
		wantNull(t, mv, "episode")
		wantNull(t, mv, "poster")
		wantNull(t, mv, "poster_full")
		if title := mv["movie"].(map[string]any)["title"]; title != "Soon" {
			t.Errorf("movie = %v, want Soon", title)
		}
	})

	t.Run("a window with nothing in it is [] never null", func(t *testing.T) {
		t.Parallel()
		mux, user := upcomingTestMux(t)
		rec := doUpcoming(t, mux, user, "?days=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body = %s, want []", body)
		}
	})

	t.Run("another user's calendar is empty", func(t *testing.T) {
		t.Parallel()
		mux, _ := upcomingTestMux(t)
		rec := doUpcoming(t, mux, 999, "")
		if body := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || body != "[]" {
			t.Fatalf("status = %d body = %s, want 200 []", rec.Code, body)
		}
	})

	invalid := []struct {
		name  string
		query string
	}{
		{"invalid type", "?type=season"},
		{"invalid days", "?days=0"},
		{"invalid from", "?from=today"},
	}
	for _, tt := range invalid {
		t.Run(tt.name+" returns 422 envelope", func(t *testing.T) {
			t.Parallel()
			mux, user := upcomingTestMux(t)
			rec := doUpcoming(t, mux, user, tt.query)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "validation" {
				t.Fatalf("body = %s, want the validation envelope", rec.Body)
			}
		})
	}
}
