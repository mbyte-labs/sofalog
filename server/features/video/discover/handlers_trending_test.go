package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

func doTrending(t *testing.T, mux *http.ServeMux, path, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/video/"+path+query, nil))
	return rec
}

// listRow is one row of a /trending or /discover response: TMDB names a
// movie's fields title/release_date and a show's name/first_air_date, and
// tags each with media_type on the trending lists.
func listRow(mediaType string, id int, title, date, poster string, popularity, vote float64, votes int) string {
	nameKey, dateKey := "title", "release_date"
	if mediaType == "tv" {
		nameKey, dateKey = "name", "first_air_date"
	}
	return fmt.Sprintf(`{"id":%d,"media_type":%q,%q:%q,%q:%q,"overview":"o","original_language":"en","genre_ids":[18],`+
		`"poster_path":%q,"popularity":%g,"vote_average":%g,"vote_count":%d}`,
		id, mediaType, nameKey, title, dateKey, date, poster, popularity, vote, votes)
}

func personRow(id int, name string) string {
	return fmt.Sprintf(`{"id":%d,"media_type":"person","name":%q,"popularity":99}`, id, name)
}

// tmdbPage wraps rows in TMDB's paging envelope.
func tmdbPage(page, totalPages int, rows ...string) string {
	return fmt.Sprintf(`{"page":%d,"total_pages":%d,"total_results":%d,"results":[%s]}`,
		page, totalPages, totalPages*20, strings.Join(rows, ","))
}

func decodePage(t *testing.T, rec *httptest.ResponseRecorder) videotest.PageBody {
	t.Helper()
	var out videotest.PageBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
	return out
}

