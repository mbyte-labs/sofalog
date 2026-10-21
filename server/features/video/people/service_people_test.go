package people

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// pittBody is /person/287 with combined credits carrying every tripwire the
// projection has to get right:
//   - Fight Club (550) appears TWICE, once per character — one card, parts joined;
//   - the show 1399 appears twice with episode counts 3 and 5 — MAX (5), never
//     the sum (8);
//   - 2001 is undated — it sorts first, like TMDB's own page leads with
//     announced work;
//   - 777 and 778 share a date — the tie breaks on title;
//   - an id-less entry, a title-less entry and a "person" media type are dropped;
//   - 778 has no poster and a zero vote — both nulls.
const pittBody = `{
  "id": 287,
  "name": "Brad Pitt",
  "biography": "An American actor.",
  "known_for_department": "Acting",
  "birthday": "1963-12-18",
  "deathday": null,
  "place_of_birth": "Shawnee, Oklahoma, USA",
  "also_known_as": ["William Bradley Pitt", "  "],
  "homepage": null,
  "profile_path": "/bp.jpg",
  "external_ids": {"imdb_id": "nm0000093"},
  "combined_credits": {"cast": [
    {"id": 550, "media_type": "movie", "title": "Fight Club", "release_date": "1999-10-15",
     "character": "Tyler Durden", "poster_path": "/fc.jpg", "vote_average": 8.4},
    {"id": 550, "media_type": "movie", "title": "Fight Club", "release_date": "1999-10-15",
     "character": "Narrator's alter ego", "poster_path": "/fc.jpg", "vote_average": 8.4},
    {"id": 1399, "media_type": "tv", "name": "Friends", "first_air_date": "1994-09-22",
     "character": "Will Colbert", "episode_count": 3, "poster_path": "/fr.jpg", "vote_average": 8.4},
    {"id": 1399, "media_type": "tv", "name": "Friends", "first_air_date": "1994-09-22",
     "character": "Will Colbert", "episode_count": 5, "poster_path": "/fr.jpg", "vote_average": 8.4},
    {"id": 2001, "media_type": "tv", "name": "Untitled Project", "first_air_date": "",
     "character": "", "episode_count": 0, "poster_path": "", "vote_average": 0},
    {"id": 778, "media_type": "movie", "title": "Zebra Day", "release_date": "2005-06-01",
     "character": "Zed", "poster_path": "", "vote_average": 0},
    {"id": 777, "media_type": "movie", "title": "alpha Day", "release_date": "2005-06-01",
     "character": "Al", "poster_path": "/ad.jpg", "vote_average": 6.1},
    {"id": 0, "media_type": "movie", "title": "Ghost", "release_date": "2010-01-01"},
    {"id": 9, "media_type": "movie", "title": "", "release_date": "2010-01-01"},
    {"id": 287, "media_type": "person", "name": "Brad Pitt"}
  ]}
}`

