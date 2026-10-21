package details

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

// The episode view has no fixture of its own: an episode is one entry in its
// season's snapshot, so ShowWithSeasonBody (see handlers_season_details_test)
// is the whole world this endpoint reads.
func doEpisodeDetails(t *testing.T, mux *http.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/video/episodes/details"+query, nil))
	return rec
}

func TestGetEpisodeDetails(t *testing.T) {
	t.Parallel()

	t.Run("bad requests", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			query string
		}{
			{"no params", ""},
			{"episode without a season", "?episode=1"},
			{"season_id without episode", "?season_id=1"},
			{"tmdb key without episode", "?tmdb_id=1396&season=2"},
			{"season_id with tmdb_id", "?season_id=1&tmdb_id=1396&episode=1"},
			{"season_id with season", "?season_id=1&season=2&episode=1"},
			{"tmdb_id without season", "?tmdb_id=1396&episode=1"},
			{"season without tmdb_id", "?season=2&episode=1"},
			{"non-numeric season_id", "?season_id=abc&episode=1"},
			{"zero season_id", "?season_id=0&episode=1"},
			{"non-numeric episode", "?tmdb_id=1396&season=2&episode=one"},
			{"negative episode", "?tmdb_id=1396&season=2&episode=-1"},
		}
		for _, tc := range tests {
			t.Run(tc.name+" returns 422 envelope", func(t *testing.T) {
				t.Parallel()
				rec := doEpisodeDetails(t, searchTestMux(t, &videotest.ProviderStub{}), tc.query)
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body)
				}
				if code := videotest.ErrorCode(t, rec); code != "validation" {
					t.Errorf("code = %q, want validation", code)
				}
			})
		}
	})

	t.Run("episode zero passes validation", func(t *testing.T) {
		t.Parallel()
		// The fixture's season 2 has no episode 0, so this 404s — the point is
		// that it is not a 422: a show may number a pilot or a recap 0.
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		rec := doEpisodeDetails(t, searchTestMux(t, stub), "?tmdb_id=1396&season=2&episode=0")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (not 422): %s", rec.Code, rec.Body)
		}
	})

	t.Run("a season_id miss is a plain 404, never a TMDB call", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		rec := doEpisodeDetails(t, searchTestMux(t, stub), "?season_id=99999&episode=1")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
		if n := stub.CallCount("/3/tv/99999"); n != 0 {
			t.Errorf("a season_id miss reached TMDB %d times", n)
		}
	})

	t.Run("an episode the season does not list is a 404", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		rec := doEpisodeDetails(t, searchTestMux(t, stub), "?tmdb_id=1396&season=2&episode=999")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
	})

	t.Run("an untracked tmdb-key view projects the episode", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		mux := searchTestMux(t, stub)

		rec := doEpisodeDetails(t, mux, "?tmdb_id=1396&season=2&episode=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got EpisodeDetails
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body is not EpisodeDetails: %v", err)
		}

		// Nothing was stored on the way through, so there is no season id to
		// link back by — the page uses (show_tmdb_id, season_number) instead.
		if got.SeasonID != nil {
			t.Errorf("season_id = %d, want null on an untracked show", *got.SeasonID)
		}
		if got.SeasonNumber != 2 || got.SeasonName != "Season 2" {
			t.Errorf("season context = %+v", got)
		}
		if got.ShowTMDBID != 1396 || got.ShowTitle != "Breaking Bad" {
			t.Errorf("show context = %d / %q", got.ShowTMDBID, got.ShowTitle)
		}
		if got.FetchedAt == "" {
			t.Error("fetched_at is empty; the season snapshot's time belongs here")
		}
		// [] on the wire, never null — an untracked season carries no copies,
		// and only the JSON round trip tells the two apart.
		if got.SeasonCopies == nil || len(got.SeasonCopies) != 0 {
			t.Errorf("season_copies = %#v, want []", got.SeasonCopies)
		}
		// Borrowed two rungs up, and false for the same reason the season
		// view's is: an untracked show has no state row to be dropped in.
		if got.ShowDropped {
			t.Error("show_dropped = true on an untracked view, want false")
		}

		ep := got.Episode
		if ep.EpisodeNumber != 1 || ep.Title == nil || *ep.Title != "Seven Thirty-Seven" {
			t.Errorf("episode = %+v", ep)
		}
		if ep.Overview == nil || *ep.Overview != "A grim omen." {
			t.Errorf("overview = %v, want the season list's withheld copy", ep.Overview)
		}
		if ep.RuntimeMin == nil || *ep.RuntimeMin != 47 {
			t.Errorf("runtime_min = %v, want 47", ep.RuntimeMin)
		}
		if ep.Rating == nil || *ep.Rating != 8.1 || ep.Votes == nil || *ep.Votes != 900 {
			t.Errorf("rating = %v votes = %v", ep.Rating, ep.Votes)
		}
		if ep.Marker == nil || *ep.Marker != "season_premiere" {
			t.Errorf("marker = %v, want season_premiere", ep.Marker)
		}
		// Both still variants, through the local proxy — never a TMDB URL.
		if ep.Still == nil || *ep.Still != "api/v1/video/posters/w300/e1.jpg" {
			t.Errorf("still = %v, want the w300 proxy URL", ep.Still)
		}
		if ep.StillFull == nil || *ep.StillFull != "api/v1/video/posters/w1280/e1.jpg" {
			t.Errorf("still_full = %v, want the w1280 proxy URL", ep.StillFull)
		}

		// Cast is the SEASON's regulars (its folded credits), guest stars are
		// this episode's own — both in billing order, blanks dropped, no episode
		// counts, photos through the proxy on an untracked view.
		if len(got.Cast) != 2 || got.Cast[0].Name != "Bryan Cranston" || got.Cast[1].Name != "Aaron Paul" {
			t.Fatalf("cast = %+v, want the season's regulars, Cranston then Paul", got.Cast)
		}
		if got.Cast[0].Episodes != nil || got.Cast[1].Photo == nil || *got.Cast[1].Photo != "api/v1/video/posters/w185/ap.jpg" {
			t.Errorf("cast = %+v, want null episodes and a proxied photo", got.Cast)
		}
		if len(got.GuestStars) != 2 || got.GuestStars[0].Name != "Max Arciniega" || got.GuestStars[1].Name != "Raymond Cruz" {
			t.Fatalf("guest_stars = %+v, want Arciniega (order 3) then Cruz (order 8)", got.GuestStars)
		}
		if got.GuestStars[1].Character == nil || *got.GuestStars[1].Character != "Tuco Salamanca" ||
			got.GuestStars[1].Photo == nil || *got.GuestStars[1].Photo != "api/v1/video/posters/w185/rc.jpg" {
			t.Errorf("guest_stars[1] = %+v", got.GuestStars[1])
		}
		// Both rails carry the person id when the document has one (D-027):
		// a regular's from the folded credits, a guest star's from the episode.
		if got.Cast[1].TMDBID == nil || *got.Cast[1].TMDBID != 84497 || got.Cast[0].TMDBID != nil {
			t.Errorf("cast person ids = %v/%v, want null/84497", got.Cast[0].TMDBID, got.Cast[1].TMDBID)
		}
		if got.GuestStars[1].TMDBID == nil || *got.GuestStars[1].TMDBID != 62912 || got.GuestStars[0].TMDBID != nil {
			t.Errorf("guest star person ids = %v/%v, want null/62912", got.GuestStars[0].TMDBID, got.GuestStars[1].TMDBID)
		}

		// An episode TMDB lists no guest stars for renders [] on the wire, and
		// still carries the season's regulars.
		rec2 := doEpisodeDetails(t, mux, "?tmdb_id=1396&season=2&episode=2")
		var e2 struct {
			Cast       []common.CastMember `json:"cast"`
			GuestStars []common.CastMember `json:"guest_stars"`
		}
		if err := json.Unmarshal(rec2.Body.Bytes(), &e2); err != nil || rec2.Code != http.StatusOK {
			t.Fatalf("episode 2: status %d, err %v", rec2.Code, err)
		}
		if e2.GuestStars == nil || len(e2.GuestStars) != 0 {
			t.Errorf("episode 2 guest_stars = %v, want an empty array", e2.GuestStars)
		}
		if len(e2.Cast) != 2 {
			t.Errorf("episode 2 cast = %+v, want the same two regulars", e2.Cast)
		}
	})

	t.Run("TMDB's zero values become explicit nulls", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		rec := doEpisodeDetails(t, searchTestMux(t, stub), "?tmdb_id=1396&season=2&episode=2")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got EpisodeDetails
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body is not EpisodeDetails: %v", err)
		}
		ep := got.Episode
		if ep.Title != nil || ep.Overview != nil || ep.AirDate != nil ||
			ep.RuntimeMin != nil || ep.Rating != nil || ep.Votes != nil ||
			ep.Still != nil || ep.StillFull != nil {
			t.Errorf("episode = %+v, want every nullable null", ep)
		}
	})

	t.Run("TMDB down with no season row is a 503", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{
			"/3/tv/1396": errors.New("dial tcp: connection refused"),
		}}
		rec := doEpisodeDetails(t, searchTestMux(t, stub), "?tmdb_id=1396&season=2&episode=1")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "provider_unavailable" {
			t.Errorf("error code = %q, want provider_unavailable", code)
		}
	})

	t.Run("a season with a row never 503s", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		ctx := context.Background()
		// A row means a tracked show — that is now the only way to have one.
		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}

		// TMDB goes away. The snapshot is still fresh, so nothing even asks —
		// and once it is stale the stale-serve path covers it (see
		// TestSeasonDetailsStaleness).
		stub.Mu.Lock()
		stub.Errs = map[string]error{"/3/tv/1396": errors.New("dial tcp: connection refused")}
		stub.Mu.Unlock()

		got, err := svc.EpisodeDetails(ctx, 0, EpisodeDetailsParams{TMDBID: "1396", Season: "2", Episode: "1"})
		if err != nil {
			t.Fatalf("a season with a row must never 503: %v", err)
		}
		if got.SeasonID == nil {
			t.Error("season_id = null on a tracked show, want the row id")
		}
	})
}