func TestGetTrending(t *testing.T) {
	t.Parallel()

	t.Run("all keeps TMDB's cross-type order, drops people, nullables present and null", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/trending/all/day": tmdbPage(1, 1000,
				listRow("tv", 1399, "Game of Thrones", "2011-04-17", "/got.jpg", 500, 8.4, 20000),
				personRow(287, "Brad Pitt"),
				listRow("movie", 603, "The Matrix", "1999-03-30", "/m.jpg", 80, 8.2, 20000),
				listRow("movie", 9000, "Obscure", "", "", 1, 0, 0)),
		}}
		rec := doTrending(t, searchTestMux(t, stub), "trending", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		body := decodePage(t, rec)
		if body.Page != 1 || body.TotalPages != 1000 {
			t.Errorf("envelope = page %d of %d, want 1 of 1000", body.Page, body.TotalPages)
		}
		entries := body.Entries
		if len(entries) != 3 {
			t.Fatalf("got %d entries, want 3 (the person dropped): %s", len(entries), rec.Body)
		}
		// TMDB's order, not a popularity sort: the show led the list.
		if entries[0]["media_type"] != "show" || entries[0]["tmdb_id"] != float64(1399) || entries[0]["title"] != "Game of Thrones" {
			t.Errorf("first entry = %v, want the show TMDB ranked first", entries[0])
		}
		if entries[0]["year"] != "2011" || entries[0]["poster"] != "api/v1/video/posters/w185/got.jpg" ||
			entries[0]["poster_full"] != "api/v1/video/posters/w500/got.jpg" || entries[0]["rating"] != 8.4 {
			t.Errorf("show fields = %v", entries[0])
		}
		if entries[1]["media_type"] != "movie" || entries[1]["title"] != "The Matrix" || entries[1]["year"] != "1999" {
			t.Errorf("second entry = %v, want The Matrix", entries[1])
		}
		obscure := entries[2]
		for _, key := range []string{"year", "rating", "poster", "poster_full", "id"} {
			v, present := obscure[key]
			if !present {
				t.Errorf("%q absent — nullable fields must be present and null", key)
			} else if v != nil {
				t.Errorf("%q = %v, want null", key, v)
			}
		}
		for _, e := range entries {
			if e["id"] != nil || e["watched"] != false || e["in_history"] != false {
				t.Errorf("entry %v carries library marks with an empty catalog", e)
			}
		}
		if got := stub.LastQuery("/3/trending/all/day").Get("page"); got != "1" {
			t.Errorf("page sent = %q, want 1", got)
		}
	})

	t.Run("a single kind reads that kind's list and only it, in TMDB order, at the asked page", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/trending/movie/week": tmdbPage(3, 500,
				listRow("movie", 2, "Low", "2020-01-01", "", 1, 5, 10),
				listRow("movie", 1, "High", "2021-01-01", "", 100, 7, 10)),
		}}
		rec := doTrending(t, searchTestMux(t, stub), "trending", "?type=movie&window=week&page=3")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		body := decodePage(t, rec)
		if len(body.Entries) != 2 || body.Entries[0]["title"] != "Low" || body.Entries[1]["title"] != "High" {
			t.Errorf("entries = %v, want TMDB's order kept (no popularity sort)", body.Entries)
		}
		if body.Page != 3 || body.TotalPages != 500 {
			t.Errorf("envelope = page %d of %d, want 3 of 500", body.Page, body.TotalPages)
		}
		if got := stub.LastQuery("/3/trending/movie/week").Get("page"); got != "3" {
			t.Errorf("page sent = %q, want 3", got)
		}
		if n := stub.CallCount("/3/trending/all/week"); n != 0 {
			t.Errorf("the mixed list was fetched %d times for a movie-only request", n)
		}

		// Shows translate to TMDB's "tv" and come back as the app's "show".
		stub = &videotest.ProviderStub{Bodies: map[string]string{
			"/3/trending/tv/day": tmdbPage(1, 1, listRow("tv", 1399, "Game of Thrones", "2011-04-17", "", 5, 8, 10)),
		}}
		rec = doTrending(t, searchTestMux(t, stub), "trending", "?type=show")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		if body := decodePage(t, rec); len(body.Entries) != 1 || body.Entries[0]["media_type"] != "show" {
			t.Errorf("entries = %v, want one show", body.Entries)
		}
	})

	t.Run("a bad window, type or page is a 422 before any call", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		mux := searchTestMux(t, stub)
		for _, q := range []string{"?window=month", "?type=person", "?window=DAY", "?page=0", "?page=501", "?page=two"} {
			rec := doTrending(t, mux, "trending", q)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%q: status = %d, want 422: %s", q, rec.Code, rec.Body)
				continue
			}
			if code := videotest.ErrorCode(t, rec); code != "validation" {
				t.Errorf("%q: code = %q, want validation", q, code)
			}
		}
		for path := range stub.Calls {
			if strings.HasPrefix(path, "/3/trending") {
				t.Errorf("TMDB was called at %s for an invalid request", path)
			}
		}
	})

	t.Run("TMDB down is a 503 on every view — the list is live and has no local half", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/trending/all/day": errors.New("dial tcp: connection refused")}}
		rec := doTrending(t, searchTestMux(t, stub), "trending", "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "provider_unavailable" {
			t.Errorf("code = %q, want provider_unavailable", code)
		}
	})
}

