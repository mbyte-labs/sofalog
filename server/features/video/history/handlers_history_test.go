package history

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// historyTestMux wires the routes over the seeded fixture. The service carries
// NO provider client at all, so a render path that reached TMDB would panic
// here rather than pass — the local-first rule, enforced by construction.
func historyTestMux(t *testing.T) (*http.ServeMux, historyFixture) {
	t.Helper()
	f := newHistoryFixture(t)
	mux := http.NewServeMux()
	allHandlers(f.svc, videotest.Cookie).RegisterRoutes(mux)
	return mux, f
}

// doHistory issues the request with the user id in its context, exactly as the
// session middleware sets it.
func doHistory(t *testing.T, mux *http.ServeMux, userID int64, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/video/history"+query, nil)
	req = req.WithContext(httpx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeHistory(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("body is not a JSON array: %v", err)
	}
	return entries
}

func TestGetHistory(t *testing.T) {
	t.Parallel()

	t.Run("returns the feed newest first", func(t *testing.T) {
		t.Parallel()
		mux, f := historyTestMux(t)
		rec := doHistory(t, mux, f.alice, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want JSON", ct)
		}

		entries := decodeHistory(t, rec)
		if len(entries) != 4 {
			t.Fatalf("len = %d, want 4", len(entries))
		}
		if got := entries[0]["watched_at"]; got != "2026-08-15T21:05:00Z" {
			t.Errorf("first watched_at = %v, want 2026-08-15T21:05:00Z (desc default)", got)
		}
	})

	// Every nullable key must be PRESENT and null — the SPA branches on null,
	// so a missing key is a contract break.
	t.Run("nullable fields serialize as explicit null", func(t *testing.T) {
		t.Parallel()
		mux, f := historyTestMux(t)
		entries := decodeHistory(t, doHistory(t, mux, f.alice, ""))

		var bare map[string]any
		for _, e := range entries {
			// The episode arm is empty in M1, so it must be present and null on
			// EVERY entry — the union key is part of the published shape.
			v, present := e["episode"]
			if !present {
				t.Errorf("entry %v: episode key absent, want explicit null", e["id"])
			} else if v != nil {
				t.Errorf("entry %v: episode = %v, want null", e["id"], v)
			}
			if e["movie"] == nil {
				t.Errorf("entry %v: movie is null, want an object", e["id"])
			}
			if e["poster"] == nil && e["user_rating"] == nil && e["rating"] == nil {
				bare = e
			}
		}
		if bare == nil {
			t.Fatal("fixture no longer has an entry with no poster, no score and no rating")
		}
		for _, key := range []string{"poster", "poster_full", "user_rating", "rating"} {
			if _, present := bare[key]; !present {
				t.Errorf("bare row: %s key absent, want explicit null", key)
			}
		}
	})

	t.Run("type=movie is the whole feed in M1", func(t *testing.T) {
		t.Parallel()
		mux, f := historyTestMux(t)
		entries := decodeHistory(t, doHistory(t, mux, f.alice, "?type=movie&order=asc"))
		if len(entries) != 4 {
			t.Fatalf("len = %d, want 4", len(entries))
		}
		for _, e := range entries {
			if got := e["target_type"]; got != "movie" {
				t.Errorf("target_type = %v, want movie", got)
			}
		}
	})

	t.Run("type=episode returns an empty array, not null", func(t *testing.T) {
		t.Parallel()
		mux, f := historyTestMux(t)
		rec := doHistory(t, mux, f.alice, "?type=episode")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body = %s, want []", body)
		}
	})

	t.Run("limit and offset page the ordered feed", func(t *testing.T) {
		t.Parallel()
		mux, f := historyTestMux(t)
		entries := decodeHistory(t, doHistory(t, mux, f.alice, "?limit=2&offset=2"))
		if len(entries) != 2 {
			t.Fatalf("len = %d, want 2", len(entries))
		}
		if got := entries[0]["id"]; got != float64(f.w2) {
			t.Errorf("first id = %v, want %d (third entry of the desc feed)", got, f.w2)
		}
	})

	t.Run("paging past the end returns an empty array, not null", func(t *testing.T) {
		t.Parallel()
		mux, f := historyTestMux(t)
		rec := doHistory(t, mux, f.alice, "?limit=20&offset=40")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Fatalf("body = %s, want []", body)
		}
	})

	// The per-user rule at the HTTP boundary: the feed is whatever the session
	// says, and nothing else.
	t.Run("another user's session sees only their own watches", func(t *testing.T) {
		t.Parallel()
		mux, f := historyTestMux(t)
		entries := decodeHistory(t, doHistory(t, mux, f.bob, ""))
		if len(entries) != 1 {
			t.Fatalf("len = %d, want bob's single watch", len(entries))
		}
		if got := entries[0]["id"]; got == float64(f.w1) || got == float64(f.w4) {
			t.Errorf("id = %v, which is one of alice's watches", got)
		}
	})

	invalid := []struct {
		name  string
		query string
	}{
		{"invalid order", "?order=sideways"},
		{"invalid type", "?type=banana"},
		{"target_type without target_id", "?target_type=movie"},
		{"target_id without target_type", "?target_id=5"},
		{"invalid limit", "?limit=0"},
		{"invalid offset", "?offset=-1"},
		{"invalid season_id", "?season_id=0"},
		{"invalid show_id", "?show_id=0"},
		{"season_id combined with a target", "?season_id=1&target_type=movie&target_id=1"},
		{"show_id combined with season_id", "?show_id=1&season_id=1"},
	}
	for _, tt := range invalid {
		t.Run(tt.name+" returns 422 envelope", func(t *testing.T) {
			t.Parallel()
			mux, f := historyTestMux(t)
			rec := doHistory(t, mux, f.alice, tt.query)
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
