package lists

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// doFavorites issues the GET with the user id in its context, exactly as the
// session middleware sets it. The fixture's service carries NO provider
// client, so a render path that reached TMDB would panic rather than pass.
func doFavorites(t *testing.T, mux *http.ServeMux, userID int64, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/video/favorites"+query, nil)
	req = req.WithContext(httpx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestGetFavorites(t *testing.T) {
	t.Parallel()

	t.Run("returns the list most recently favorited first", func(t *testing.T) {
		t.Parallel()
		f := newFavoritesFixture(t)
		mux := http.NewServeMux()
		allHandlers(f.svc, videotest.Cookie).RegisterRoutes(mux)

		rec := doFavorites(t, mux, f.alice, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want JSON", ct)
		}
		entries := videotest.DecodeWatchlist(t, rec)
		if len(entries) != 4 {
			t.Fatalf("len = %d, want 4", len(entries))
		}
		if got := entries[0]["added_at"]; got != "2026-08-15T21:05:00Z" {
			t.Errorf("first added_at = %v, want 2026-08-15T21:05:00Z (desc default)", got)
		}
		// The watchlist card's shape, union keys included.
		for _, e := range entries {
			movie, hasMovie := e["movie"]
			show, hasShow := e["show"]
			if !hasMovie || !hasShow || (movie == nil) == (show == nil) {
				t.Errorf("entry %v: want both union keys present and exactly one filled", e)
			}
		}

		if rec := doFavorites(t, mux, f.alice, "?type=show"); len(videotest.DecodeWatchlist(t, rec)) != 1 {
			t.Errorf("type=show = %s, want the one show", rec.Body)
		}
		if rec := doFavorites(t, mux, f.alice, "?order_by=rating"); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("bad order_by status = %d, want 422", rec.Code)
		}
		if got := strings.TrimSpace(doFavorites(t, mux, videotest.SeedUser(t, f.svc.DB, "nobody"), "").Body.String()); got != "[]" {
			t.Errorf("empty list = %s, want []", got)
		}
	})
}

func TestFavoriteHandlers(t *testing.T) {
	t.Parallel()

	t.Run("POST /video/favorites then DELETE round-trips for a movie", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/favorites",
			`{"target_type": "movie", "tmdb_id": 603}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("add status = %d: %s", rec.Code, rec.Body)
		}
		var body struct {
			Created    bool   `json:"created"`
			TargetType string `json:"target_type"`
			TargetID   int64  `json:"target_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if !body.Created || body.TargetType != "movie" || body.TargetID < 1 {
			t.Errorf("body = %+v", body)
		}

		// The details view reports it, and the list holds it.
		rec = videotest.DoTracking(t, mux, userID, "GET", "/api/v1/video/movies/details?id="+videotest.JSONNumber(body.TargetID), "")
		var details map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &details); err != nil || details["favorited"] != true {
			t.Errorf("details favorited = %v (%v), want true", details["favorited"], err)
		}
		if entries := videotest.DecodeWatchlist(t, doFavorites(t, mux, userID, "")); len(entries) != 1 {
			t.Errorf("favorites = %d entries, want 1", len(entries))
		}

		for i := 0; i < 2; i++ { // idempotent: the second delete is still 204
			rec = videotest.DoTracking(t, mux, userID, "DELETE",
				"/api/v1/video/favorites/movie/"+videotest.JSONNumber(body.TargetID), "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("delete #%d status = %d, want 204: %s", i+1, rec.Code, rec.Body)
			}
		}
		if got := strings.TrimSpace(doFavorites(t, mux, userID, "").Body.String()); got != "[]" {
			t.Fatalf("favorites after removal = %s, want []", got)
		}
	})

	t.Run("a show round-trips through the polymorphic path", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/favorites",
			`{"target_type": "show", "tmdb_id": 1396}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("add status = %d: %s", rec.Code, rec.Body)
		}
		var body struct {
			TargetID int64 `json:"target_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		entries := videotest.DecodeWatchlist(t, doFavorites(t, mux, userID, "?type=show"))
		if len(entries) != 1 || entries[0]["target_type"] != "show" {
			t.Fatalf("favorites = %v, want the one show", entries)
		}
		rec = videotest.DoTracking(t, mux, userID, "DELETE", "/api/v1/video/favorites/show/"+videotest.JSONNumber(body.TargetID), "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d, want 204: %s", rec.Code, rec.Body)
		}
	})

	t.Run("refused targets are 422 and touch no provider", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		mux, userID := trackingTestMux(t, stub)
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/favorites",
			`{"target_type": "episode", "target_id": 1}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("episode add status = %d, want 422: %s", rec.Code, rec.Body)
		}
		rec = videotest.DoTracking(t, mux, userID, "DELETE", "/api/v1/video/favorites/season/1", "")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("season delete status = %d, want 422: %s", rec.Code, rec.Body)
		}
		rec = videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/favorites", `{"target_type": "movie", "target_id": 4242}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("unknown local id status = %d, want 404: %s", rec.Code, rec.Body)
		}
		rec = videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/favorites", `{not json`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("bad body status = %d, want 400", rec.Code)
		}
	})
}
