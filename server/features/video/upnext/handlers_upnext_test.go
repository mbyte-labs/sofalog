package upnext

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// upNextTestMux wires the routes over a small seeded fixture: one started show
// whose next episode is undated, so every nullable field of the contract is
// exercised as an explicit null. The service carries NO provider client, so a
// render path reaching TMDB would panic here rather than pass.
func upNextTestMux(t *testing.T) (*http.ServeMux, int64) {
	t.Helper()
	svc := upNextService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
	e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
	videotest.SeedEpisode(t, svc.DB, show, 1, 2, nil)
	videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")

	mux := http.NewServeMux()
	allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)
	return mux, user
}

// doUpNext issues the request with the user id in its context, exactly as the
// session middleware sets it.
func doUpNext(t *testing.T, mux *http.ServeMux, userID int64, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/video/up-next"+query, nil)
	req = req.WithContext(httpx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestGetUpNext(t *testing.T) {
	t.Parallel()

	t.Run("returns entries with nullable fields serialized as null", func(t *testing.T) {
		t.Parallel()
		mux, user := upNextTestMux(t)
		rec := doUpNext(t, mux, user, "")
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
		if len(entries) != 1 {
			t.Fatalf("len = %d, want 1", len(entries))
		}
		entry := entries[0]
		if name := entry["show"].(map[string]any)["name"]; name != "Alpha" {
			t.Errorf("show = %v, want Alpha", name)
		}

		// The undated next episode of a show with no season row: air_date,
		// rating, marker, season_id and poster must be PRESENT and null — the
		// SPA branches on null, a missing key is a contract break — and aired
		// must be present as the boolean false.
		episode := entry["episode"].(map[string]any)
		for key, obj := range map[string]map[string]any{
			"poster": entry, "poster_full": entry, "air_date": episode, "rating": episode,
			"marker": episode, "season_id": episode,
		} {
			v, present := obj[key]
			if !present {
				t.Errorf("%s key absent, want explicit null", key)
			} else if v != nil {
				t.Errorf("%s = %v, want null", key, v)
			}
		}
		if aired, ok := episode["aired"].(bool); !ok || aired {
			t.Errorf("aired = %v, want the boolean false", episode["aired"])
		}
		if lw, ok := entry["last_watched_at"].(string); !ok || lw != "2026-08-01T20:00:00Z" {
			t.Errorf("last_watched_at = %v, want the watch instant", entry["last_watched_at"])
		}
	})

	t.Run("combined params filter the undated pick away", func(t *testing.T) {
		t.Parallel()
		mux, user := upNextTestMux(t)
		rec := doUpNext(t, mux, user, "?order_by=air_date&order=asc&only_released=true")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		// The only pick is undated, so the filtered list is EMPTY — and must
		// serialize as [], never null.
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body = %s, want []", body)
		}
	})

	t.Run("another user's list is empty", func(t *testing.T) {
		t.Parallel()
		mux, _ := upNextTestMux(t)
		rec := doUpNext(t, mux, 999, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body = %s, want []", body)
		}
	})

	invalid := []struct {
		name  string
		query string
	}{
		{"invalid order_by", "?order_by=banana"},
		{"invalid order", "?order=sideways"},
		{"invalid boolean", "?only_released=maybe"},
	}
	for _, tt := range invalid {
		t.Run(tt.name+" returns 422 envelope", func(t *testing.T) {
			t.Parallel()
			mux, user := upNextTestMux(t)
			rec := doUpNext(t, mux, user, tt.query)
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
