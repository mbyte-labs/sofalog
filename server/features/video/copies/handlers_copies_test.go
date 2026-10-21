package copies

import (
	"encoding/json"
	"net/http"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestCopiesHandlers(t *testing.T) {
	t.Parallel()

	t.Run("POST /video/copies then DELETE round-trips", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/copies",
			`{"target_type": "movie", "tmdb_id": 603, "kind": "dvd"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var body struct {
			TargetType string `json:"target_type"`
			TargetID   int64  `json:"target_id"`
			Kind       string `json:"kind"`
			Created    int64  `json:"created"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if body.TargetType != "movie" || body.TargetID < 1 || body.Kind != "dvd" || body.Created != 1 {
			t.Errorf("body = %+v", body)
		}

		rec = videotest.DoTracking(t, mux, userID, "DELETE",
			"/api/v1/video/copies/movie/"+videotest.JSONNumber(body.TargetID)+"/dvd", "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d, want 204: %s", rec.Code, rec.Body)
		}
		// Idempotent: deleting it again is still a 204.
		rec = videotest.DoTracking(t, mux, userID, "DELETE",
			"/api/v1/video/copies/movie/"+videotest.JSONNumber(body.TargetID)+"/dvd", "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("repeat delete status = %d, want 204: %s", rec.Code, rec.Body)
		}
	})

	t.Run("a malformed body is a 400 envelope", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/copies", `{"target_type":`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "invalid_body" {
			t.Errorf("code = %q, want invalid_body", code)
		}
	})

	t.Run("bad values are 422 envelopes", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		tests := []struct {
			name, method, path, body string
		}{
			{"unknown kind", "POST", "/api/v1/video/copies",
				`{"target_type": "movie", "tmdb_id": 603, "kind": "betamax"}`},
			{"episode target", "POST", "/api/v1/video/copies",
				`{"target_type": "episode", "target_id": 1, "kind": "dvd"}`},
			{"delete with unknown kind", "DELETE", "/api/v1/video/copies/movie/1/betamax", ""},
			{"delete with episode target", "DELETE", "/api/v1/video/copies/episode/1/dvd", ""},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				rec := videotest.DoTracking(t, mux, userID, tc.method, tc.path, tc.body)
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
				}
				if code := videotest.ErrorCode(t, rec); code != "validation" {
					t.Errorf("code = %q, want validation", code)
				}
			})
		}
	})
}
