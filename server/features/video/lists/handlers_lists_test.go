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

// TestGetLists checks the route's wire shape. The service carries NO provider
// client, so a render path that reached TMDB would panic rather than pass.
func TestGetLists(t *testing.T) {
	t.Parallel()
	svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
	alice := videotest.SeedUser(t, svc.DB, "alice")
	m := videotest.SeedMovie(t, svc.DB, 603, "The Matrix", nil, nil, `{}`)
	videotest.SeedFavorite(t, svc.DB, alice, "movie", m.ID, "2026-08-01T00:00:00Z")

	mux := http.NewServeMux()
	allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)
	req := httptest.NewRequest("GET", "/api/v1/video/lists", nil)
	req = req.WithContext(httpx.WithUserID(req.Context(), alice))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body) != 3 || body[0]["key"] != "watchlist" || body[1]["key"] != "favorites" || body[2]["key"] != "dropped" {
		t.Fatalf("body = %v, want watchlist, favorites, dropped", body)
	}
	if body[1]["movies"] != float64(1) || body[1]["shows"] != float64(0) {
		t.Errorf("favorites = %v, want 1 movie, 0 shows", body[1])
	}
	// An empty backdrop list is [], never null.
	if bd, ok := body[0]["backdrops"].([]any); !ok || len(bd) != 0 {
		t.Errorf("watchlist backdrops = %#v, want []", body[0]["backdrops"])
	}
	if bd, ok := body[0]["backdrops_full"].([]any); !ok || len(bd) != 0 {
		t.Errorf("watchlist backdrops_full = %#v, want []", body[0]["backdrops_full"])
	}
}