func TestGetDiscover(t *testing.T) {
	t.Parallel()

	t.Run("defaults are the popular list: popularity order, adult off, nothing else sent", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/discover/movie": tmdbPage(1, 500, listRow("movie", 1, "M", "2020-01-01", "", 50, 7, 100)),
		}}
		rec := doTrending(t, searchTestMux(t, stub), "discover", "?type=movie")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		q := stub.LastQuery("/3/discover/movie")
		if q.Get("sort_by") != "popularity.desc" || q.Get("include_adult") != "false" || q.Get("page") != "1" {
			t.Errorf("query = %v", q)
		}
		for _, k := range []string{"vote_average.gte", "vote_average.lte", "vote_count.gte", "with_genres",
			"with_original_language", "primary_release_date.gte", "primary_release_date.lte",
			"with_watch_providers", "watch_region"} {
			if q.Has(k) {
				t.Errorf("%s sent without a filter: %q", k, q.Get(k))
			}
		}
		if n := stub.CallCount("/3/discover/tv"); n != 0 {
			t.Errorf("shows fetched %d times for a movies-only request", n)
		}
	})

	t.Run("every filter reaches TMDB under its per-kind name", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/discover/movie": tmdbPage(2, 9),
			"/3/discover/tv":    tmdbPage(2, 4),
		}}
		rec := doTrending(t, searchTestMux(t, stub), "discover",
			"?page=2&vote_min=7.5&vote_max=9&votes_min=1000&language=en&year_from=2015&year_to=2020&genre=18&genre=80&provider=8&provider=337")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		body := decodePage(t, rec)
		if body.Page != 2 || body.TotalPages != 9 {
			t.Errorf("envelope = page %d of %d, want 2 of 9 (the larger of the two kinds)", body.Page, body.TotalPages)
		}
		movie := stub.LastQuery("/3/discover/movie")
		tv := stub.LastQuery("/3/discover/tv")
		want := map[string]string{
			"page": "2", "vote_average.gte": "7.5", "vote_average.lte": "9", "vote_count.gte": "1000",
			"with_original_language": "en", "with_genres": "18,80", // comma: EVERY genre
			"with_watch_providers": "8|337", // pipe: ANY provider
			"watch_region":         videotest.WatchRegion,
		}
		for k, v := range want {
			if movie.Get(k) != v {
				t.Errorf("movie %s = %q, want %q", k, movie.Get(k), v)
			}
			if tv.Get(k) != v {
				t.Errorf("tv %s = %q, want %q", k, tv.Get(k), v)
			}
		}
		if movie.Get("primary_release_date.gte") != "2015-01-01" || movie.Get("primary_release_date.lte") != "2020-12-31" {
			t.Errorf("movie dates = %q..%q", movie.Get("primary_release_date.gte"), movie.Get("primary_release_date.lte"))
		}
		if tv.Get("first_air_date.gte") != "2015-01-01" || tv.Get("first_air_date.lte") != "2020-12-31" {
			t.Errorf("tv dates = %q..%q", tv.Get("first_air_date.gte"), tv.Get("first_air_date.lte"))
		}
		if tv.Get("include_null_first_air_dates") != "false" {
			t.Errorf("tv include_null_first_air_dates = %q", tv.Get("include_null_first_air_dates"))
		}
	})

	t.Run("highest rated implies a vote floor unless one was given; newest caps at today", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/discover/tv": tmdbPage(1, 1)}}
		mux := searchTestMux(t, stub)

		doTrending(t, mux, "discover", "?type=show&sort=rating")
		q := stub.LastQuery("/3/discover/tv")
		if q.Get("sort_by") != "vote_average.desc" || q.Get("vote_count.gte") != "200" {
			t.Errorf("rating query = %v, want vote_average.desc with vote_count.gte=200", q)
		}
		doTrending(t, mux, "discover", "?type=show&sort=rating&votes_min=50")
		if got := stub.LastQuery("/3/discover/tv").Get("vote_count.gte"); got != "50" {
			t.Errorf("explicit votes_min overridden: %q", got)
		}

		doTrending(t, mux, "discover", "?type=show&sort=newest")
		q = stub.LastQuery("/3/discover/tv")
		if q.Get("sort_by") != "first_air_date.desc" {
			t.Errorf("newest sort_by = %q", q.Get("sort_by"))
		}
		// The service's clock is the test harness's, not the real one; the
		// handler mux has no injected clock, so assert the shape and that a cap exists.
		if got := q.Get("first_air_date.lte"); len(got) != 10 {
			t.Errorf("newest sent no date cap: %q", got)
		}
		// A year_to earlier than today wins over the cap.
		doTrending(t, mux, "discover", "?type=show&sort=newest&year_to=2019")
		if got := stub.LastQuery("/3/discover/tv").Get("first_air_date.lte"); got != "2019-12-31" {
			t.Errorf("year_to lost to the newest cap: %q", got)
		}
		doTrending(t, mux, "discover", "?type=show&sort=votes")
		if got := stub.LastQuery("/3/discover/tv").Get("sort_by"); got != "vote_count.desc" {
			t.Errorf("votes sort_by = %q", got)
		}
	})

	t.Run("all merges both kinds by the sort's own key and never caps", func(t *testing.T) {
		t.Parallel()
		var movies, shows []string
		for i := 0; i < 20; i++ {
			// Interleaved ratings: movies take the even ranks, shows the odd.
			movies = append(movies, listRow("movie", 100+i, fmt.Sprintf("Movie %02d", i), "2020-01-01", "", 1, float64(100-2*i)/10, 500))
			shows = append(shows, listRow("tv", 200+i, fmt.Sprintf("Show %02d", i), "2020-01-01", "", 1000, float64(99-2*i)/10, 500))
		}
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/discover/movie": tmdbPage(1, 500, movies...),
			"/3/discover/tv":    tmdbPage(1, 500, shows...),
		}}
		rec := doTrending(t, searchTestMux(t, stub), "discover", "?sort=rating")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		body := decodePage(t, rec)
		if len(body.Entries) != 40 {
			t.Fatalf("got %d entries, want all 40 — a merged page is never capped", len(body.Entries))
		}
		for i, e := range body.Entries {
			wantKind := "movie"
			if i%2 == 1 {
				wantKind = "show"
			}
			if e["media_type"] != wantKind {
				t.Errorf("entry %d is a %v, want %s — the merge follows the rating, not popularity", i, e["media_type"], wantKind)
			}
		}
	})

	t.Run("bad filters are a 422 before any call", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		mux := searchTestMux(t, stub)
		for _, q := range []string{"?type=person", "?sort=random", "?page=0", "?vote_min=11", "?vote_max=abc",
			"?vote_min=8&vote_max=7", "?votes_min=-1", "?language=eng", "?language=EN", "?year_from=1800",
			"?year_from=2020&year_to=2019", "?genre=0", "?genre=18&genre=18", "?provider=x"} {
			rec := doTrending(t, mux, "discover", q)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%q: status = %d, want 422: %s", q, rec.Code, rec.Body)
				continue
			}
			if code := videotest.ErrorCode(t, rec); code != "validation" {
				t.Errorf("%q: code = %q, want validation", q, code)
			}
		}
		for path := range stub.Calls {
			if strings.HasPrefix(path, "/3/discover") {
				t.Errorf("TMDB was called at %s for an invalid request", path)
			}
		}
	})

	t.Run("TMDB down is a 503", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/discover/movie": errors.New("dial tcp: connection refused")}}
		rec := doTrending(t, searchTestMux(t, stub), "discover", "?type=movie")
		if rec.Code != http.StatusServiceUnavailable || videotest.ErrorCode(t, rec) != "provider_unavailable" {
			t.Errorf("status = %d, body %s, want a 503 provider_unavailable", rec.Code, rec.Body)
		}
	})
}

