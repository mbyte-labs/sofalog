package videotest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"mbyte.ro/sofalog/httpx"
)

var ErrTimeout = errors.New("dial tcp: timeout")

func JSONNumber(v int64) string { return strconv.FormatInt(v, 10) }

func DoTracking(t *testing.T, mux *http.ServeMux, userID int64, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req = req.WithContext(httpx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// DoWatchlist issues the request with the user id in its context, exactly as the
// session middleware sets it.
func DoWatchlist(t *testing.T, mux *http.ServeMux, userID int64, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/video/watchlist"+query, nil)
	req = req.WithContext(httpx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func DecodeWatchlist(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("body is not a JSON array: %v", err)
	}
	return entries
}
