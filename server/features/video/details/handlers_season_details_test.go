package details

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

func doSeasonDetails(t *testing.T, mux *http.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/video/seasons/details"+query, nil))
	return rec
}

func TestGetSeasonDetails(t *testing.T) {
	t.Parallel()

	t.Run("bad requests", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			query string
		}{
			{"no params", ""},
			{"id with tmdb_id", "?id=1&tmdb_id=1396"},
			{"id with season", "?id=1&season=2"},
			{"tmdb_id without season", "?tmdb_id=1396"},
			{"season without tmdb_id", "?season=2"},
			{"non-numeric id", "?id=abc"},
			{"zero id", "?id=0"},
			{"non-numeric tmdb_id", "?tmdb_id=abc&season=2"},
			{"non-numeric season", "?tmdb_id=1396&season=two"},
			{"negative season", "?tmdb_id=1396&season=-1"},
		}
		for _, tc := range tests {
			t.Run(tc.name+" returns 422 envelope", func(t *testing.T) {
				t.Parallel()
				rec := doSeasonDetails(t, searchTestMux(t, &videotest.ProviderStub{}), tc.query)
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
				}
				if code := videotest.ErrorCode(t, rec); code != "validation" {
					t.Errorf("code = %q, want validation", code)
				}
			})
		}
	})

	t.Run("an id miss is a plain 404, never a TMDB call", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		rec := doSeasonDetails(t, searchTestMux(t, stub), "?id=99999")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
		if n := stub.CallCount("/3/tv/99999"); n != 0 {
			t.Errorf("an id miss reached TMDB %d times", n)
		}
	})

	t.Run("an unknown show is a 404 from the provider", func(t *testing.T) {
		t.Parallel()
		rec := doSeasonDetails(t, searchTestMux(t, &videotest.ProviderStub{}), "?tmdb_id=424242&season=1")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
	})

	t.Run("a season number the show does not have is a 404", func(t *testing.T) {
		t.Parallel()
		// The response is a valid show root without the requested "season/9" key.
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		rec := doSeasonDetails(t, searchTestMux(t, stub), "?tmdb_id=1396&season=9")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
	})

	t.Run("season zero (Specials) passes validation", func(t *testing.T) {
		t.Parallel()
		// No "season/0" in the body, so this 404s — the point is it is not a 422.
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		rec := doSeasonDetails(t, searchTestMux(t, stub), "?tmdb_id=1396&season=0")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (not 422): %s", rec.Code, rec.Body)
		}
	})

	t.Run("an untracked tmdb-key view maps the DTO and stores nothing", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		mux := searchTestMux(t, stub)

		rec := doSeasonDetails(t, mux, "?tmdb_id=1396&season=2")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got SeasonDetails
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body is not SeasonDetails: %v", err)
		}

		// Untracked: served live, so there is no row and no id to carry.
		if got.ID != nil {
			t.Errorf("id = %d, want null — an untracked view stores nothing", *got.ID)
		}
		// A show with no state row cannot be dropped. On the wire because the
		// field is in the schema's required list.
		if got.ShowDropped {
			t.Error("show_dropped = true on an untracked view, want false")
		}
		if got.ShowTMDBID != 1396 || got.SeasonNumber != 2 {
			t.Errorf("identity = %+v", got)
		}
		// The show's name rides the root of the same response — no second call.
		if got.ShowTitle != "Breaking Bad" {
			t.Errorf("show_title = %q, want Breaking Bad", got.ShowTitle)
		}
		if got.Name != "Season 2" || got.EpisodeCount != 4 {
			t.Errorf("name/episode_count = %q/%d", got.Name, got.EpisodeCount)
		}
		if got.Year == nil || *got.Year != "2009" {
			t.Errorf("year = %v, want 2009", got.Year)
		}
		if got.Rating == nil || *got.Rating != 8.3 {
			t.Errorf("rating = %v, want 8.3", got.Rating)
		}
		if got.Poster == nil || *got.Poster != "api/v1/video/posters/w185/s2.jpg" ||
			got.PosterFull == nil || *got.PosterFull != "api/v1/video/posters/w500/s2.jpg" {
			t.Errorf("poster = %v / %v, want the w185/w500 proxy URLs", got.Poster, got.PosterFull)
		}

		if len(got.Episodes) != 4 {
			t.Fatalf("episodes = %d entries, want 4", len(got.Episodes))
		}
		full := got.Episodes[0]
		if full.EpisodeNumber != 1 || full.Title == nil || *full.Title != "Seven Thirty-Seven" {
			t.Errorf("episodes[0] = %+v", full)
		}
		if full.RuntimeMin == nil || *full.RuntimeMin != 47 {
			t.Errorf("episodes[0].runtime_min = %v, want 47", full.RuntimeMin)
		}
		if full.Rating == nil || *full.Rating != 8.1 || full.Votes == nil || *full.Votes != 900 {
			t.Errorf("episodes[0] rating = %v votes = %v", full.Rating, full.Votes)
		}
		// Stills are the single w300 variant, through the local proxy.
		if full.Still == nil || *full.Still != "api/v1/video/posters/w300/e1.jpg" {
			t.Errorf("episodes[0].still = %v, want the w300 proxy URL", full.Still)
		}
		// TMDB's zero-values (blank name, runtime 0, vote 0, no still) must
		// serialize as explicit nulls.
		bare := got.Episodes[1]
		if bare.Title != nil || bare.Overview != nil || bare.AirDate != nil ||
			bare.RuntimeMin != nil || bare.Rating != nil || bare.Votes != nil || bare.Still != nil {
			t.Errorf("episodes[1] = %+v, want every nullable null", bare)
		}

		// Markers: episode 1 is the premiere, TMDB's mid_season / finale types
		// map to the up-next vocabulary, and a plain episode carries null.
		wantMarkers := []*string{
			videotest.Ptr("season_premiere"), nil, videotest.Ptr("mid_season_finale"), videotest.Ptr("season_finale"),
		}
		for i, want := range wantMarkers {
			gotM := got.Episodes[i].Marker
			switch {
			case want == nil && gotM != nil:
				t.Errorf("episodes[%d].marker = %q, want null", i, *gotM)
			case want != nil && (gotM == nil || *gotM != *want):
				t.Errorf("episodes[%d].marker = %v, want %q", i, gotM, *want)
			}
		}

		// The season's own aggregate credits: billing order, blank names
		// skipped, parts joined most-episodes-first, counts within the season,
		// photos through the proxy on an untracked view.
		if len(got.Cast) != 2 || got.Cast[0].Name != "Bryan Cranston" || got.Cast[1].Name != "Aaron Paul" {
			t.Fatalf("cast = %+v, want Cranston then Paul", got.Cast)
		}
		if got.Cast[0].Character == nil || *got.Cast[0].Character != "Walter White / Heisenberg" {
			t.Errorf("cast[0].character = %v", got.Cast[0].Character)
		}
		if got.Cast[0].Episodes == nil || *got.Cast[0].Episodes != 4 {
			t.Errorf("cast[0].episodes = %v, want 4 — the count within the season", got.Cast[0].Episodes)
		}
		if got.Cast[1].Photo == nil || *got.Cast[1].Photo != "api/v1/video/posters/w185/ap.jpg" {
			t.Errorf("cast[1].photo = %v", got.Cast[1].Photo)
		}
		// A season TMDB sent no section for renders [] on the wire, never null.
		rec1 := doSeasonDetails(t, mux, "?tmdb_id=1396&season=1")
		var s1 struct {
			Cast []common.CastMember `json:"cast"`
		}
		if err := json.Unmarshal(rec1.Body.Bytes(), &s1); err != nil || rec1.Code != http.StatusOK {
			t.Fatalf("season 1: status %d, err %v", rec1.Code, err)
		}
		if s1.Cast == nil || len(s1.Cast) != 0 {
			t.Errorf("season 1 cast = %v, want an empty array", s1.Cast)
		}

		// With no snapshot there is nothing to serve from, so a repeat view is
		// another live call — exactly an untracked movie's behaviour.
		calls := stub.CallCount("/3/tv/1396")
		rec = doSeasonDetails(t, mux, "?tmdb_id=1396&season=2")
		if rec.Code != http.StatusOK {
			t.Fatalf("repeat status = %d, want 200: %s", rec.Code, rec.Body)
		}
		if n := stub.CallCount("/3/tv/1396"); n != calls+1 {
			t.Errorf("a repeat untracked view made %d calls, want %d", n, calls+1)
		}
	})

	t.Run("two seasons of one show get distinct rows", func(t *testing.T) {
		t.Parallel()
		// Regression: the appended season document carries no integer season
		// id, so any per-season uniqueness derived from one would collide on
		// the second insert. The pair key must be the only constraint hit.
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		ctx := context.Background()
		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}

		s1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 1)
		if err != nil {
			t.Fatalf("season 1: %v", err)
		}
		s2, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 2)
		if err != nil {
			t.Fatalf("season 2: %v", err)
		}
		if s1.ID == s2.ID {
			t.Errorf("both seasons materialised as row %d", s1.ID)
		}
	})

	t.Run("TMDB down with no row is a 503", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{
			"/3/tv/1396": errors.New("dial tcp: connection refused"),
		}}
		rec := doSeasonDetails(t, searchTestMux(t, stub), "?tmdb_id=1396&season=2")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "provider_unavailable" {
			t.Errorf("error code = %q, want provider_unavailable", code)
		}
	})
}

