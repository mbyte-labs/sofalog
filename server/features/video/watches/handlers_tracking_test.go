package watches

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/provider"
)

// trackingTestMux wires the routes against a stubbed provider and a temp DB
// with one seeded user, whose id every request carries — the handler reads it
// from the context exactly as the session middleware would set it.
func trackingTestMux(t *testing.T, stub *videotest.ProviderStub) (*http.ServeMux, int64) {
	t.Helper()
	if stub.Bodies == nil {
		stub.Bodies = map[string]string{}
	}
	tmdb := provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"}, provider.WithHTTPDoer(stub))
	database, dataDir := videotest.DB(t), t.TempDir()
	cache, tree := assets.NewCache(dataDir), assets.NewTree(dataDir)
	svc := NewService(common.NewCore(database, tmdb, cache, provider.NewYouTube(provider.WithYouTubeHTTPDoer(stub)),
		tree, assets.NewQueue(database, tree, cache, nil), videotest.WatchRegion, 0, 0, videotest.LinkRedirectURI,
		provider.NewJellyfin("", ""), 0))
	userID := videotest.SeedUser(t, svc.DB, "alice")
	mux := http.NewServeMux()
	allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)
	return mux, userID
}

func TestTrackingHandlers(t *testing.T) {
	t.Parallel()

	t.Run("POST /video/watches returns the tracking result", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "movie", "tmdb_id": 603}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
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
	})

	// The wire form of the dated-log mode: the two extra body fields reach the
	// service, and its refusals arrive as 422 rather than 500.
	t.Run("POST /video/watches accepts watched_at and rating", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "movie", "tmdb_id": 603, "watched_at": "2020-05-01T20:15:00Z", "rating": 9}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}

		// Logging it again is a rewatch, not a no-op — the whole point of
		// naming an instant.
		rec = videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "movie", "tmdb_id": 603, "watched_at": "2021-05-01T20:15:00Z"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("second status = %d, want 200: %s", rec.Code, rec.Body)
		}
		rec = videotest.DoTracking(t, mux, userID, "GET", "/api/v1/video/history?limit=10", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("history status = %d: %s", rec.Code, rec.Body)
		}
		var entries []struct {
			WatchedAt string `json:"watched_at"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
			t.Fatalf("history body is not JSON: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("history entries = %d, want 2: %s", len(entries), rec.Body)
		}
		if entries[0].WatchedAt != "2021-05-01T20:15:00Z" {
			t.Errorf("newest entry watched_at = %q", entries[0].WatchedAt)
		}
	})

	t.Run("POST /video/watches refuses a future watched_at with 422", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "movie", "tmdb_id": 603, "watched_at": "2999-01-01T00:00:00Z"}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
		}
		// Refused before the target is resolved, so nothing was materialised.
		if n := stub.CallCount("/3/movie/603"); n != 0 {
			t.Errorf("a refused body reached TMDB %d times", n)
		}
	})

	t.Run("POST /video/watchlist then DELETE round-trips", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watchlist",
			`{"target_type": "movie", "tmdb_id": 603}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("add status = %d: %s", rec.Code, rec.Body)
		}
		var body struct {
			TargetID int64 `json:"target_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}

		rec = videotest.DoTracking(t, mux, userID, "DELETE",
			"/api/v1/video/watchlist/movie/"+videotest.JSONNumber(body.TargetID), "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d, want 204: %s", rec.Code, rec.Body)
		}
		// Idempotent: deleting again is still 204.
		rec = videotest.DoTracking(t, mux, userID, "DELETE",
			"/api/v1/video/watchlist/movie/"+videotest.JSONNumber(body.TargetID), "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("second delete status = %d, want 204: %s", rec.Code, rec.Body)
		}
	})

	// The DELETE route is polymorphic — {target_type}/{target_id} — so the show
	// arm proves the path is read, not assumed.
	t.Run("POST /video/watchlist then DELETE round-trips for a show", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watchlist",
			`{"target_type": "show", "tmdb_id": 1396}`)
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
		if !body.Created || body.TargetType != "show" || body.TargetID < 1 {
			t.Errorf("body = %+v", body)
		}

		// It shows up on the list — the whole reason a show may be listed now.
		rec = videotest.DoWatchlist(t, mux, userID, "?type=show")
		entries := videotest.DecodeWatchlist(t, rec)
		if len(entries) != 1 || entries[0]["target_type"] != "show" {
			t.Fatalf("watchlist = %s, want the one show", rec.Body)
		}

		for i := 0; i < 2; i++ { // idempotent: the second delete is still 204
			rec = videotest.DoTracking(t, mux, userID, "DELETE",
				"/api/v1/video/watchlist/show/"+videotest.JSONNumber(body.TargetID), "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("delete #%d status = %d, want 204: %s", i+1, rec.Code, rec.Body)
			}
		}
		if got := strings.TrimSpace(videotest.DoWatchlist(t, mux, userID, "").Body.String()); got != "[]" {
			t.Fatalf("watchlist after removal = %s, want []", got)
		}
	})

	// The drop pair. Show-only, so the id is in the path and neither direction
	// carries a body; the DELETE answers 200 with the recomputed state rather
	// than the 204 the watchlist removal above uses.
	t.Run("POST then DELETE /video/shows/{id}/drop round-trips", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		mux, userID := trackingTestMux(t, stub)

		// Marking an episode is what makes the show droppable at all.
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "episode", "tmdb_id": 1396, "season": 1, "episode": 1}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("mark status = %d: %s", rec.Code, rec.Body)
		}
		var marked struct {
			Show struct {
				ID int64 `json:"id"`
			} `json:"show"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &marked); err != nil {
			t.Fatalf("mark body is not JSON: %v", err)
		}
		path := "/api/v1/video/shows/" + videotest.JSONNumber(marked.Show.ID) + "/drop"

		type dropBody struct {
			Changed bool `json:"changed"`
			Show    struct {
				ID     int64   `json:"id"`
				Status *string `json:"status"`
			} `json:"show"`
		}
		decode := func(rec *httptest.ResponseRecorder) dropBody {
			t.Helper()
			var body dropBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			return body
		}

		rec = videotest.DoTracking(t, mux, userID, "POST", path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("drop status = %d, want 200: %s", rec.Code, rec.Body)
		}
		body := decode(rec)
		if !body.Changed || body.Show.ID != marked.Show.ID ||
			body.Show.Status == nil || *body.Show.Status != "dropped" {
			t.Fatalf("drop body = %+v", body)
		}

		// Idempotent: a repeat is a 200 that says it changed nothing.
		if body := decode(videotest.DoTracking(t, mux, userID, "POST", path, "")); body.Changed {
			t.Error("second drop reported changed = true")
		}

		rec = videotest.DoTracking(t, mux, userID, "DELETE", path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("un-drop status = %d, want 200: %s", rec.Code, rec.Body)
		}
		body = decode(rec)
		if !body.Changed || body.Show.Status == nil || *body.Show.Status != "watching" {
			t.Fatalf("un-drop body = %+v", body)
		}
		if body := decode(videotest.DoTracking(t, mux, userID, "DELETE", path, "")); body.Changed {
			t.Error("second un-drop reported changed = true")
		}
	})

	t.Run("dropping an unstarted show is a 422 envelope", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		mux, userID := trackingTestMux(t, stub)

		// Watchlisting materialises the show without starting it.
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watchlist",
			`{"target_type": "show", "tmdb_id": 1396}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("add status = %d: %s", rec.Code, rec.Body)
		}
		var listed struct {
			TargetID int64 `json:"target_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}

		rec = videotest.DoTracking(t, mux, userID, "POST",
			"/api/v1/video/shows/"+videotest.JSONNumber(listed.TargetID)+"/drop", "")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "validation" {
			t.Errorf("code = %q, want validation", code)
		}
	})

	t.Run("dropping an unknown show is a 404 envelope", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/shows/4242/drop", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "not_found" {
			t.Errorf("code = %q, want not_found", code)
		}
	})

	t.Run("a malformed body is a 400 envelope", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches", `{not json`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "invalid_body" {
			t.Errorf("code = %q, want invalid_body", code)
		}
	})

	t.Run("an unsupported target_type is a 422 envelope", func(t *testing.T) {
		t.Parallel()
		mux, userID := trackingTestMux(t, &videotest.ProviderStub{})
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "show", "tmdb_id": 1}`)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "validation" {
			t.Errorf("code = %q, want validation", code)
		}
	})

	t.Run("TMDB down on first track is a 503 envelope", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/movie/603": videotest.ErrTimeout}}
		mux, userID := trackingTestMux(t, stub)
		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "movie", "tmdb_id": 603}`)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "provider_unavailable" {
			t.Errorf("code = %q, want provider_unavailable", code)
		}
	})

	t.Run("POST then DELETE removes the watch, and DELETE is not idempotent", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		mux, userID := trackingTestMux(t, stub)

		rec := videotest.DoTracking(t, mux, userID, "POST", "/api/v1/video/watches",
			`{"target_type": "movie", "tmdb_id": 603}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("post status = %d: %s", rec.Code, rec.Body)
		}

		// The watch-row id is not in the track response — it is what the
		// history feed keys its entries on, so fetch it the way a client does.
		rec = videotest.DoTracking(t, mux, userID, "GET", "/api/v1/video/history", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("history status = %d: %s", rec.Code, rec.Body)
		}
		var feed []struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("history body is not JSON: %v", err)
		}
		if len(feed) != 1 {
			t.Fatalf("history entries = %d, want 1", len(feed))
		}
		path := "/api/v1/video/watches/" + videotest.JSONNumber(feed[0].ID)

		if rec = videotest.DoTracking(t, mux, userID, "DELETE", path, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d, want 204: %s", rec.Code, rec.Body)
		}
		// Deliberately unlike the ratings and watchlist deletes: this one names
		// one row, so deleting it twice is a 404, not a second success.
		if rec = videotest.DoTracking(t, mux, userID, "DELETE", path, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("second delete status = %d, want 404: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "not_found" {
			t.Errorf("code = %q, want not_found", code)
		}

		if rec = videotest.DoTracking(t, mux, userID, "DELETE", "/api/v1/video/watches/abc", ""); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("malformed id status = %d, want 422: %s", rec.Code, rec.Body)
		}
	})
}
