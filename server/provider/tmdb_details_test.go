package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"mbyte.ro/sofalog/httpx"
)

const movieDetailsBody = `{
  "id": 603,
  "title": "The Matrix",
  "original_title": "The Matrix",
  "release_date": "1999-03-30",
  "runtime": 136,
  "overview": "A hacker learns the truth.",
  "tagline": "Free your mind.",
  "genres": [{"id": 28, "name": "Action"}, {"id": 878, "name": "Science Fiction"}],
  "poster_path": "/m.jpg",
  "backdrop_path": "/b.jpg",
  "vote_average": 8.2,
  "vote_count": 26000,
  "credits": {"cast": [{"name": "Keanu Reeves", "character": "Neo", "order": 0}]}
}`

func TestTMDBMovieDetails(t *testing.T) {
	t.Parallel()

	t.Run("decodes the promoted fields and returns the raw body verbatim", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: movieDetailsBody}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		details, raw, err := c.MovieDetails(context.Background(), 603)
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if details.ID != 603 || details.Title != "The Matrix" || details.Runtime != 136 {
			t.Errorf("details = %+v", details)
		}
		if details.Tagline != "Free your mind." || details.ReleaseDate != "1999-03-30" {
			t.Errorf("details = %+v", details)
		}
		if len(details.Genres) != 2 || details.Genres[0].Name != "Action" {
			t.Errorf("genres = %+v", details.Genres)
		}
		if details.VoteAverage != 8.2 || details.VoteCount != 26000 {
			t.Errorf("votes = %v/%v", details.VoteAverage, details.VoteCount)
		}
		// The raw body is the stored snapshot: it must be the response
		// verbatim, credits included, not a re-encoding of the struct.
		if string(raw) != movieDetailsBody {
			t.Errorf("raw body was not passed through verbatim")
		}

		req := stub.requests[0]
		if got := req.URL.Path; got != "/3/movie/603" {
			t.Errorf("path = %s, want /3/movie/603", got)
		}
		if got := req.URL.Query().Get("append_to_response"); got != "credits,external_ids,videos,images,watch/providers,recommendations" {
			t.Errorf("append_to_response = %q, want credits,external_ids,videos,images,watch/providers,recommendations", got)
		}
		// Without this the galleries come back language-filtered, which drops
		// exactly the textless backdrops the media section wants.
		if got := req.URL.Query().Get("include_image_language"); got != "en,null" {
			t.Errorf("include_image_language = %q, want en,null", got)
		}
	})

	t.Run("an unknown id is 404, not a provider outage", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{status: http.StatusNotFound, body: `{"status_message":"not found"}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		_, _, err := c.MovieDetails(context.Background(), 999999999)
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("a 500 is a provider outage", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{status: http.StatusInternalServerError, body: "boom"}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		_, _, err := c.MovieDetails(context.Background(), 603)
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want ErrProviderUnavailable", err)
		}
	})

	t.Run("a transport error never leaks the key", func(t *testing.T) {
		t.Parallel()
		// net/http embeds the full request URL — key included on the v3 auth
		// path — in transport errors; redact must strip it.
		stub := &stubDoer{err: errors.New(`Get "https://provider.example/3/movie/603?api_key=v3key": dial tcp: timeout`)}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		_, _, err := c.MovieDetails(context.Background(), 603)
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want ErrProviderUnavailable", err)
		}
		if strings.Contains(err.Error(), "v3key") {
			t.Fatalf("the API key leaked into the error: %v", err)
		}
	})
}

const tvDetailsBody = `{
  "id": 1396,
  "name": "Breaking Bad",
  "first_air_date": "2008-01-20",
  "seasons": [{"season_number": 1, "name": "Season 1", "episode_count": 7}],
  "aggregate_credits": {"cast": [{"name": "Bryan Cranston", "order": 0,
    "total_episode_count": 62, "roles": [{"character": "Walter White", "episode_count": 62}]}]}
}`

func TestTMDBTVDetails(t *testing.T) {
	t.Parallel()

	t.Run("appends the aggregate credits and returns the raw body verbatim", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: tvDetailsBody}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		details, raw, err := c.TVDetails(context.Background(), 1396)
		if err != nil {
			t.Fatalf("TVDetails: %v", err)
		}
		if details.ID != 1396 || details.Name != "Breaking Bad" || len(details.Seasons) != 1 {
			t.Errorf("details = %+v", details)
		}
		// The raw body is the stored snapshot, and the appended sections are
		// parsed back out of it — so it must be the response verbatim.
		if string(raw) != tvDetailsBody {
			t.Errorf("raw body was not passed through verbatim")
		}

		req := stub.requests[0]
		if got := req.URL.Path; got != "/3/tv/1396" {
			t.Errorf("path = %s, want /3/tv/1396", got)
		}
		// aggregate_credits, never credits: TMDB's tv credits is the latest
		// season's regulars only, which is how a show its site lists 150 people
		// for rendered five.
		if got := req.URL.Query().Get("append_to_response"); got != "aggregate_credits,external_ids,videos,images,watch/providers,recommendations" {
			t.Errorf("append_to_response = %q, want aggregate_credits,external_ids,videos,images,watch/providers,recommendations", got)
		}
		if got := req.URL.Query().Get("include_image_language"); got != "en,null" {
			t.Errorf("include_image_language = %q, want en,null", got)
		}
	})
}

const tvSeasonBody = `{
  "id": 1396, "name": "Breaking Bad",
  "season/1": {"_id": "abc", "season_number": 1, "name": "Season 1", "air_date": "2008-01-20",
    "episodes": [{"episode_number": 1, "name": "Pilot",
      "guest_stars": [{"id": 1223192, "name": "Max Arciniega", "character": "Krazy-8", "order": 12, "profile_path": "/ma.jpg"}]}]},
  "season/1/aggregate_credits": {"cast": [{"name": "Bryan Cranston", "order": 0,
    "total_episode_count": 7, "roles": [{"character": "Walter White", "episode_count": 7}]}]},
  "season/1/credits": {"cast": [{"name": "Bryan Cranston", "character": "Walter White", "order": 0}]}
}`

func TestTMDBTVSeasonDetails(t *testing.T) {
	t.Parallel()

	t.Run("appends the season's aggregate and regular credits and folds both into the season document", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: tvSeasonBody}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		season, showTitle, raw, err := c.TVSeasonDetails(context.Background(), 1396, 1)
		if err != nil {
			t.Fatalf("TVSeasonDetails: %v", err)
		}
		if season.SeasonNumber != 1 || season.Name != "Season 1" || len(season.Episodes) != 1 {
			t.Errorf("season = %+v", season)
		}
		// Guest stars ride the episode entry as TMDB sends it — no append.
		if gs := season.Episodes[0].GuestStars; len(gs) != 1 || gs[0].ID != 1223192 || gs[0].Name != "Max Arciniega" || gs[0].Character != "Krazy-8" || gs[0].ProfilePath != "/ma.jpg" {
			t.Errorf("guest_stars = %+v", season.Episodes[0].GuestStars)
		}
		if showTitle != "Breaking Bad" {
			t.Errorf("showTitle = %q", showTitle)
		}
		// The nested append arrives as a sibling key; the stored document must
		// carry it, or a snapshot read back could never render a cast.
		type castList struct {
			Cast []struct {
				Name string `json:"name"`
			} `json:"cast"`
		}
		var doc struct {
			SeasonNumber int       `json:"season_number"`
			Aggregate    *castList `json:"aggregate_credits"`
			Credits      *castList `json:"credits"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("raw is not JSON: %v", err)
		}
		if doc.SeasonNumber != 1 || doc.Aggregate == nil || len(doc.Aggregate.Cast) != 1 || doc.Aggregate.Cast[0].Name != "Bryan Cranston" {
			t.Errorf("raw = %s, want the season document with aggregate_credits folded in", raw)
		}
		if doc.Credits == nil || len(doc.Credits.Cast) != 1 || doc.Credits.Cast[0].Name != "Bryan Cranston" {
			t.Errorf("raw = %s, want the season's regular credits folded in too", raw)
		}

		req := stub.requests[0]
		if got := req.URL.Path; got != "/3/tv/1396" {
			t.Errorf("path = %s, want /3/tv/1396", got)
		}
		if got := req.URL.Query().Get("append_to_response"); got != "season/1,season/1/aggregate_credits,season/1/credits" {
			t.Errorf("append_to_response = %q, want season/1,season/1/aggregate_credits,season/1/credits", got)
		}
	})

	t.Run("a response without the nested sections still writes both keys, as null", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: `{"id": 1396, "name": "Breaking Bad",
			"season/1": {"season_number": 1, "name": "Season 1", "episodes": []}}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		_, _, raw, err := c.TVSeasonDetails(context.Background(), 1396, 1)
		if err != nil {
			t.Fatalf("TVSeasonDetails: %v", err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("raw is not JSON: %v", err)
		}
		// Present-and-null: distinguishable from a pre-append snapshot (no key,
		// which the details gate refreshes) and from "nobody" ({"cast": []}).
		for _, key := range []string{"aggregate_credits", "credits"} {
			if v, ok := doc[key]; !ok || string(v) != "null" {
				t.Errorf("%s = %s (present=%v), want an explicit null", key, v, ok)
			}
		}
	})
}
