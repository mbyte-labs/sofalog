package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

// chainedMux assembles the middleware stack exactly as main.go does, with the
// auth routes registered plus a protected stub API route and a stub SPA
// handler at /.
func chainedMux(t *testing.T, database *db.DB) http.Handler {
	t.Helper()
	svc := NewService(database, nil, "UTC")
	mux := http.NewServeMux()
	NewHandler(svc, rootCookie()).RegisterRoutes(mux)
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/protected", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]int64{"user_id": httpx.UserIDFrom(r.Context())})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<!doctype html>"))
	})
	return httpx.Chain(mux,
		httpx.RequestID,
		httpx.RealIP(nil),
		httpx.Logging,
		httpx.Recover,
		Sessions(svc, rootCookie()),
	)
}

func get(h http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMiddlewareFailsClosed(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	h := chainedMux(t, database)

	// No cookie: every API route 401s...
	rec := get(h, "/api/v1/protected", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("protected without cookie: %d", rec.Code)
	}
	// ...including ones that do not exist — fail closed, don't leak routes.
	if rec := get(h, "/api/v1/does-not-exist", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown API path without cookie: %d", rec.Code)
	}

	// The public allowlist stays reachable.
	for _, path := range []string{"/api/v1/health", "/api/v1/auth/session"} {
		if rec := get(h, path, ""); rec.Code != http.StatusOK {
			t.Errorf("GET %s without cookie: %d", path, rec.Code)
		}
	}

	// Non-API paths always pass — the SPA must load to show the login page,
	// including on a deep-route refresh.
	for _, path := range []string{"/", "/video/history"} {
		if rec := get(h, path, ""); rec.Code != http.StatusOK {
			t.Errorf("GET %s without cookie: %d", path, rec.Code)
		}
	}
}

func TestMiddlewareStampsUser(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	token := mintSession(t, database, userID)
	h := chainedMux(t, database)

	rec := get(h, "/api/v1/protected", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("protected with session: %d, body %s", rec.Code, rec.Body)
	}
	body := decodeBody(t, rec)
	if int64(body["user_id"].(float64)) != userID {
		t.Errorf("handler saw user %v, want %d", body["user_id"], userID)
	}
}

func TestMiddlewareRejectsBadTokens(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	expired := mintSessionExpiring(t, database, userID, time.Now().UTC().Add(-time.Hour))
	h := chainedMux(t, database)

	for name, token := range map[string]string{"garbage": "nope", "expired": expired} {
		rec := get(h, "/api/v1/protected", token)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s token: %d", name, rec.Code)
		}
		// The dead cookie gets cleared so the browser stops sending it.
		cleared := false
		for _, c := range rec.Result().Cookies() {
			if c.Name == sessionCookieName && c.MaxAge < 0 {
				cleared = true
			}
		}
		if !cleared {
			t.Errorf("%s token: cookie not cleared", name)
		}
	}
}

func TestMiddlewareSlidesExpiry(t *testing.T) {
	t.Parallel()
	database := testDB(t)
	userID := seedUserWithPassword(t, database, "mihai@example.test", "hunter2hunter2", false)
	// Session past the refresh threshold: >refreshAfter of its TTL burnt.
	token := mintSessionExpiring(t, database, userID, time.Now().UTC().Add(sessionTTL-refreshAfter-time.Hour))
	h := chainedMux(t, database)

	rec := get(h, "/api/v1/protected", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	reissued := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value == token && c.MaxAge == int(sessionTTL.Seconds()) {
			reissued = true
		}
	}
	if !reissued {
		t.Error("no re-issued session cookie on a refresh-due request")
	}

	// A fresh session must NOT be re-issued on every request.
	fresh := mintSession(t, database, userID)
	rec = get(h, "/api/v1/protected", fresh)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			t.Errorf("fresh session got a Set-Cookie: %+v", c)
		}
	}
}
