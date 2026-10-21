package people

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func doPersonDetails(t *testing.T, mux *http.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/video/people/details"+query, nil))
	return rec
}

func TestGetPersonDetails(t *testing.T) {
	t.Parallel()

	t.Run("200 with every nullable field present and every array an array", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			// A minimal person: everything nullable must serialize as an
			// explicit null, arrays as [].
			"/3/person/5": `{"id": 5, "name": "Bare"}`,
		}}
		rec := doPersonDetails(t, searchTestMux(t, stub), "?tmdb_id=5")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if body["name"] != "Bare" || body["tmdb_id"] != float64(5) {
			t.Errorf("body = %v", body)
		}
		// A person never has a local id: there is no such key at all.
		if _, present := body["id"]; present {
			t.Errorf("id key present on a person, want none (a person is never a row)")
		}
		for _, key := range []string{
			"biography", "known_for_department", "birthday", "deathday",
			"place_of_birth", "homepage", "imdb_id", "photo", "photo_full",
		} {
			v, present := body[key]
			if !present {
				t.Errorf("%s key absent, want explicit null", key)
			} else if v != nil {
				t.Errorf("%s = %v, want null", key, v)
			}
		}
		for _, key := range []string{"also_known_as", "credits"} {
			v, ok := body[key].([]any)
			if !ok || len(v) != 0 {
				t.Errorf("%s = %v, want an empty array", key, body[key])
			}
		}
	})

	t.Run("a credit carries explicit nulls and both library fields", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/person/287": pittBody}}
		rec := doPersonDetails(t, searchTestMux(t, stub), "?tmdb_id=287")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var body struct {
			Credits []map[string]any `json:"credits"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if len(body.Credits) == 0 {
			t.Fatal("no credits")
		}
		undated := body.Credits[0]
		for _, key := range []string{"year", "character", "rating", "poster", "poster_full", "episodes", "id"} {
			v, present := undated[key]
			if !present {
				t.Errorf("%s key absent, want explicit null", key)
			} else if v != nil {
				t.Errorf("%s = %v, want null", key, v)
			}
		}
		for _, key := range []string{"watched", "in_history"} {
			if w, ok := undated[key].(bool); !ok || w {
				t.Errorf("%s = %v, want false", key, undated[key])
			}
		}
	})

	t.Run("a bad id is a 422", func(t *testing.T) {
		t.Parallel()
		mux := searchTestMux(t, &videotest.ProviderStub{})
		for _, q := range []string{"", "?tmdb_id=abc", "?tmdb_id=0"} {
			rec := doPersonDetails(t, mux, q)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%q: status = %d, want 422: %s", q, rec.Code, rec.Body)
				continue
			}
			if code := videotest.ErrorCode(t, rec); code != "validation" {
				t.Errorf("%q: code = %q, want validation", q, code)
			}
		}
	})

	t.Run("an unknown person is a 404", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Statuses: map[string]int{"/3/person/1": 404}}
		rec := doPersonDetails(t, searchTestMux(t, stub), "?tmdb_id=1")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "not_found" {
			t.Errorf("code = %q, want not_found", code)
		}
	})

	t.Run("TMDB down is a 503 — the one read route that always can", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/person/287": errors.New("dial tcp: connection refused")}}
		rec := doPersonDetails(t, searchTestMux(t, stub), "?tmdb_id=287")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "provider_unavailable" {
			t.Errorf("code = %q, want provider_unavailable", code)
		}
	})
}
