package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

func testMux(t *testing.T, database *db.DB, cookie CookieConfig) *http.ServeMux {
	t.Helper()
	svc := NewService(database, nil, "UTC")
	svc.now = func() time.Time { return testNow }
	mux := http.NewServeMux()
	NewHandler(svc, cookie).RegisterRoutes(mux)
	return mux
}

func rootCookie() CookieConfig { return CookieConfig{Path: "/", Secure: false} }

// sessionCookie extracts the sofalog_session cookie from a recorded response.
func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatal("no session cookie in response")
	return nil
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	return body
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := decodeBody(t, rec)
	envelope, _ := body["error"].(map[string]any)
	code, _ := envelope["code"].(string)
	return code
}

func TestSetupHandler(t *testing.T) {
	t.Parallel()
	mux := testMux(t, testDB(t), rootCookie())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/setup",
		strings.NewReader(`{"email":"mihai@example.test","password":"hunter2hunter2"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Secure {
		t.Errorf("cookie attributes: %+v", c)
	}
	if c.MaxAge != int(sessionTTL.Seconds()) {
		t.Errorf("cookie Max-Age = %d, want %d", c.MaxAge, int(sessionTTL.Seconds()))
	}
	body := decodeBody(t, rec)
	user, _ := body["user"].(map[string]any)
	if user["email"] != "mihai@example.test" || user["is_admin"] != true {
		t.Errorf("body = %v", body)
	}

	// Second setup: 409.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/setup",
		strings.NewReader(`{"email":"other@example.test","password":"hunter2hunter2"}`)))
	if rec.Code != http.StatusConflict || errorCode(t, rec) != "conflict" {
		t.Errorf("second setup: status %d, code %q", rec.Code, errorCode(t, rec))
	}
}

func TestSetupHandlerValidation(t *testing.T) {
	t.Parallel()
	mux := testMux(t, testDB(t), rootCookie())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/setup",
		strings.NewReader(`{"email":"mihai@example.test","password":"short"}`)))
	if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "validation" {
		t.Errorf("status %d, code %q, want 422 validation", rec.Code, errorCode(t, rec))
	}
}

// Behind a sub-path reverse proxy the cookie must be scoped to the sub-path.
func TestCookiePathFollowsBasePath(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	mux := testMux(t, database, CookieConfig{Path: "/media/", Secure: true})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/login",
		strings.NewReader(`{"email":"mihai@example.test","password":"hunter2hunter2"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	c := sessionCookie(t, rec)
	if c.Path != "/media/" || !c.Secure {
		t.Errorf("cookie Path=%q Secure=%v, want /media/ true", c.Path, c.Secure)
	}
}

func TestLoginHandler(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	mux := testMux(t, database, rootCookie())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/login",
		strings.NewReader(`{"email":"mihai@example.test","password":"hunter2hunter2"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if c := sessionCookie(t, rec); c.Value == "" {
		t.Error("empty session cookie")
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/login",
		strings.NewReader(`{"email":"mihai@example.test","password":"wrong password"}`)))
	if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "unauthorized" {
		t.Errorf("bad login: status %d, code %q, want 401 unauthorized", rec.Code, errorCode(t, rec))
	}
}

func TestLogoutHandler(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	token := mintSession(t, database, userID)
	mux := testMux(t, database, rootCookie())

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if c := sessionCookie(t, rec); c.MaxAge >= 0 || c.Value != "" {
		t.Errorf("logout did not expire the cookie: %+v", c)
	}

	// Idempotent: no cookie at all still 204s.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/auth/logout", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("cookieless logout status = %d", rec.Code)
	}
}

func TestSessionProbeHandler(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	mux := testMux(t, database, rootCookie())

	probe := func(token string) (int, map[string]any) {
		req := httptest.NewRequest("GET", "/api/v1/auth/session", nil)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, decodeBody(t, rec)
	}

	code, body := probe("")
	if code != 200 || body["status"] != "setup_required" {
		t.Errorf("empty instance: %d %v", code, body)
	}

	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", true)
	code, body = probe("")
	if code != 200 || body["status"] != "unauthenticated" {
		t.Errorf("no cookie: %d %v", code, body)
	}

	code, body = probe(mintSession(t, database, userID))
	user, _ := body["user"].(map[string]any)
	if code != 200 || body["status"] != "authenticated" || user["email"] != "mihai@example.test" {
		t.Errorf("with session: %d %v", code, body)
	}
	// The D-028 consent flag rides the probe, default off — and is a key the
	// SPA can bind to, not an absent one.
	if v, ok := user["match_watchlist"]; !ok || v != false {
		t.Errorf("match_watchlist = %v (present %v), want false", v, ok)
	}
}

func TestMatchWatchlistHandler(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	mux := testMux(t, database, rootCookie())
	memberID := seedUserWithPassword(t, database, "member@example.test", "hunter2hunter2", false)
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)

	do := func(actorID, targetID int64, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/auth/users/%d/match-watchlist", targetID), strings.NewReader(body))
		req = req.WithContext(httpx.WithUserID(req.Context(), actorID))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(memberID, memberID, `{"match_watchlist": true}`); rec.Code != http.StatusNoContent {
		t.Fatalf("self opt-in status = %d, body %s", rec.Code, rec.Body)
	}
	if row, _ := getUserByID(context.Background(), database, memberID); !row.MatchWatchlist {
		t.Error("opt-in did not land")
	}
	if rec := do(adminID, memberID, `{"match_watchlist": false}`); rec.Code != http.StatusForbidden {
		t.Errorf("admin on another = %d, want 403", rec.Code)
	}
	if rec := do(memberID, memberID, `{"match_watchlist": "yes"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("non-bool body = %d, want 400", rec.Code)
	}
}

func TestCreateUserHandler(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "hunter2hunter2", false)
	mux := testMux(t, database, rootCookie())

	// The middleware (tested separately) stamps the user id; handler tests
	// inject it directly, like the video feature's do.
	post := func(actorID int64, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/auth/users", strings.NewReader(body))
		req = req.WithContext(httpx.WithUserID(req.Context(), actorID))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := post(adminID, `{"email":"kid@example.test","password":"hunter2hunter2"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create: status %d, body %s", rec.Code, rec.Body)
	}
	body := decodeBody(t, rec)
	user, _ := body["user"].(map[string]any)
	if user["email"] != "kid@example.test" || user["is_admin"] != false {
		t.Errorf("created user = %v", body)
	}

	rec = post(memberID, `{"email":"kid2@example.test","password":"hunter2hunter2"}`)
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != "forbidden" {
		t.Errorf("non-admin create: status %d, code %q", rec.Code, errorCode(t, rec))
	}

	rec = post(adminID, `{"email":"kid@example.test","password":"hunter2hunter2"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate create: status %d", rec.Code)
	}
}

func TestUserAdminHandlers(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	adminID := seedUserWithPassword(t, database, "admin@example.test", "hunter2hunter2", true)
	memberID := seedUserWithPassword(t, database, "member@example.test", "oldpassword", false)
	mux := testMux(t, database, rootCookie())

	// The middleware (tested separately) stamps the user id; handler tests
	// inject it directly, as the video feature's tests do.
	do := func(method, path string, actorID int64, body string) *httptest.ResponseRecorder {
		var r io.Reader
		if body != "" {
			r = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, r)
		req = req.WithContext(httpx.WithUserID(req.Context(), actorID))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	t.Run("list", func(t *testing.T) {
		rec := do("GET", "/api/v1/auth/users", adminID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("admin list: status %d, body %s", rec.Code, rec.Body)
		}
		body := decodeBody(t, rec)
		users, _ := body["users"].([]any)
		if len(users) != 2 {
			t.Errorf("got %d users, want 2: %v", len(users), body)
		}
		first, _ := users[0].(map[string]any)
		if first["created_at"] == nil || first["email"] == nil || first["display_name"] == nil {
			t.Errorf("list entry missing fields: %v", first)
		}
		if rec := do("GET", "/api/v1/auth/users", memberID, ""); rec.Code != http.StatusForbidden {
			t.Errorf("non-admin list: status %d, want 403", rec.Code)
		}
	})

	t.Run("password", func(t *testing.T) {
		path := "/api/v1/auth/users/" + strconv.FormatInt(memberID, 10) + "/password"

		rec := do("PUT", path, adminID, `{"new_password":"resetpassword"}`)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("admin reset: status %d, body %s", rec.Code, rec.Body)
		}

		// Wrong current password on a self-change: 422, never 401 — a 401
		// would make the SPA client log the user out for a typo.
		rec = do("PUT", path, memberID, `{"current_password":"nope","new_password":"anotherpassword"}`)
		if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "validation" {
			t.Errorf("wrong current password: status %d code %q, want 422 validation", rec.Code, errorCode(t, rec))
		}

		rec = do("PUT", path, memberID, `{"current_password":"resetpassword","new_password":"anotherpassword"}`)
		if rec.Code != http.StatusNoContent {
			t.Errorf("self change: status %d, body %s", rec.Code, rec.Body)
		}

		rec = do("PUT", "/api/v1/auth/users/9999/password", adminID, `{"new_password":"resetpassword"}`)
		if rec.Code != http.StatusNotFound {
			t.Errorf("unknown user: status %d, want 404", rec.Code)
		}

		rec = do("PUT", "/api/v1/auth/users/abc/password", adminID, `{"new_password":"resetpassword"}`)
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "invalid_id" {
			t.Errorf("garbage id: status %d code %q, want 400 invalid_id", rec.Code, errorCode(t, rec))
		}
	})

	t.Run("delete", func(t *testing.T) {
		self := "/api/v1/auth/users/" + strconv.FormatInt(adminID, 10)
		if rec := do("DELETE", self, adminID, ""); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("self-delete: status %d, want 422", rec.Code)
		}
		target := "/api/v1/auth/users/" + strconv.FormatInt(memberID, 10)
		if rec := do("DELETE", target, memberID, ""); rec.Code != http.StatusForbidden {
			t.Errorf("non-admin delete: status %d, want 403", rec.Code)
		}
		if rec := do("DELETE", target, adminID, ""); rec.Code != http.StatusNoContent {
			t.Errorf("admin delete: status %d", rec.Code)
		}
		if rec := do("GET", "/api/v1/auth/users", adminID, ""); len(decodeBody(t, rec)["users"].([]any)) != 1 {
			t.Error("deleted user still in the list")
		}
	})
}