func TestGetDiscoverOptions(t *testing.T) {
	t.Parallel()

	t.Run("lists are projected, cleaned and cached for the process", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/genre/movie/list":        `{"genres":[{"id":28,"name":"Action"},{"id":18,"name":"Drama"}]}`,
			"/3/genre/tv/list":           `{"genres":[{"id":10759,"name":"Action & Adventure"},{"id":18,"name":"Drama"}]}`,
			"/3/configuration/languages": `[{"iso_639_1":"xx","english_name":"No Language","name":""},{"iso_639_1":"ro","english_name":"Romanian","name":"Română"},{"iso_639_1":"en","english_name":"English","name":"English"}]`,
			"/3/watch/providers/movie":   `{"results":[{"provider_id":8,"provider_name":"Netflix","logo_path":"/n.jpg","display_priority":1},{"provider_id":337,"provider_name":"Disney Plus","logo_path":"/d.jpg","display_priority":3}]}`,
			"/3/watch/providers/tv":      `{"results":[{"provider_id":8,"provider_name":"Netflix","logo_path":"/n.jpg","display_priority":1},{"provider_id":384,"provider_name":"HBO Max","logo_path":"","display_priority":2}]}`,
		}}
		mux := searchTestMux(t, stub)
		rec := doTrending(t, mux, "discover/options", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
		var got DiscoverOptions
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding: %v", err)
		}
		if got.Region != videotest.WatchRegion {
			t.Errorf("region = %q, want %q", got.Region, videotest.WatchRegion)
		}
		if len(got.Genres.Movie) != 2 || got.Genres.Movie[0].Name != "Action" || len(got.Genres.Show) != 2 || got.Genres.Show[0].ID != 10759 {
			t.Errorf("genres = %+v", got.Genres)
		}
		if len(got.Languages) != 2 || got.Languages[0].Code != "en" || got.Languages[1].Name != "Romanian" {
			t.Errorf("languages = %+v, want English then Romanian with xx dropped", got.Languages)
		}
		if len(got.Providers) != 3 || got.Providers[0].Name != "Netflix" || got.Providers[1].Name != "HBO Max" || got.Providers[2].Name != "Disney Plus" {
			t.Errorf("providers = %+v, want Netflix, HBO Max, Disney Plus (deduped, by display priority)", got.Providers)
		}
		if got.Providers[0].Logo == nil || *got.Providers[0].Logo != "api/v1/video/posters/w185/n.jpg" || got.Providers[1].Logo != nil {
			t.Errorf("logos = %v / %v", got.Providers[0].Logo, got.Providers[1].Logo)
		}
		if q := stub.LastQuery("/3/watch/providers/movie"); q.Get("watch_region") != videotest.WatchRegion {
			t.Errorf("providers fetched for region %q, want %q", q.Get("watch_region"), videotest.WatchRegion)
		}

		// A second view answers from the cache: every list fetched once.
		if rec := doTrending(t, mux, "discover/options", ""); rec.Code != http.StatusOK {
			t.Fatalf("second status = %d", rec.Code)
		}
		for _, path := range []string{"/3/genre/movie/list", "/3/genre/tv/list", "/3/configuration/languages", "/3/watch/providers/movie", "/3/watch/providers/tv"} {
			if n := stub.CallCount(path); n != 1 {
				t.Errorf("%s fetched %d times, want 1 (cached)", path, n)
			}
		}
	})

	t.Run("TMDB down is a 503 and nothing is cached", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/genre/movie/list": errors.New("dial tcp: connection refused")}}
		mux := searchTestMux(t, stub)
		rec := doTrending(t, mux, "discover/options", "")
		if rec.Code != http.StatusServiceUnavailable || videotest.ErrorCode(t, rec) != "provider_unavailable" {
			t.Errorf("status = %d, body %s, want a 503 provider_unavailable", rec.Code, rec.Body)
		}
		doTrending(t, mux, "discover/options", "")
		if n := stub.CallCount("/3/genre/movie/list"); n != 2 {
			t.Errorf("a failure was cached: %d calls, want 2", n)
		}
	})
}

