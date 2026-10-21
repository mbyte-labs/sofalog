package jellyfin

import (
	"encoding/json"
	"net/http"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestJellyfinHandlers(t *testing.T) {
	t.Parallel()

	t.Run("unconfigured: status says so and scan is a 404", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "GET", "/api/v1/video/jellyfin", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		var st JellyfinStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatalf("body: %v", err)
		}
		if st.Configured || st.Last != nil || st.ServerName != nil {
			t.Errorf("status = %+v", st)
		}
		rec = videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/jellyfin/scan", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("scan status = %d, want 404: %s", rec.Code, rec.Body)
		}
	})

	t.Run("configured: scan is a 202, a running scan a 409", func(t *testing.T) {
		t.Parallel()
		svc := jellyfinService(t, &videotest.JellyfinFake{})
		userID := videotest.SeedUser(t, svc.DB, "alice")
		mux := http.NewServeMux()
		allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)

		rec := videotest.DoTracking(t, mux, userID, "GET", "/api/v1/video/jellyfin", "")
		var st JellyfinStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatalf("body: %v", err)
		}
		if !st.Configured || st.Scanning {
			t.Errorf("status = %+v", st)
		}
		rec = videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/jellyfin/scan", "")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("scan status = %d, want 202: %s", rec.Code, rec.Body)
		}
		svc.jellyfinScan.mu.Lock()
		svc.jellyfinScan.running = true
		svc.jellyfinScan.mu.Unlock()
		rec = videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/jellyfin/scan", "")
		if rec.Code != http.StatusConflict {
			t.Errorf("second scan status = %d, want 409: %s", rec.Code, rec.Body)
		}
		// The copy route refuses the managed kind.
		rec = videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/copies",
			`{"target_type": "movie", "tmdb_id": 603, "kind": "jellyfin"}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("copy status = %d, want 422: %s", rec.Code, rec.Body)
		}
	})
}
