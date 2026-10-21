package tmdbaccount

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// The link's four routes (D-029). Two are browser navigation and must answer
// with redirects, never an error envelope. The handler never names the TMDB
// host — the approval URL is the provider's — so these assertions look for
// the path and the redirect_to, not the host.

func tmdbLinkMux(t *testing.T, stub *videotest.ProviderStub) (*http.ServeMux, *Service, int64) {
	t.Helper()
	svc, userID, _ := syncService(t, stub)
	mux := http.NewServeMux()
	allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)
	return mux, svc, userID
}

func doLink(t *testing.T, mux *http.ServeMux, userID int64, method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(httpx.WithUserID(req.Context(), userID))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func linkStatus(t *testing.T, mux *http.ServeMux, userID int64) TMDBLinkStatus {
	t.Helper()
	rec := doLink(t, mux, userID, "GET", "/api/v1/video/tmdb/link")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var st TMDBLinkStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("body: %v", err)
	}
	return st
}

// flowCookie extracts the link cookie a start response set.
func flowCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == tmdbLinkCookieName && c.MaxAge > 0 {
			if !c.HttpOnly {
				t.Error("flow cookie is not HttpOnly")
			}
			return c
		}
	}
	t.Fatal("no flow cookie set")
	return nil
}

func TestTMDBLinkHandlers(t *testing.T) {
	t.Parallel()

	t.Run("status has one shape, linked or not", func(t *testing.T) {
		t.Parallel()
		mux, _, userID := tmdbLinkMux(t, &videotest.ProviderStub{})
		rec := doLink(t, mux, userID, "GET", "/api/v1/video/tmdb/link")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"configured", "linked", "broken", "username", "linked_at", "pending", "setup_done", "sync", "pull"} {
			if _, ok := raw[key]; !ok {
				t.Errorf("key %q missing from %s", key, rec.Body)
			}
		}
		if raw["linked"] != false || raw["username"] != nil || raw["configured"] != true {
			t.Errorf("body = %s", rec.Body)
		}
	})

	t.Run("start redirects to the approval page and pins the flow", func(t *testing.T) {
		t.Parallel()
		mux, _, userID := tmdbLinkMux(t, &videotest.ProviderStub{})
		rec := doLink(t, mux, userID, "GET", "/api/v1/video/tmdb/link/start")
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "/authenticate/tok-1?redirect_to=") || !strings.Contains(loc, "%2Fvideo%2Ftmdb%2Flink%2Fcallback") {
			t.Errorf("Location = %q", loc)
		}
		c := flowCookie(t, rec)
		if c.Path != "/" || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("cookie = %+v", c)
		}
	})

	t.Run("start with tmdb down lands on the result page", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/authentication/token/new": videotest.ErrTimeout}}
		mux, _, userID := tmdbLinkMux(t, stub)
		rec := doLink(t, mux, userID, "GET", "/api/v1/video/tmdb/link/start")
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/settings/tmdb?tmdb_link=provider_unavailable" {
			t.Errorf("status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("the full round trip links, clears the cookie and then unlinks", func(t *testing.T) {
		t.Parallel()
		mux, _, userID := tmdbLinkMux(t, &videotest.ProviderStub{})
		start := doLink(t, mux, userID, "GET", "/api/v1/video/tmdb/link/start")
		c := flowCookie(t, start)

		rec := doLink(t, mux, userID, "GET", "/api/v1/video/tmdb/link/callback?request_token=tok-1&approved=true", c)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/settings/tmdb?tmdb_link=ok" {
			t.Fatalf("status = %d, Location = %q: %s", rec.Code, rec.Header().Get("Location"), rec.Body)
		}
		cleared := false
		for _, sc := range rec.Result().Cookies() {
			if sc.Name == tmdbLinkCookieName && sc.MaxAge < 0 {
				cleared = true
			}
		}
		if !cleared {
			t.Error("flow cookie was not cleared by the callback")
		}
		st := linkStatus(t, mux, userID)
		if !st.Linked || st.Username == nil || *st.Username != "bob" {
			t.Errorf("status = %+v", st)
		}

		del := doLink(t, mux, userID, "DELETE", "/api/v1/video/tmdb/link")
		if del.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d: %s", del.Code, del.Body)
		}
		if st := linkStatus(t, mux, userID); st.Linked {
			t.Errorf("still linked after DELETE: %+v", st)
		}
	})

	t.Run("callback failures redirect with a code", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name   string
			query  string
			cookie bool
			other  bool // the flow belongs to another user
			want   string
		}{
			{name: "no cookie", query: "request_token=tok-1&approved=true", want: "expired"},
			{name: "denied", query: "request_token=tok-1&denied=true", cookie: true, want: "denied"},
			{name: "token mismatch", query: "request_token=forged&approved=true", cookie: true, want: "expired"},
			{name: "another user's flow", query: "request_token=tok-1&approved=true", cookie: true, other: true, want: "expired"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				mux, svc, userID := tmdbLinkMux(t, &videotest.ProviderStub{})
				var cookies []*http.Cookie
				if tt.cookie {
					starter := userID
					if tt.other {
						starter = videotest.SeedUser(t, svc.DB, "carol")
					}
					cookies = append(cookies, flowCookie(t, doLink(t, mux, starter, "GET", "/api/v1/video/tmdb/link/start")))
				}
				rec := doLink(t, mux, userID, "GET", "/api/v1/video/tmdb/link/callback?"+tt.query, cookies...)
				if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/settings/tmdb?tmdb_link="+tt.want {
					t.Errorf("status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
				}
				if st := linkStatus(t, mux, userID); st.Linked {
					t.Error("linked after a failed callback")
				}
			})
		}
	})

	t.Run("the result page honours the base path", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		mux := http.NewServeMux()
		allHandlers(svc, common.CookieSettings{Path: "/media/"}).RegisterRoutes(mux)
		rec := doLink(t, mux, userID, "GET", "/api/v1/video/tmdb/link/callback?request_token=x&approved=true")
		if rec.Header().Get("Location") != "/media/settings/tmdb?tmdb_link=expired" {
			t.Errorf("Location = %q", rec.Header().Get("Location"))
		}
	})
}

