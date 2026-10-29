package lists

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// TestGetDropped checks the route's wire shape. The service carries NO
// provider client, so a render path that reached TMDB would panic rather than
// pass.
func TestGetDropped(t *testing.T) {
	t.Parallel()
	svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
	alice := videotest.SeedUser(t, svc.DB, "alice")
	sh := seedShowSnapshot(t, svc.DB, 1399, "Mars Signal", videotest.Ptr("1990-03-04"), nil, `{}`)
	seedShowState(t, svc.DB, alice, sh.ID, "dropped", "2026-08-01T00:00:00Z")

	mux := http.NewServeMux()
	allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)
	serve := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		req = req.WithContext(httpx.WithUserID(req.Context(), alice))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := serve("/api/v1/video/dropped")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body) != 1 || body[0]["target_type"] != "show" || body[0]["added_at"] != "2026-08-01T00:00:00Z" {
		t.Fatalf("body = %v, want one dropped show", body)
	}
	if movie, ok := body[0]["movie"]; !ok || movie != nil {
		t.Errorf("movie = %#v, want an explicit null", movie)
	}

	if rec := serve("/api/v1/video/dropped?order_by=rating"); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad order_by status = %d, want 422", rec.Code)
	}
}
