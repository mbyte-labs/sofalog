package restore

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestRestoreHandlers(t *testing.T) {
	t.Parallel()

	t.Run("GET before any start is a 404", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "GET", "/api/v1/video/restore/ticket-box", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "not_found" {
			t.Errorf("code = %q, want not_found", code)
		}
	})

	t.Run("POST accepts a v5 file and GET then reports it", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		// An empty file: the job has nothing to do, so it finishes at once and
		// the test never leaves a goroutine writing into a closed database.
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/restore/ticket-box",
			`{"schemaVersion": 5, "exportedAt": "2026-10-10T19:53:54.865687Z", "history": [], "ratings": [], "watchlist": [], "favorites": []}`)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body)
		}
		var job RestoreJob
		if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if job.Kind != "ticket_box" || job.Progress.GroupsTotal != 0 {
			t.Errorf("job = %+v", job)
		}

		deadline := time.Now().Add(5 * time.Second)
		for {
			rec = videotest.DoTracking(t, mux, userID, "GET", "/api/v1/video/restore/ticket-box", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if job.State != "running" || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if job.State != "done" || job.FinishedAt == nil {
			t.Errorf("job = %+v, want done", job)
		}
	})

	t.Run("malformed JSON is a 400", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/restore/ticket-box", `{`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "invalid_body" {
			t.Errorf("code = %q, want invalid_body", code)
		}
	})

	t.Run("a version-1 file is a 422", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/restore/ticket-box",
			`{"schemaVersion": 1, "exportedAt": "x", "history": [], "ratings": []}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "validation" {
			t.Errorf("code = %q, want validation", code)
		}
	})
}