// doLinkJSON is doLink with a JSON body.
func doLinkJSON(t *testing.T, mux *http.ServeMux, userID int64, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(httpx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestTMDBSetupHandlers(t *testing.T) {
	t.Parallel()
	const skipAll = `{"ratings":{"action":"skip"},"watchlist":{"action":"skip"},"favorites":{"action":"skip"}}`

	t.Run("setup answers 202 with the status", func(t *testing.T) {
		t.Parallel()
		mux, svc, userID := tmdbLinkMux(t, &videotest.ProviderStub{})
		linkOnly(t, svc, userID)
		rec := doLinkJSON(t, mux, userID, "POST", "/api/v1/video/tmdb/link/setup", skipAll)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		var st TMDBLinkStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		if !st.SetupDone || st.Pull != nil {
			t.Errorf("body = %s", rec.Body)
		}
	})

	t.Run("setup refusals", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			link bool
			body string
			want int
		}{
			{name: "unlinked is 404", body: skipAll, want: http.StatusNotFound},
			{name: "malformed is 400", link: true, body: `{"ratings":`, want: http.StatusBadRequest},
			{name: "unknown action is 422", link: true, body: `{"ratings":{"action":"merge"},"watchlist":{"action":"skip"},"favorites":{"action":"skip"}}`, want: http.StatusUnprocessableEntity},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				mux, svc, userID := tmdbLinkMux(t, &videotest.ProviderStub{})
				if tt.link {
					linkOnly(t, svc, userID)
				}
				rec := doLinkJSON(t, mux, userID, "POST", "/api/v1/video/tmdb/link/setup", tt.body)
				if rec.Code != tt.want {
					t.Errorf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
				}
			})
		}
	})

	t.Run("sync switches answer 200 with the status, 404 unlinked", func(t *testing.T) {
		t.Parallel()
		mux, svc, userID := tmdbLinkMux(t, &videotest.ProviderStub{})
		if rec := doLinkJSON(t, mux, userID, "PUT", "/api/v1/video/tmdb/link/sync", `{"ratings":true}`); rec.Code != http.StatusNotFound {
			t.Errorf("unlinked status = %d", rec.Code)
		}
		linkOnly(t, svc, userID)
		rec := doLinkJSON(t, mux, userID, "PUT", "/api/v1/video/tmdb/link/sync", `{"ratings":true,"watchlist":false,"favorites":true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		var st TMDBLinkStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		if st.Sync != (TMDBSyncKinds{Ratings: true, Favorites: true}) {
			t.Errorf("sync = %+v", st.Sync)
		}
		if rec := doLinkJSON(t, mux, userID, "PUT", "/api/v1/video/tmdb/link/sync", `{"ratings":"yes"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("malformed status = %d", rec.Code)
		}
	})
}
