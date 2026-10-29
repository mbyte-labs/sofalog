package watches

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/media"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

func isValidationError(err error) bool { return errors.Is(err, httpx.ErrValidation) }

// bulkTestMux wires the real routes over a materialised show, so these tests
// exercise the ROUTE as well as the service — the registration itself is worth
// covering, because a handler with no route is invisible to a service test.
func bulkTestMux(t *testing.T) (*http.ServeMux, int64) {
	t.Helper()
	stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
	mux, userID := trackingTestMux(t, stub)
	return mux, userID
}

func TestBulkWatchHandler(t *testing.T) {
	t.Parallel()

	t.Run("POST /video/watches/bulk marks the show", func(t *testing.T) {
		t.Parallel()
		mux, userID := bulkTestMux(t)
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches/bulk",
			`{"scope": "show", "target_type": "show", "tmdb_id": 1396}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got BulkWatchResult
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if got.Created != 4 || got.Status == nil || *got.Status != "watched" {
			t.Errorf("result = %+v, want 4 created and status watched", got)
		}
	})

	t.Run("bad bodies", func(t *testing.T) {
		t.Parallel()
		mux, userID := bulkTestMux(t)
		tests := []struct {
			body string
			want int
		}{
			{`{`, http.StatusBadRequest},
			// A scope/target mismatch: scope season takes a season target.
			{`{"scope": "season", "target_type": "show", "tmdb_id": 1396}`, http.StatusUnprocessableEntity},
			{`{"scope": "season", "target_type": "season", "tmdb_id": 1396}`, http.StatusUnprocessableEntity},
			{`{"scope": "show", "target_type": "movie", "tmdb_id": 603}`, http.StatusUnprocessableEntity},
			{`{"scope": "show", "target_type": "show"}`, http.StatusUnprocessableEntity},
			{`{"scope": "show", "target_type": "show", "target_id": 999}`, http.StatusNotFound},
			{`{"scope": "show", "target_type": "show", "tmdb_id": 1396, "watched_at_mode": "air_date"}`,
				http.StatusUnprocessableEntity},
			{`{"scope": "show", "target_type": "show", "tmdb_id": 1396, "watched_at_mode": "fixed"}`,
				http.StatusUnprocessableEntity},
			{`{"scope": "show", "target_type": "show", "tmdb_id": 1396,
			   "watched_at_mode": "fixed", "watched_at": "2036-01-01T00:00:00Z"}`,
				http.StatusUnprocessableEntity},
			{`{"scope": "show", "target_type": "show", "tmdb_id": 1396, "watched_at": "2020-01-01T00:00:00Z"}`,
				http.StatusUnprocessableEntity},
		}
		for _, tc := range tests {
			rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches/bulk", tc.body)
			if rec.Code != tc.want {
				t.Errorf("%s → status %d, want %d: %s", tc.body, rec.Code, tc.want, rec.Body)
			}
		}
	})

	// The season scope over the route: one season's aired episodes (season 2
	// has three of its four dated) and the show's status back.
	t.Run("POST /video/watches/bulk marks one season", func(t *testing.T) {
		t.Parallel()
		mux, userID := bulkTestMux(t)
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches/bulk",
			`{"scope": "season", "target_type": "season", "tmdb_id": 1396, "season": 2}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got BulkWatchResult
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if got.Created != 3 || got.TargetType != "season" {
			t.Errorf("result = %+v, want 3 created on a season target", got)
		}
		if got.Status == nil || *got.Status != "watching" {
			t.Errorf("status = %v, want watching — season 1 is untouched", got.Status)
		}
	})

	// The widened body over the route: a dated full rewatch in one call.
	t.Run("POST /video/watches/bulk accepts the mode and rewatch fields", func(t *testing.T) {
		t.Parallel()
		mux, userID := bulkTestMux(t)
		if rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches/bulk",
			`{"scope": "show", "target_type": "show", "tmdb_id": 1396}`); rec.Code != http.StatusOK {
			t.Fatalf("first mark status = %d: %s", rec.Code, rec.Body)
		}
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches/bulk",
			`{"scope": "show", "target_type": "show", "tmdb_id": 1396,
			  "rewatch": true, "watched_at_mode": "episode_air"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got BulkWatchResult
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if got.Created != 4 || got.Status == nil || *got.Status != "watched" {
			t.Errorf("result = %+v, want 4 created (a rewatch re-inserts) and status watched", got)
		}
	})

	// The episode arm of POST /video/watches, over the route, including the
	// show summary the episode page settles from.
	t.Run("POST /video/watches marks one episode and reports the show", func(t *testing.T) {
		t.Parallel()
		mux, userID := bulkTestMux(t)
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "episode", "tmdb_id": 1396, "season": 1, "episode": 1}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got common.TrackResult
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if !got.Created || got.TargetType != "episode" {
			t.Fatalf("result = %+v", got)
		}
		if got.Show == nil || got.Show.Status == nil || *got.Show.Status != "watching" {
			t.Errorf("show = %+v, want status watching", got.Show)
		}
	})

	// The movie contract must be byte-identical: show is an added nullable
	// field, never a populated one for a movie.
	t.Run("a movie result still carries a null show", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "movie", "tmdb_id": 603}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if v, ok := raw["show"]; !ok || v != nil {
			t.Errorf("show = %v (present %v), want an explicit null", v, ok)
		}
	})
}

func TestShowAssetHandler(t *testing.T) {
	t.Parallel()

	t.Run("a miss is a 404, not a panic", func(t *testing.T) {
		t.Parallel()
		mux, userID := bulkTestMux(t)
		rec := videotest.DoTracking(t, mux, userID, "GET", "/api/v1/video/shows/1/assets/poster.jpg", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
	})

	// The episode directory name carries a hyphen, which the path element
	// check must not reject.
	t.Run("an episode still path is a valid asset path", func(t *testing.T) {
		t.Parallel()
		svc, _ := detailsService(t, &videotest.ProviderStub{})
		_, _, err := media.NewService(svc.Core).ShowAsset(context.Background(), "1", "episodes/2-7/still.thumb.jpg")
		if err == nil {
			t.Fatal("want a not-found error for an absent file")
		}
		if isValidationError(err) {
			t.Errorf("error = %v, want a not-found — the hyphen must not fail validation", err)
		}
	})

	t.Run("path traversal is refused", func(t *testing.T) {
		t.Parallel()
		svc, _ := detailsService(t, &videotest.ProviderStub{})
		for _, file := range []string{"../secret.jpg", "a/../../b.jpg", "poster.txt", ""} {
			if _, _, err := media.NewService(svc.Core).ShowAsset(context.Background(), "1", file); !isValidationError(err) {
				t.Errorf("ShowAsset(%q) error = %v, want a validation error", file, err)
			}
		}
	})
}
