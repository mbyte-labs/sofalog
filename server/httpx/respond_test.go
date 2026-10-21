package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONLimit(t *testing.T) {
	t.Parallel()
	body := `{"name": "abc"}`
	var dst struct {
		Name string `json:"name"`
	}

	t.Run("a body at the limit decodes", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if err := DecodeJSONLimit(r, &dst, int64(len(body))); err != nil {
			t.Fatalf("DecodeJSONLimit: %v", err)
		}
	})

	t.Run("a body one byte over the limit is refused", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		if err := DecodeJSONLimit(r, &dst, int64(len(body))-1); err == nil {
			t.Fatal("DecodeJSONLimit accepted an oversize body")
		}
	})

	t.Run("DecodeJSON keeps the default cap", func(t *testing.T) {
		t.Parallel()
		big := `{"name": "` + strings.Repeat("x", maxBodyBytes) + `"}`
		r := httptest.NewRequest("POST", "/", strings.NewReader(big))
		if err := DecodeJSON(r, &dst); err == nil {
			t.Fatal("DecodeJSON accepted a body over 1 MiB")
		}
	})
}
