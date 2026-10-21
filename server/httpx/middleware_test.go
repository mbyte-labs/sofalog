package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func prefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()
	var out []netip.Prefix
	for _, c := range cidrs {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}

func TestRealIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		trusted []string
		remote  string
		xff     string
		want    string
	}{
		{
			name:   "no trusted proxies ignores XFF entirely",
			remote: "203.0.113.7:1234",
			xff:    "8.8.8.8",
			want:   "203.0.113.7",
		},
		{
			name:    "untrusted peer ignores XFF",
			trusted: []string{"10.0.0.0/8"},
			remote:  "203.0.113.7:1234",
			xff:     "8.8.8.8",
			want:    "203.0.113.7",
		},
		{
			name:    "trusted peer takes right-most non-proxy hop",
			trusted: []string{"10.0.0.0/8"},
			remote:  "10.0.0.2:1234",
			xff:     "8.8.8.8, 198.51.100.4",
			want:    "198.51.100.4",
		},
		{
			name:    "spoofed left entry behind one proxy is not used",
			trusted: []string{"10.0.0.0/8"},
			remote:  "10.0.0.2:1234",
			xff:     "1.2.3.4, 10.0.0.3, 198.51.100.4",
			want:    "198.51.100.4",
		},
		{
			name:    "chained trusted proxies skip to real client",
			trusted: []string{"10.0.0.0/8"},
			remote:  "10.0.0.2:1234",
			xff:     "198.51.100.4, 10.0.0.3",
			want:    "198.51.100.4",
		},
		{
			name:    "trusted peer without XFF uses peer",
			trusted: []string{"10.0.0.0/8"},
			remote:  "10.0.0.2:1234",
			want:    "10.0.0.2",
		},
		{
			name:    "malformed hop falls back to peer",
			trusted: []string{"10.0.0.0/8"},
			remote:  "10.0.0.2:1234",
			xff:     "8.8.8.8, garbage",
			want:    "10.0.0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got netip.Addr
			h := RealIP(prefixes(t, tt.trusted...))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = ClientIPFrom(r.Context())
			}))
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			h.ServeHTTP(httptest.NewRecorder(), r)
			if got.String() != tt.want {
				t.Errorf("client IP = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecoverWritesEnvelope(t *testing.T) {
	t.Parallel()
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the JSON envelope: %v (%s)", err, rec.Body)
	}
	if body.Error.Code != "internal" || body.Error.Message != "internal server error" {
		t.Errorf("envelope = %+v; the panic value must not leak", body)
	}
}

func TestRequestIDEchoed(t *testing.T) {
	t.Parallel()
	var inCtx string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inCtx = RequestIDFrom(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	header := rec.Header().Get("X-Request-Id")
	if header == "" || header != inCtx {
		t.Errorf("header id %q != context id %q", header, inCtx)
	}
}
