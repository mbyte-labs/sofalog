package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"mbyte.ro/sofalog/httpx"
)

// personDetailsBody is /person/287 with combined_credits and external_ids
// appended, trimmed to what the person page reads. The two Fight Club entries
// are deliberate: TMDB lists a title once per character.
const personDetailsBody = `{
  "id": 287,
  "name": "Brad Pitt",
  "biography": "An American actor and producer.",
  "known_for_department": "Acting",
  "birthday": "1963-12-18",
  "deathday": null,
  "place_of_birth": "Shawnee, Oklahoma, USA",
  "also_known_as": ["William Bradley Pitt"],
  "homepage": null,
  "profile_path": "/bp.jpg",
  "external_ids": {"imdb_id": "nm0000093", "instagram_id": null},
  "combined_credits": {
    "cast": [
      {"id": 550, "media_type": "movie", "title": "Fight Club", "release_date": "1999-10-15",
       "character": "Tyler Durden", "poster_path": "/fc.jpg", "vote_average": 8.4, "order": 1},
      {"id": 550, "media_type": "movie", "title": "Fight Club", "release_date": "1999-10-15",
       "character": "Narrator's alter ego", "poster_path": "/fc.jpg", "vote_average": 8.4, "order": 1},
      {"id": 1399, "media_type": "tv", "name": "Friends", "first_air_date": "1994-09-22",
       "character": "Will Colbert", "episode_count": 1, "poster_path": "/fr.jpg", "vote_average": 8.4}
    ],
    "crew": [{"id": 9999, "media_type": "movie", "job": "Producer"}]
  }
}`

func TestTMDBPersonDetails(t *testing.T) {
	t.Parallel()

	t.Run("decodes the person, the external ids and the combined cast credits", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{body: personDetailsBody}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		p, err := c.PersonDetails(context.Background(), 287)
		if err != nil {
			t.Fatalf("PersonDetails: %v", err)
		}
		if p.ID != 287 || p.Name != "Brad Pitt" || p.KnownForDepartment != "Acting" {
			t.Errorf("person = %+v", p)
		}
		if p.Birthday != "1963-12-18" || p.Deathday != "" || p.PlaceOfBirth != "Shawnee, Oklahoma, USA" {
			t.Errorf("dates/place = %q %q %q", p.Birthday, p.Deathday, p.PlaceOfBirth)
		}
		if p.Biography != "An American actor and producer." || p.ProfilePath != "/bp.jpg" || p.Homepage != "" {
			t.Errorf("bio/photo/homepage = %q %q %q", p.Biography, p.ProfilePath, p.Homepage)
		}
		if len(p.AlsoKnownAs) != 1 || p.AlsoKnownAs[0] != "William Bradley Pitt" {
			t.Errorf("also_known_as = %v", p.AlsoKnownAs)
		}
		if p.ExternalIDs.IMDBID != "nm0000093" {
			t.Errorf("imdb_id = %q", p.ExternalIDs.IMDBID)
		}
		cast := p.CombinedCredits.Cast
		if len(cast) != 3 {
			t.Fatalf("cast = %d entries, want 3 (one per character, undeduplicated here)", len(cast))
		}
		if cast[0].ID != 550 || cast[0].MediaType != "movie" || cast[0].Title != "Fight Club" ||
			cast[0].ReleaseDate != "1999-10-15" || cast[0].Character != "Tyler Durden" ||
			cast[0].PosterPath != "/fc.jpg" || cast[0].VoteAverage != 8.4 || cast[0].EpisodeCount != 0 {
			t.Errorf("cast[0] = %+v", cast[0])
		}
		if cast[2].ID != 1399 || cast[2].MediaType != "tv" || cast[2].Name != "Friends" ||
			cast[2].FirstAirDate != "1994-09-22" || cast[2].EpisodeCount != 1 {
			t.Errorf("cast[2] = %+v", cast[2])
		}

		req := stub.requests[0]
		if got := req.URL.Path; got != "/3/person/287" {
			t.Errorf("path = %s, want /3/person/287", got)
		}
		if got := req.URL.Query().Get("append_to_response"); got != "combined_credits,external_ids" {
			t.Errorf("append_to_response = %q, want combined_credits,external_ids", got)
		}
	})

	t.Run("an unknown id is 404, not a provider outage", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{status: http.StatusNotFound, body: `{"status_message":"not found"}`}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		_, err := c.PersonDetails(context.Background(), 999999999)
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("a 500 is a provider outage", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{status: http.StatusInternalServerError, body: "boom"}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		_, err := c.PersonDetails(context.Background(), 287)
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want ErrProviderUnavailable", err)
		}
	})

	t.Run("a transport error never leaks the key", func(t *testing.T) {
		t.Parallel()
		stub := &stubDoer{err: errors.New(`Get "https://provider.example/3/person/287?api_key=v3key": dial tcp: timeout`)}
		c := NewTMDB(TMDBCredentials{APIKeyV3: "v3key"}, WithHTTPDoer(stub))

		_, err := c.PersonDetails(context.Background(), 287)
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want ErrProviderUnavailable", err)
		}
		if strings.Contains(err.Error(), "v3key") {
			t.Fatalf("the API key leaked into the error: %v", err)
		}
	})
}