func TestSeasonDetailsStaleness(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a stale snapshot refreshes on view", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, clock := detailsService(t, stub)
		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}

		first, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{TMDBID: "1396", Season: "2"})
		if err != nil {
			t.Fatalf("first view: %v", err)
		}
		if first.ID == nil {
			t.Fatalf("id = null, want the tracked show's season row")
		}
		calls := stub.CallCount("/3/tv/1396")

		// Within the window: served from the snapshot.
		if _, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{ID: strconv.FormatInt(*first.ID, 10)}); err != nil {
			t.Fatalf("fresh view: %v", err)
		}
		if n := stub.CallCount("/3/tv/1396"); n != calls {
			t.Fatalf("a fresh view refetched (%d calls, had %d)", n, calls)
		}

		// Past it: one refetch, and fetched_at moves.
		*clock = clock.Add(seasonStaleAfter + time.Hour)
		refreshed, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{ID: strconv.FormatInt(*first.ID, 10)})
		if err != nil {
			t.Fatalf("stale view: %v", err)
		}
		if n := stub.CallCount("/3/tv/1396"); n != calls+1 {
			t.Errorf("stale view made %d calls, want %d", n, calls+1)
		}
		if refreshed.FetchedAt == first.FetchedAt {
			t.Errorf("fetched_at = %s, want it rewritten by the refresh", refreshed.FetchedAt)
		}
	})

	t.Run("a failed refresh serves the stale snapshot", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, clock := detailsService(t, stub)
		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}

		first, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{TMDBID: "1396", Season: "2"})
		if err != nil {
			t.Fatalf("first view: %v", err)
		}
		if first.ID == nil {
			t.Fatalf("id = null, want the tracked show's season row")
		}

		*clock = clock.Add(seasonStaleAfter + time.Hour)
		stub.Mu.Lock()
		stub.Errs = map[string]error{"/3/tv/1396": errors.New("dial tcp: connection refused")}
		stub.Mu.Unlock()

		stale, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{ID: strconv.FormatInt(*first.ID, 10)})
		if err != nil {
			t.Fatalf("a season with a row must never 503: %v", err)
		}
		if stale.Name != "Season 2" || len(stale.Episodes) != 4 || stale.FetchedAt != first.FetchedAt {
			t.Errorf("stale serve = %+v, want the original snapshot", stale)
		}
	})

	t.Run("a tracked show's new season materialises and links on view", func(t *testing.T) {
		t.Parallel()
		// The one case where a VIEW still writes: the show is already in the
		// catalog and TMDB has gained a season since it was written. Without
		// this the season's episodes would have no rows and could never be
		// marked.
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithOneSeasonBody}}
		svc, _ := detailsService(t, stub)
		show, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		if _, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 2); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("season 2 already present: err = %v, want ErrNotFound", err)
		}
		stub.Mu.Lock()
		stub.Bodies["/3/tv/1396"] = videotest.ShowWithSeasonBody
		stub.Mu.Unlock()

		calls := stub.CallCount("/3/tv/1396")
		got, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{TMDBID: "1396", Season: "2"})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		if n := stub.CallCount("/3/tv/1396"); n != calls+1 {
			t.Errorf("materialising view made %d calls, want exactly %d", n, calls+1)
		}
		if got.ID == nil {
			t.Fatalf("id = null, want the newly materialised row")
		}
		season, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 2)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		if season.ShowID == nil || *season.ShowID != show.ID {
			t.Errorf("season.ShowID = %v, want %d", season.ShowID, show.ID)
		}
		// Its episodes came with it, or the mark path would 404.
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM episodes WHERE show_id = ? AND season_number = 2`, show.ID); n != 4 {
			t.Errorf("episodes = %d, want 4 written alongside the season", n)
		}

		// A second view is snapshot-only.
		if _, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{ID: strconv.FormatInt(*got.ID, 10)}); err != nil {
			t.Fatalf("second view: %v", err)
		}
		if n := stub.CallCount("/3/tv/1396"); n != calls+1 {
			t.Errorf("a materialised season refetched (%d calls, want %d)", n, calls+1)
		}
	})

	t.Run("a stale refresh of a linked season updates its episodes' stills", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, clock := detailsService(t, stub)
		show, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}

		*clock = clock.Add(seasonStaleAfter + time.Hour)
		stub.Mu.Lock()
		stub.Bodies["/3/tv/1396"] = strings.Replace(videotest.ShowWithSeasonBody, "/e1.jpg", "/e1-new.jpg", 1)
		stub.Mu.Unlock()

		if _, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{TMDBID: "1396", Season: "2"}); err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes
WHERE show_id = ? AND season_number = 2 AND episode_number = 1 AND still_path = '/e1-new.jpg'`, show.ID); n != 1 {
			t.Errorf("changed still_path did not reach the episodes row (matches = %d, want 1)", n)
		}
	})

	t.Run("a show details view writes no season rows", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)

		if _, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{TMDBID: "1396"}); err != nil {
			t.Fatalf("ShowDetails: %v", err)
		}
		if _, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 2); !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("a show view materialised a seasons row: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("show details carries season ids once tracked, null before", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)

		// Untracked: no season has a row, however many times it is viewed, so
		// the show page links every season by (tmdb_id, season_number).
		if _, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{TMDBID: "1396", Season: "2"}); err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		before, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails: %v", err)
		}
		for _, s := range before.Seasons {
			if s.ID != nil {
				t.Errorf("season %d id = %d while untracked, want null", s.SeasonNumber, *s.ID)
			}
		}

		// Tracking writes every season at once, so all of them gain ids.
		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		after, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails after tracking: %v", err)
		}
		if len(after.Seasons) == 0 {
			t.Fatal("no seasons on the tracked show")
		}
		for _, s := range after.Seasons {
			if s.ID == nil {
				t.Errorf("season %d id = null after tracking, want a row id", s.SeasonNumber)
			}
		}
	})
}
