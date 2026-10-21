package details

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func doShowDetails(t *testing.T, mux *http.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/video/shows/details"+query, nil))
	return rec
}

func TestGetShowDetails(t *testing.T) {
	t.Parallel()

	t.Run("200 with every nullable field present", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			// A minimal show: everything nullable must serialize as an explicit
			// null, arrays as [].
			"/3/tv/1399": `{"id": 1399, "name": "Bare"}`,
		}}
		mux := searchTestMux(t, stub)

		rec := doShowDetails(t, mux, "?tmdb_id=1399")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}

		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if body["title"] != "Bare" || body["tmdb_id"] != float64(1399) {
			t.Errorf("body = %v", body)
		}
		// A show view is always live: no catalog row, so id is an explicit null
		// and every tracking flag is false.
		if id, present := body["id"]; !present || id != nil {
			t.Errorf("id = %v, want explicit null on a live view", body["id"])
		}
		for _, key := range []string{"tracked", "watched", "watchlisted", "favorited", "in_production"} {
			if v, ok := body[key].(bool); !ok || v {
				t.Errorf("%s = %v, want false", key, body[key])
			}
		}
		for _, key := range []string{
			"original_title", "first_air_date", "last_air_date", "year", "end_year",
			"episode_runtime_min", "show_status", "overview", "tagline", "next_episode",
			"rating", "votes", "poster", "poster_full", "homepage", "imdb_id",
			"trailer_url", "original_language", "popularity",
		} {
			v, present := body[key]
			if !present {
				t.Errorf("%s key absent, want explicit null", key)
			} else if v != nil {
				t.Errorf("%s = %v, want null", key, v)
			}
		}
		for _, key := range []string{
			"genres", "created_by", "networks", "cast", "seasons",
			"videos", "backdrops", "posters", "similar",
		} {
			v, ok := body[key].([]any)
			if !ok || len(v) != 0 {
				t.Errorf("%s = %v, want an empty array", key, body[key])
			}
		}
		for _, key := range []string{"seasons_count", "episodes_count"} {
			if v, ok := body[key].(float64); !ok || v != 0 {
				t.Errorf("%s = %v, want 0", key, body[key])
			}
		}
		if _, ok := body["fetched_at"].(string); !ok {
			t.Errorf("fetched_at = %v, want an RFC3339 string", body["fetched_at"])
		}
	})

	t.Run("a full show projects seasons, facts and media", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/tv/1396": `{
				"id": 1396, "name": "Breaking Bad", "original_name": "Breaking Bad",
				"first_air_date": "2008-01-20", "last_air_date": "2013-09-29",
				"episode_run_time": [45, 47], "number_of_seasons": 5,
				"number_of_episodes": 62, "status": "Ended", "in_production": false,
				"overview": "A teacher turns to crime.", "tagline": "All hail the king",
				"genres": [{"id": 18, "name": "Drama"}, {"id": 0, "name": ""}],
				"poster_path": "/show.jpg", "vote_average": 8.9, "vote_count": 1234,
				"homepage": "https://example.test", "original_language": "en",
				"popularity": 321.5,
				"created_by": [{"name": "Vince Gilligan"}, {"name": ""}],
				"networks": [{"name": "AMC"}],
				"next_episode_to_air": {
					"season_number": 6, "episode_number": 2,
					"name": "Nope", "air_date": "2026-09-01"
				},
				"seasons": [
					{"season_number": 0, "name": "Specials", "episode_count": 5,
					 "air_date": "2009-02-17", "poster_path": "/sp.jpg", "vote_average": 0},
					{"season_number": 2, "name": "Season 2", "episode_count": 13,
					 "air_date": "2009-03-08", "overview": "Two", "vote_average": 8.3},
					{"season_number": 1, "name": "Season 1", "episode_count": 7,
					 "air_date": "2008-01-20", "poster_path": "/s1.jpg"}
				],
				"external_ids": {"imdb_id": "tt0903747"},
				"watch/providers": {"results": {
					"US": {
						"link": "https://watch.example/tv/1396?locale=US",
						"flatrate": [{"provider_id": 8, "provider_name": "Netflix",
						              "logo_path": "/nf.jpg", "display_priority": 3}],
						"buy": [{"provider_id": 10, "provider_name": "Amazon Video",
						         "logo_path": "/av.jpg", "display_priority": 6}]
					}
				}},
				"aggregate_credits": {"cast": [
					{"name": "Aaron Paul", "order": 1, "profile_path": "/ap.jpg",
					 "total_episode_count": 62,
					 "roles": [{"character": "Jesse Pinkman", "episode_count": 62}]},
					{"name": "", "order": 0, "total_episode_count": 1,
					 "roles": [{"character": "Nobody", "episode_count": 1}]},
					{"name": "Bryan Cranston", "order": 0, "total_episode_count": 62,
					 "roles": [{"character": "Heisenberg", "episode_count": 3},
					           {"character": "Walter White", "episode_count": 62}]}
				]},
				"videos": {"results": [
					{"key": "XZ8ZP0nyBSw", "name": "Clip", "site": "YouTube",
					 "type": "Clip", "official": true, "published_at": "2013-01-01T00:00:00.000Z"},
					{"key": "HhesaQXLuRY", "name": "Trailer", "site": "YouTube",
					 "type": "Trailer", "official": true, "published_at": "2012-01-01T00:00:00.000Z"}
				]},
				"images": {
					"backdrops": [{"file_path": "/b.jpg", "width": 1920, "height": 1080,
					               "vote_average": 5, "vote_count": 9}],
					"posters": [{"file_path": "/p.jpg", "width": 600, "height": 900,
					             "vote_average": 4, "vote_count": 2}]
				},
				"recommendations": {"results": [
					{"id": 60059, "name": "Better Call Saul", "first_air_date": "2015-02-08",
					 "poster_path": "/bcs.jpg", "vote_average": 8.7},
					{"id": 1398, "name": "The Sopranos", "first_air_date": "1999-01-10",
					 "poster_path": "", "vote_average": 0}
				]}
			}`,
		}}

		rec := doShowDetails(t, searchTestMux(t, stub), "?tmdb_id=1396")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got ShowDetails
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body is not ShowDetails: %v", err)
		}

		if got.Year == nil || *got.Year != "2008" {
			t.Errorf("year = %v, want 2008", got.Year)
		}
		// A show that has stopped airing gets an end year.
		if got.EndYear == nil || *got.EndYear != "2013" {
			t.Errorf("end_year = %v, want 2013", got.EndYear)
		}
		// TMDB reports an array; the first entry is what its own pages show.
		if got.EpisodeRuntimeMin == nil || *got.EpisodeRuntimeMin != 45 {
			t.Errorf("episode_runtime_min = %v, want 45", got.EpisodeRuntimeMin)
		}
		if got.ShowStatus == nil || *got.ShowStatus != "Ended" {
			t.Errorf("show_status = %v, want Ended", got.ShowStatus)
		}
		// Blank names are dropped, not rendered as empty chips.
		if len(got.Genres) != 1 || got.Genres[0] != "Drama" {
			t.Errorf("genres = %v, want [Drama]", got.Genres)
		}
		if len(got.CreatedBy) != 1 || got.CreatedBy[0] != "Vince Gilligan" {
			t.Errorf("created_by = %v, want [Vince Gilligan]", got.CreatedBy)
		}
		if len(got.Networks) != 1 || got.Networks[0] != "AMC" {
			t.Errorf("networks = %v, want [AMC]", got.Networks)
		}
		if got.NextEpisode == nil || got.NextEpisode.Season != 6 || got.NextEpisode.Number != 2 {
			t.Errorf("next_episode = %+v, want S6E2", got.NextEpisode)
		}
		if got.IMDBID == nil || *got.IMDBID != "tt0903747" {
			t.Errorf("imdb_id = %v", got.IMDBID)
		}

		// Seasons: ascending, Specials last, never in TMDB's response order.
		wantOrder := []int{1, 2, 0}
		if len(got.Seasons) != len(wantOrder) {
			t.Fatalf("seasons = %d entries, want %d", len(got.Seasons), len(wantOrder))
		}
		for i, want := range wantOrder {
			if got.Seasons[i].SeasonNumber != want {
				t.Errorf("seasons[%d].season_number = %d, want %d", i, got.Seasons[i].SeasonNumber, want)
			}
		}
		if got.Seasons[0].Poster == nil || *got.Seasons[0].Poster != "api/v1/video/posters/w185/s1.jpg" {
			t.Errorf("seasons[0].poster = %v, want the relative w185 proxy URL", got.Seasons[0].Poster)
		}
		if got.Seasons[0].Year == nil || *got.Seasons[0].Year != "2008" {
			t.Errorf("seasons[0].year = %v, want 2008", got.Seasons[0].Year)
		}
		// TMDB reports 0 for an unrated season — null is the honest rendering.
		if got.Seasons[2].Rating != nil {
			t.Errorf("seasons[2].rating = %v, want null", *got.Seasons[2].Rating)
		}
		if got.Seasons[1].Rating == nil || *got.Seasons[1].Rating != 8.3 {
			t.Errorf("seasons[1].rating = %v, want 8.3", got.Seasons[1].Rating)
		}
		// A season with no poster renders a placeholder, not a broken image.
		if got.Seasons[1].Poster != nil {
			t.Errorf("seasons[1].poster = %v, want null", *got.Seasons[1].Poster)
		}

		// Cast: aggregate credits in billing order, blank names skipped, photos
		// through the proxy, several parts joined most-episodes-first.
		if len(got.Cast) != 2 || got.Cast[0].Name != "Bryan Cranston" || got.Cast[1].Name != "Aaron Paul" {
			t.Fatalf("cast = %+v, want Cranston then Paul", got.Cast)
		}
		if got.Cast[0].Character == nil || *got.Cast[0].Character != "Walter White / Heisenberg" {
			t.Errorf("cast[0].character = %v, want the parts joined, most episodes first", got.Cast[0].Character)
		}
		if got.Cast[0].Episodes == nil || *got.Cast[0].Episodes != 62 {
			t.Errorf("cast[0].episodes = %v, want 62", got.Cast[0].Episodes)
		}
		if got.Cast[1].Photo == nil || *got.Cast[1].Photo != "api/v1/video/posters/w185/ap.jpg" {
			t.Errorf("cast[1].photo = %v", got.Cast[1].Photo)
		}
		if got.Cast[0].Photo != nil {
			t.Errorf("cast[0].photo = %v, want null", *got.Cast[0].Photo)
		}

		// Similar: TMDB's recommendations in TMDB's order, read off the TV
		// shape (name / first_air_date), posters through the proxy, TMDB's 0
		// score and missing poster rendered null.
		if len(got.Similar) != 2 || got.Similar[0].TMDBID != 60059 || got.Similar[1].TMDBID != 1398 {
			t.Fatalf("similar = %+v, want Better Call Saul then The Sopranos", got.Similar)
		}
		if got.Similar[0].Title != "Better Call Saul" || got.Similar[0].Year == nil || *got.Similar[0].Year != "2015" {
			t.Errorf("similar[0] = %+v, want title from name and year from first_air_date", got.Similar[0])
		}
		if got.Similar[0].Poster == nil || *got.Similar[0].Poster != "api/v1/video/posters/w185/bcs.jpg" ||
			got.Similar[0].PosterFull == nil || *got.Similar[0].PosterFull != "api/v1/video/posters/w500/bcs.jpg" {
			t.Errorf("similar[0] posters = %v / %v, want the two proxy variants", got.Similar[0].Poster, got.Similar[0].PosterFull)
		}
		if got.Similar[1].Rating != nil || got.Similar[1].Poster != nil || got.Similar[1].PosterFull != nil {
			t.Errorf("similar[1] = %+v, want null rating and posters", got.Similar[1])
		}

		// The trailer link and the Videos tab agree on which video ranks first.
		if got.TrailerURL == nil || *got.TrailerURL != "https://www.youtube.com/watch?v=HhesaQXLuRY" {
			t.Errorf("trailer_url = %v", got.TrailerURL)
		}
		if len(got.Videos) != 2 || got.Videos[0].Name != "Trailer" {
			t.Fatalf("videos = %+v, want the trailer first", got.Videos)
		}
		if got.Videos[0].Thumb == nil || *got.Videos[0].Thumb != "api/v1/video/youtube-thumbs/HhesaQXLuRY" {
			t.Errorf("videos[0].thumb = %v", got.Videos[0].Thumb)
		}

		if len(got.Backdrops) != 1 || got.Backdrops[0].Thumb != "api/v1/video/posters/w300/b.jpg" ||
			got.Backdrops[0].Full != "api/v1/video/posters/w1280/b.jpg" {
			t.Errorf("backdrops = %+v, want the w300/w1280 ladder", got.Backdrops)
		}
		if len(got.Posters) != 1 || got.Posters[0].Width != 600 {
			t.Errorf("posters = %+v", got.Posters)
		}
		if got.Poster == nil || *got.Poster != "api/v1/video/posters/w185/show.jpg" {
			t.Errorf("poster = %v", got.Poster)
		}
		// Nothing is stored, so nothing can reference the show.
		if got.ID != nil || got.Tracked || got.Watched || got.Watchlisted {
			t.Errorf("a live view must be untracked: %v %v %v %v",
				got.ID, got.Tracked, got.Watched, got.Watchlisted)
		}
	})

	t.Run("a returning series has no end year", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/tv/2": `{"id": 2, "name": "Ongoing", "first_air_date": "2020-05-01",
			             "last_air_date": "2026-08-01", "status": "Returning Series",
			             "in_production": true}`,
		}}
		rec := doShowDetails(t, searchTestMux(t, stub), "?tmdb_id=2")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
		}
		var got ShowDetails
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body is not ShowDetails: %v", err)
		}
		if got.EndYear != nil {
			t.Errorf("end_year = %v, want null while in production", *got.EndYear)
		}
		if got.LastAirDate == nil || *got.LastAirDate != "2026-08-01" {
			t.Errorf("last_air_date = %v, want the most recent episode's date", got.LastAirDate)
		}
	})

	t.Run("bad requests", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			query string
			want  int
		}{
			{"no tmdb_id", "", http.StatusUnprocessableEntity},
			{"empty tmdb_id", "?tmdb_id=", http.StatusUnprocessableEntity},
			{"non-numeric tmdb_id", "?tmdb_id=abc", http.StatusUnprocessableEntity},
			{"zero tmdb_id", "?tmdb_id=0", http.StatusUnprocessableEntity},
			{"negative tmdb_id", "?tmdb_id=-3", http.StatusUnprocessableEntity},
			// id is a real parameter now, and exclusive with tmdb_id.
			{"both id and tmdb_id", "?id=7&tmdb_id=1396", http.StatusUnprocessableEntity},
			{"non-numeric id", "?id=abc", http.StatusUnprocessableEntity},
			{"zero id", "?id=0", http.StatusUnprocessableEntity},
			// An id no tracked show holds is a plain 404, and never a TMDB call.
			{"unknown local id", "?id=7", http.StatusNotFound},
			// An id TMDB does not know is a plain 404, straight from the provider.
			{"unknown show", "?tmdb_id=99999999", http.StatusNotFound},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				rec := doShowDetails(t, searchTestMux(t, &videotest.ProviderStub{}), tc.query)
				if rec.Code != tc.want {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body)
				}
			})
		}
	})

	t.Run("TMDB down is a 503 — there is no snapshot to fall back on", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{
			"/3/tv/1396": errors.New("dial tcp: connection refused"),
		}}
		rec := doShowDetails(t, searchTestMux(t, stub), "?tmdb_id=1396")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body)
		}
		if code := videotest.ErrorCode(t, rec); code != "provider_unavailable" {
			t.Errorf("error code = %q, want provider_unavailable", code)
		}
	})
}