func TestPersonDetails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("served live on every view, stores nothing and maps the DTO", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/person/287": pittBody}}
		svc, _ := detailsService(t, stub)

		d, err := svc.PersonDetails(ctx, 1, PersonDetailsParams{TMDBID: "287"})
		if err != nil {
			t.Fatalf("PersonDetails: %v", err)
		}
		if d.TMDBID != 287 || d.Name != "Brad Pitt" {
			t.Errorf("identity = %+v", d)
		}
		if d.Biography == nil || *d.Biography != "An American actor." ||
			d.KnownForDepartment == nil || *d.KnownForDepartment != "Acting" ||
			d.Birthday == nil || *d.Birthday != "1963-12-18" || d.Deathday != nil ||
			d.PlaceOfBirth == nil || *d.PlaceOfBirth != "Shawnee, Oklahoma, USA" ||
			d.Homepage != nil || d.IMDBID == nil || *d.IMDBID != "nm0000093" {
			t.Errorf("person fields = %+v", d)
		}
		// Blank aliases are dropped; the array is never null.
		if len(d.AlsoKnownAs) != 1 || d.AlsoKnownAs[0] != "William Bradley Pitt" {
			t.Errorf("also_known_as = %v", d.AlsoKnownAs)
		}
		// The portrait rides TMDB's PROFILE ladder: w185 thumb, h632 full.
		if d.Photo == nil || *d.Photo != "api/v1/video/posters/w185/bp.jpg" ||
			d.PhotoFull == nil || *d.PhotoFull != "api/v1/video/posters/h632/bp.jpg" {
			t.Errorf("photo = %v / %v", d.Photo, d.PhotoFull)
		}

		// Six credits survive: undated first, then newest first, ties on title.
		titles := make([]string, 0, len(d.Credits))
		for _, c := range d.Credits {
			titles = append(titles, c.Title)
		}
		want := []string{"Untitled Project", "alpha Day", "Zebra Day", "Fight Club", "Friends"}
		if strings.Join(titles, "|") != strings.Join(want, "|") {
			t.Fatalf("credit order = %v, want %v", titles, want)
		}

		fc := d.Credits[3]
		if fc.MediaType != "movie" || fc.TMDBID != 550 || fc.Year == nil || *fc.Year != "1999" {
			t.Errorf("Fight Club = %+v", fc)
		}
		if fc.Character == nil || *fc.Character != "Tyler Durden / Narrator's alter ego" {
			t.Errorf("Fight Club character = %v, want both parts joined", fc.Character)
		}
		if fc.Episodes != nil || fc.Rating == nil || *fc.Rating != 8.4 {
			t.Errorf("Fight Club episodes/rating = %v/%v", fc.Episodes, fc.Rating)
		}
		if fc.Poster == nil || *fc.Poster != "api/v1/video/posters/w185/fc.jpg" ||
			fc.PosterFull == nil || *fc.PosterFull != "api/v1/video/posters/w500/fc.jpg" {
			t.Errorf("Fight Club poster = %v / %v", fc.Poster, fc.PosterFull)
		}

		fr := d.Credits[4]
		if fr.MediaType != "show" || fr.TMDBID != 1399 || fr.Episodes == nil || *fr.Episodes != 5 {
			t.Errorf("Friends = %+v, want media_type show and episodes 5 (max, not 8)", fr)
		}
		if fr.Character == nil || *fr.Character != "Will Colbert" {
			t.Errorf("Friends character = %v, want the one part once", fr.Character)
		}

		up := d.Credits[0]
		if up.Year != nil || up.Character != nil || up.Episodes != nil || up.Poster != nil || up.Rating != nil {
			t.Errorf("undated credit = %+v, want every optional field null", up)
		}
		zd := d.Credits[2]
		if zd.Poster != nil || zd.PosterFull != nil || zd.Rating != nil {
			t.Errorf("poster-less unrated credit = %+v", zd)
		}

		// Nothing in the library: no ids, nothing watched.
		for _, c := range d.Credits {
			if c.ID != nil || c.Watched {
				t.Errorf("%s: library flags = %v/%v on an empty library", c.Title, c.ID, c.Watched)
			}
		}

		// Live each time, and never a row.
		if _, err := svc.PersonDetails(ctx, 1, PersonDetailsParams{TMDBID: "287"}); err != nil {
			t.Fatalf("second view: %v", err)
		}
		if n := stub.CallCount("/3/person/287"); n != 2 {
			t.Errorf("TMDB called %d times, want 2 (live each time)", n)
		}
		if _, err := common.GetMovieByTMDBID(ctx, svc.DB, 550); !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("a person view materialised a movies row: err = %v", err)
		}
	})

	t.Run("credits the library holds carry its id and THIS user's watched flag", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/person/287": pittBody}}
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
		untitledEp := videotest.SeedEpisode(t, svc.DB, untitled, 1, 1, nil)
		videotest.SeedWatch(t, svc.DB, alice, "episode", untitledEp, "2026-01-03T20:00:00Z")
		// A movie watch whose id collides with that episode's must not make
		// anything look started: the join carries target_type.
		if untitledEp != movie.ID {
			videotest.SeedWatch(t, svc.DB, bob, "movie", untitledEp, "2026-01-04T20:00:00Z")
		}

		byTitle := func(d PersonDetails, title string) PersonCredit {
			for _, c := range d.Credits {
				if c.Title == title {
					return c
				}
			}
			t.Fatalf("no credit titled %q in %+v", title, d.Credits)
			return PersonCredit{}
		}

		a, err := svc.PersonDetails(ctx, alice, PersonDetailsParams{TMDBID: "287"})
		if err != nil {
			t.Fatalf("alice: %v", err)
		}
		if c := byTitle(a, "Fight Club"); c.ID == nil || *c.ID != movie.ID || !c.Watched || !c.InHistory {
			t.Errorf("alice's Fight Club = %+v, want the local id, watched and in history", c)
		}
		if c := byTitle(a, "Friends"); c.ID == nil || *c.ID != friends || !c.Watched || !c.InHistory {
			t.Errorf("alice's Friends = %+v, want the local id, watched (completed) and in history", c)
		}
		// In the library and started is not watched: a show's ✓ means finished.
		// But it IS in the history — one episode watch is enough for that.
		if c := byTitle(a, "Untitled Project"); c.ID == nil || *c.ID != untitled || c.Watched || !c.InHistory {
			t.Errorf("alice's Untitled Project = %+v, want the local id, NOT watched, in history", c)
		}
		// Held but never watched: in the library, in neither sense seen.
		if c := byTitle(a, "alpha Day"); c.ID == nil || c.Watched || c.InHistory {
			t.Errorf("alice's alpha Day = %+v, want the local id and no watch flags", c)
		}
		if c := byTitle(a, "Zebra Day"); c.ID != nil || c.Watched || c.InHistory {
			t.Errorf("alice's Zebra Day = %+v, want no library flags", c)
		}

		// The catalog is shared, the flags are not.
		b, err := svc.PersonDetails(ctx, bob, PersonDetailsParams{TMDBID: "287"})
		if err != nil {
			t.Fatalf("bob: %v", err)
		}
		if c := byTitle(b, "Fight Club"); c.ID == nil || *c.ID != movie.ID || c.Watched || c.InHistory {
			t.Errorf("bob's Fight Club = %+v, want the local id but NOT watched", c)
		}
		if c := byTitle(b, "Friends"); c.ID == nil || c.Watched || c.InHistory {
			t.Errorf("bob's Friends = %+v, want the local id but NOT watched", c)
		}
		if c := byTitle(b, "Untitled Project"); c.ID == nil || c.Watched || c.InHistory {
			t.Errorf("bob's Untitled Project = %+v, want no watch flags — his movie watch shares only the id", c)
		}
	})

	t.Run("an unknown person is a 404", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Statuses: map[string]int{"/3/person/1": 404}}
		svc, _ := detailsService(t, stub)
		if _, err := svc.PersonDetails(ctx, 1, PersonDetailsParams{TMDBID: "1"}); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("TMDB down is a 503 on EVERY view, and the key never leaks", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{
			"/3/person/287": errors.New(`Get "https://provider.example/3/person/287?api_key=test-key": dial tcp: connection refused`),
		}}
		svc, _ := detailsService(t, stub)
		_, err := svc.PersonDetails(ctx, 1, PersonDetailsParams{TMDBID: "287"})
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want ErrProviderUnavailable", err)
		}
		if strings.Contains(err.Error(), "test-key") {
			t.Fatalf("the API key leaked into the error: %v", err)
		}
	})

	t.Run("a bad tmdb_id is a 422 before any provider call", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/person/287": pittBody}}
		svc, _ := detailsService(t, stub)
		for _, bad := range []string{"", "abc", "0", "-1", "1.5"} {
			_, err := svc.PersonDetails(ctx, 1, PersonDetailsParams{TMDBID: bad})
			if !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("tmdb_id=%q: err = %v, want ErrValidation", bad, err)
			}
		}
		if n := stub.CallCount("/3/person/287"); n != 0 {
			t.Errorf("TMDB called %d times on invalid input, want 0", n)
		}
	})
}
