package lists

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// watchlistTestMux wires the routes over the seeded fixture. The service carries
// NO provider client at all, so a render path that reached TMDB would panic here
// rather than pass — the local-first rule, enforced by construction.
func watchlistTestMux(t *testing.T) (*http.ServeMux, watchlistFixture) {
	t.Helper()
	f := newWatchlistFixture(t)
	mux := http.NewServeMux()
	allHandlers(f.svc, videotest.Cookie).RegisterRoutes(mux)
	return mux, f
}

func TestGetWatchlist(t *testing.T) {
	t.Parallel()

	// The GET shares its path with the POST that adds to the list; the method
	// prefix is what separates them, so this also proves the routing.
	t.Run("returns the list most recently added first", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		rec := videotest.DoWatchlist(t, mux, f.alice, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want JSON", ct)
		}

		entries := videotest.DecodeWatchlist(t, rec)
		if len(entries) != 4 {
			t.Fatalf("len = %d, want 4", len(entries))
		}
		if got := entries[0]["added_at"]; got != "2026-08-15T21:05:00Z" {
			t.Errorf("first added_at = %v, want 2026-08-15T21:05:00Z (desc default)", got)
		}
	})

	// Every nullable key must be PRESENT and null — the SPA branches on null,
	// so a missing key is a contract break.
	t.Run("nullable fields serialize as explicit null", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		entries := videotest.DecodeWatchlist(t, videotest.DoWatchlist(t, mux, f.alice, ""))

		var bare map[string]any
		for _, e := range entries {
			// Both union keys are present on EVERY entry, and exactly one is
			// non-null — the unfilled arm is null, never absent.
			movie, hasMovie := e["movie"]
			show, hasShow := e["show"]
			if !hasMovie || !hasShow {
				t.Errorf("entry %v: a union key is absent, want both present", e)
				continue
			}
			if (movie == nil) == (show == nil) {
				t.Errorf("entry %v: movie = %v show = %v, want exactly one", e, movie, show)
			}
			if want := "movie"; (movie != nil) != (e["target_type"] == want) {
				t.Errorf("entry %v: the filled arm disagrees with target_type", e)
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
		movie, ok := bare["movie"].(map[string]any)
		if !ok {
			t.Fatalf("bare row: movie = %v, want an object", bare["movie"])
		}
		if v, present := movie["release_date"]; !present || v != nil {
			t.Errorf("bare row: movie.release_date = %v (present=%v), want explicit null", v, present)
		}
		// watched is a plain bool, so it is never null — but it must be there.
		if _, present := bare["watched"]; !present {
			t.Error("bare row: watched key absent")
		}
	})

	t.Run("type=movie excludes the show", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		entries := videotest.DecodeWatchlist(t, videotest.DoWatchlist(t, mux, f.alice, "?type=movie&order=asc"))
		if len(entries) != 3 {
			t.Fatalf("len = %d, want 3", len(entries))
		}
		for _, e := range entries {
			if got := e["target_type"]; got != "movie" {
				t.Errorf("target_type = %v, want movie", got)
			}
		}
	})

	t.Run("type=show returns the show arm, movie null", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		rec := videotest.DoWatchlist(t, mux, f.alice, "?type=show")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		entries := videotest.DecodeWatchlist(t, rec)
		if len(entries) != 1 {
			t.Fatalf("len = %d, want the one show", len(entries))
		}
		e := entries[0]
		if e["target_type"] != "show" || e["movie"] != nil {
			t.Fatalf("entry = %v, want the show arm and a null movie", e)
		}
		show, ok := e["show"].(map[string]any)
		if !ok {
			t.Fatalf("show = %v, want an object", e["show"])
		}
		for _, key := range []string{"id", "name", "first_air_date"} {
			if _, present := show[key]; !present {
				t.Errorf("show.%s absent, want it present", key)
			}
		}
		if show["name"] != "Mars Signal" || show["first_air_date"] != "1990-03-04" {
			t.Errorf("show = %v, want mars with its first air date", show)
		}
	})

	// An empty page for a documented value is still an array, never null — the
	// contract is a bare array whether anything matched or not.
	t.Run("a page past the end is an empty array, not null", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		rec := videotest.DoWatchlist(t, mux, f.alice, "?type=show&offset=5&limit=5")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
			t.Fatalf("body = %s, want []", got)
		}
	})

	t.Run("order_by=title sorts case-insensitively", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		entries := videotest.DecodeWatchlist(t, videotest.DoWatchlist(t, mux, f.alice, "?order_by=title&order=asc"))
		titles := make([]string, len(entries))
		for i, e := range entries {
			// A movie carries `title`, a show carries `name` — the two catalog
			// tables keep their own vocabulary, and one list holds both.
			if movie, ok := e["movie"].(map[string]any); ok {
				titles[i] = movie["title"].(string)
			} else {
				titles[i] = e["show"].(map[string]any)["name"].(string)
			}
		}
		want := []string{"alien electric", "Bright Wire", "Mars Signal", "Zero Hour"}
		for i := range want {
			if i >= len(titles) || titles[i] != want[i] {
				t.Fatalf("titles = %v, want %v", titles, want)
			}
		}
	})

	// The per-user rule at the HTTP boundary: the list is whatever the session
	// says, and nothing else.
	t.Run("another user's session sees only their own rows", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		entries := videotest.DecodeWatchlist(t, videotest.DoWatchlist(t, mux, f.bob, ""))
		if len(entries) != 1 {
			t.Fatalf("len = %d, want bob's single row", len(entries))
		}
		if got := entries[0]["movie"].(map[string]any)["id"]; got != float64(f.bright.ID) {
			t.Errorf("movie.id = %v, want bob's own row", got)
		}
	})

	invalid := []struct {
		name  string
		query string
	}{
		{"invalid order_by", "?order_by=popularity"},
		{"invalid order", "?order=sideways"},
		{"invalid type", "?type=banana"},
		{"invalid limit", "?limit=0"},
		{"invalid offset", "?offset=-1"},
		{"match_user not a number", "?match_user=bob"},
		{"match_user not sharing", "?match_user=%d"},
	}
	for _, tt := range invalid {
		t.Run(tt.name+" returns 422 envelope", func(t *testing.T) {
			t.Parallel()
			mux, f := watchlistTestMux(t)
			query := tt.query
			if strings.Contains(query, "%d") {
				query = fmt.Sprintf(query, f.dave)
			}
			rec := videotest.DoWatchlist(t, mux, f.alice, query)
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

func TestWatchlistMatchingHandlers(t *testing.T) {
	t.Parallel()

	// The parameter is REPEATED, and the handler hands every value through.
	t.Run("repeated match_user intersects all of them", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		rec := videotest.DoWatchlist(t, mux, f.alice, fmt.Sprintf("?match_user=%d&match_user=%d", f.bob, f.carol))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		entries := videotest.DecodeWatchlist(t, rec)
		if len(entries) != 1 || entries[0]["movie"].(map[string]any)["id"] != float64(f.bright.ID) {
			t.Fatalf("entries = %v, want bright alone", entries)
		}
	})

	t.Run("match-users lists id and label only", func(t *testing.T) {
		t.Parallel()
		mux, f := watchlistTestMux(t)
		req := httptest.NewRequest("GET", "/api/v1/video/watchlist/match-users", nil)
		req = req.WithContext(httpx.WithUserID(req.Context(), f.alice))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		users := videotest.DecodeWatchlist(t, rec)
		if len(users) != 2 {
			t.Fatalf("users = %v, want bob and carol", users)
		}
		for _, u := range users {
			if _, leaked := u["email"]; leaked || len(u) != 2 {
				t.Errorf("entry = %v, want exactly id and label", u)
			}
		}
		// The body is a bare array even when nobody shares.
		if strings.TrimSpace(rec.Body.String())[0] != '[' {
			t.Errorf("body = %s, want a bare array", rec.Body)
		}
	})
}
