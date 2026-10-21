package discover

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/provider"
)

// searchTestMux wires a real mux against a stubbed provider and a temp-dir
// cache — no network, no shared state between tests.
func searchTestMux(t *testing.T, stub *videotest.ProviderStub) *http.ServeMux {
	t.Helper()
	if stub.Bodies == nil {
		stub.Bodies = map[string]string{}
	}
	if _, ok := stub.Bodies["/3/configuration"]; !ok {
		stub.Bodies["/3/configuration"] = videotest.TMDBConfigBody
	}
	tmdb := provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"}, provider.WithHTTPDoer(stub))
	mux := http.NewServeMux()
	database, dataDir := videotest.DB(t), t.TempDir()
	cache, tree := assets.NewCache(dataDir), assets.NewTree(dataDir)
	allHandlers(NewService(common.NewCore(database, tmdb, cache, provider.NewYouTube(provider.WithYouTubeHTTPDoer(stub)),
		tree, assets.NewQueue(database, tree, cache, nil), videotest.WatchRegion, 0, 0, videotest.LinkRedirectURI,
		provider.NewJellyfin("", ""), 0)), videotest.Cookie).RegisterRoutes(mux)
	return mux
}

func doSearch(t *testing.T, mux *http.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/video/search"+query, nil))
	return rec
}

func movieRowJSON(id int, title, date, poster string, popularity, vote float64) string {
	return fmt.Sprintf(`{"id":%d,"title":%q,"release_date":%q,"overview":"o",`+
		`"poster_path":%q,"popularity":%g,"vote_average":%g}`, id, title, date, poster, popularity, vote)
}

func tvRow(id int, name, date, poster string, popularity, vote float64) string {
	return fmt.Sprintf(`{"id":%d,"name":%q,"first_air_date":%q,"overview":"o",`+
		`"poster_path":%q,"popularity":%g,"vote_average":%g}`, id, name, date, poster, popularity, vote)
}

func TestSearchVideo(t *testing.T) {
	t.Parallel()

	t.Run("maps movie fields, nullables present and null", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			// One full row and one with no date, no poster and a 0 vote — the
			// unrated/undated/artless case must serialize as explicit nulls.
			"/3/search/movie": `{"results":[` +
				movieRowJSON(603, "The Matrix", "1999-03-30", "/m.jpg", 80, 8.2) + "," +
				movieRowJSON(9000, "Obscure", "", "", 1, 0) + `]}`,
		}}
		rec := doSearch(t, searchTestMux(t, stub), "?q=matrix&type=movie")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}

		var results []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
			t.Fatalf("body is not a JSON array: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("len = %d, want 2", len(results))
		}

		full := results[0]
		if full["media_type"] != "movie" || full["tmdb_id"] != float64(603) || full["title"] != "The Matrix" {
			t.Errorf("first result = %v", full)
		}
		if full["year"] != "1999" { // the date's first four digits
			t.Errorf("year = %v, want 1999", full["year"])
		}
		if full["poster"] != "api/v1/video/posters/w185/m.jpg" {
			t.Errorf("poster = %v, want the local proxy URL", full["poster"])
		}
		if full["poster_full"] != "api/v1/video/posters/w500/m.jpg" {
			t.Errorf("poster_full = %v, want the local proxy URL", full["poster_full"])
		}
		if full["rating"] != 8.2 {
			t.Errorf("rating = %v", full["rating"])
		}

		bare := results[1]
		for _, key := range []string{"year", "poster", "poster_full", "rating"} {
			v, present := bare[key]
			if !present {
				t.Errorf("%s key absent, want explicit null", key)
			} else if v != nil {
				t.Errorf("%s = %v, want null", key, v)
			}
		}
		// A movie-only search must not touch the tv endpoint.
		if n := stub.CallCount("/3/search/tv"); n != 0 {
			t.Errorf("tv endpoint called %d times on a movie search", n)
		}
	})

	t.Run("all merges by popularity desc and caps at 20", func(t *testing.T) {
		t.Parallel()
		// 15 movies with even popularity, 15 shows with odd — a strict
		// interleave, so order proves the merge rather than concatenation.
		var movies, shows []string
		for i := range 15 {
			movies = append(movies, movieRowJSON(100+i, fmt.Sprintf("M%02d", i), "2000-01-01", "", float64(2*i), 5))
			shows = append(shows, tvRow(200+i, fmt.Sprintf("S%02d", i), "2000-01-01", "", float64(2*i+1), 5))
		}
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/search/movie": `{"results":[` + strings.Join(movies, ",") + `]}`,
			"/3/search/tv":    `{"results":[` + strings.Join(shows, ",") + `]}`,
		}}
		rec := doSearch(t, searchTestMux(t, stub), "?q=x")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}

		var results []struct {
			MediaType string `json:"media_type"`
			Title     string `json:"title"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
			t.Fatalf("body is not a JSON array: %v", err)
		}
		if len(results) != 20 {
			t.Fatalf("len = %d, want the cap of 20", len(results))
		}
		// Highest popularity is show S14 (29), then movie M14 (28), then S13…
		want := []string{"S14", "M14", "S13", "M13"}
		for i, w := range want {
			if results[i].Title != w {
				t.Errorf("results[%d] = %s, want %s (popularity desc)", i, results[i].Title, w)
			}
		}
	})

	t.Run("no matches is an empty array, not null", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/search/movie": `{"results":[]}`,
			"/3/search/tv":    `{"results":[]}`,
		}}
		rec := doSearch(t, searchTestMux(t, stub), "?q=zzz")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
			t.Errorf("body = %s, want []", body)
		}
	})

	invalid := []struct {
		name  string
		query string
	}{
		{"missing q", ""},
		{"blank q", "?q=%20%20"},
		{"invalid type", "?q=x&type=banana"},
	}
	for _, tt := range invalid {
		t.Run(tt.name+" returns 422 envelope", func(t *testing.T) {
			t.Parallel()
			rec := doSearch(t, searchTestMux(t, &videotest.ProviderStub{}), tt.query)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
			}
			if code := videotest.ErrorCode(t, rec); code != "validation" {
				t.Errorf("code = %q, want validation", code)
			}
		})
	}

	t.Run("a provider failure is a 503 that never leaks the key", func(t *testing.T) {
		t.Parallel()
		// The fake transport error carries the key the way net/http would —
		// in the request URL. (A neutral host keeps the provider-hostname
		// invariant grep quiet; only the api_key parameter matters here.)
		stub := &videotest.ProviderStub{Errs: map[string]error{
			"/3/search/movie": fmt.Errorf(`Get "https://provider.example/3/search/movie?api_key=test-key": dial tcp: timeout`),
		}}
		rec := doSearch(t, searchTestMux(t, stub), "?q=x&type=movie")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "provider_unavailable" {
			t.Errorf("code = %q, want provider_unavailable", code)
		}
		if strings.Contains(rec.Body.String(), "test-key") {
			t.Fatalf("the API key leaked into the response: %s", rec.Body)
		}
	})
}
