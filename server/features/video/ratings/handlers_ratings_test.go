package ratings

import (
	"encoding/json"
	"net/http"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestRatingHandlers(t *testing.T) {
	t.Parallel()

	t.Run("PUT /video/ratings stores the rating and returns the local id", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "PUT", "/api/v1/video/ratings",
			`{"target_type": "movie", "tmdb_id": 603, "rating": 7}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var body struct {
			TargetType string `json:"target_type"`
			TargetID   int64  `json:"target_id"`
			Rating     int    `json:"rating"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if body.TargetType != "movie" || body.TargetID < 1 || body.Rating != 7 {
			t.Errorf("body = %+v", body)
		}

		// The details view now serves the rating back, so a reload of the page
		// initialises the widget from the server rather than from nothing.
		rec = videotest.DoTracking(t, mux, userID, "GET",
			"/api/v1/video/movies/details?id="+videotest.JSONNumber(body.TargetID), "")
		if rec.Code != http.StatusOK {
			t.Fatalf("details status = %d: %s", rec.Code, rec.Body)
		}
		var details map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &details); err != nil {
			t.Fatalf("details body is not JSON: %v", err)
		}
		if details["user_rating"] != float64(7) {
			t.Errorf("user_rating = %v, want 7", details["user_rating"])
		}
	})

	t.Run("PUT then DELETE round-trips and DELETE is idempotent", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "PUT", "/api/v1/video/ratings",
			`{"target_type": "movie", "tmdb_id": 603, "rating": 3}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("put status = %d: %s", rec.Code, rec.Body)
		}
		var body struct {
			TargetID int64 `json:"target_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		path := "/api/v1/video/ratings/movie/" + videotest.JSONNumber(body.TargetID)

		for i := 0; i < 2; i++ {
			rec = videotest.DoTracking(t, mux, userID, "DELETE", path, "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("delete %d status = %d, want 204: %s", i+1, rec.Code, rec.Body)
			}
		}
	})

	t.Run("a malformed body is a 400 envelope", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "PUT", "/api/v1/video/ratings", `{not json`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "invalid_body" {
			t.Errorf("code = %q, want invalid_body", code)
		}
	})

	t.Run("an unusable rating or target is a 422 envelope", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		for _, body := range []string{
			`{"target_type": "movie", "tmdb_id": 603, "rating": 0}`,
			`{"target_type": "movie", "tmdb_id": 603, "rating": 11}`,
			`{"target_type": "movie", "tmdb_id": 603}`,
			`{"target_type": "season", "tmdb_id": 1, "rating": 5}`,
			`{"target_type": "movie", "rating": 5}`,
		} {
			rec := videotest.DoTracking(t, mux, userID, "PUT", "/api/v1/video/ratings", body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s → status %d, want 422: %s", body, rec.Code, rec.Body)
			}
			if code := videotest.ErrorCode(t, rec); code != "validation" {
				t.Errorf("%s → code %q, want validation", body, code)
			}
		}
	})

	t.Run("TMDB down on a first rating is a 503 envelope", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/movie/603": videotest.ErrTimeout}}
		mux, userID := trackingTestMux(t, stub)
		rec := videotest.DoTracking(t, mux, userID, "PUT", "/api/v1/video/ratings",
			`{"target_type": "movie", "tmdb_id": 603, "rating": 6}`)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "provider_unavailable" {
			t.Errorf("code = %q, want provider_unavailable", code)
		}
	})
}