// The marks are the person page's (D-027): a local id when the catalog holds
// the title, and THIS user's watched flags out of a local join — never a
// second provider call, never anyone else's state.
func TestTrendingLibraryMarks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	stub := &videotest.ProviderStub{Bodies: map[string]string{
		"/3/trending/all/day": tmdbPage(1, 1,
			listRow("movie", 550, "Fight Club", "1999-10-15", "", 90, 8.4, 100),
			listRow("tv", 1399, "Friends", "1994-09-22", "", 80, 8.5, 100),
			listRow("tv", 2001, "Untitled Project", "", "", 70, 0, 0),
			listRow("movie", 777, "alpha Day", "2005-06-01", "", 60, 6, 100),
			listRow("movie", 888, "Zebra Day", "2006-06-01", "", 50, 6, 100)),
	}}
	svc, _ := detailsService(t, stub)
	alice := videotest.SeedUser(t, svc.DB, "alice@example.com")
	bob := videotest.SeedUser(t, svc.DB, "bob@example.com")

	movie := videotest.SeedMovie(t, svc.DB, 550, "Fight Club", videotest.Ptr("1999-10-15"), nil, `{}`)
	videotest.SeedMovie(t, svc.DB, 777, "alpha Day", videotest.Ptr("2005-06-01"), nil, `{}`) // held, never watched
	friends := videotest.SeedShow(t, svc.DB, 1399, "Friends")
	untitled := videotest.SeedShow(t, svc.DB, 2001, "Untitled Project")
	videotest.SeedWatch(t, svc.DB, alice, "movie", movie.ID, "2026-01-01T20:00:00Z")
	videotest.SeedShowStatus(t, svc.DB, alice, friends, "watched")
	videotest.SeedWatch(t, svc.DB, alice, "episode", videotest.SeedEpisode(t, svc.DB, friends, 1, 1, videotest.Ptr("1994-09-22")), "2026-01-02T20:00:00Z")
	// Started, not finished: one episode watch and a watching state.
	videotest.SeedShowStatus(t, svc.DB, alice, untitled, "watching")
	videotest.SeedWatch(t, svc.DB, alice, "episode", videotest.SeedEpisode(t, svc.DB, untitled, 1, 1, nil), "2026-01-03T20:00:00Z")

	byTitle := func(page TrendingPage, title string) TrendingEntry {
		for _, e := range page.Entries {
			if e.Title == title {
				return e
			}
		}
		t.Fatalf("no entry titled %q in %+v", title, page.Entries)
		return TrendingEntry{}
	}

	a, err := svc.Trending(ctx, alice, TrendingParams{})
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	if e := byTitle(a, "Fight Club"); e.ID == nil || *e.ID != movie.ID || !e.Watched || !e.InHistory {
		t.Errorf("alice's Fight Club = %+v, want the local id, watched and in history", e)
	}
	if e := byTitle(a, "Friends"); e.ID == nil || *e.ID != friends || !e.Watched || !e.InHistory {
		t.Errorf("alice's Friends = %+v, want the local id, watched (completed) and in history", e)
	}
	// In the library and started is not watched: a show's ✓ means finished.
	if e := byTitle(a, "Untitled Project"); e.ID == nil || *e.ID != untitled || e.Watched || !e.InHistory {
		t.Errorf("alice's Untitled Project = %+v, want the local id, NOT watched, in history", e)
	}
	if e := byTitle(a, "alpha Day"); e.ID == nil || e.Watched || e.InHistory {
		t.Errorf("alice's alpha Day = %+v, want the local id and no watch flags", e)
	}
	if e := byTitle(a, "Zebra Day"); e.ID != nil || e.Watched || e.InHistory {
		t.Errorf("alice's Zebra Day = %+v, want no library flags", e)
	}

	// The catalog is shared, the flags are not.
	b, err := svc.Trending(ctx, bob, TrendingParams{})
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	if e := byTitle(b, "Fight Club"); e.ID == nil || *e.ID != movie.ID || e.Watched || e.InHistory {
		t.Errorf("bob's Fight Club = %+v, want the local id but NOT watched", e)
	}
	if e := byTitle(b, "Friends"); e.ID == nil || e.Watched || e.InHistory {
		t.Errorf("bob's Friends = %+v, want the local id but NOT watched", e)
	}

	// Two views, two calls, nothing materialised: the list is live.
	if n := stub.CallCount("/3/trending/all/day"); n != 2 {
		t.Errorf("TMDB called %d times, want 2 (live each time)", n)
	}
	if _, err := common.GetMovieByTMDBID(ctx, svc.DB, 888); !errors.Is(err, httpx.ErrNotFound) {
		t.Errorf("a trending view materialised a movies row: err = %v", err)
	}
}
