package upcoming

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// upcomingService pins today to FixedClock's 2026-08-18, so the window fixtures
// below stay on their side of the line forever.
func upcomingService(t *testing.T) *Service {
	t.Helper()
	return &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
}

func upcoming(t *testing.T, svc *Service, userID int64, p UpcomingParams) []UpcomingEntry {
	t.Helper()
	entries, err := svc.Upcoming(context.Background(), userID, p)
	if err != nil {
		t.Fatalf("Upcoming(%+v) error = %v", p, err)
	}
	return entries
}

// upcomingKeys renders each entry as "<date> <kind> <title>[ SxEy]" — every
// field an ordering or membership assertion turns on, in one comparable string.
func upcomingKeys(entries []UpcomingEntry) []string {
	keys := make([]string, len(entries))
	for i, e := range entries {
		switch {
		case e.Episode != nil:
			keys[i] = fmt.Sprintf("%s episode %s S%dE%d", e.Date, e.Show.Name, e.Episode.Season, e.Episode.Number)
		case e.Movie != nil:
			keys[i] = fmt.Sprintf("%s movie %s", e.Date, e.Movie.Title)
		default:
			keys[i] = e.Date + " " + e.Kind + " <empty union>"
		}
	}
	return keys
}

func wantKeys(t *testing.T, entries []UpcomingEntry, want ...string) {
	t.Helper()
	got := upcomingKeys(entries)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

// followedShow seeds a show the user has started: one aired, watched episode
// gives it the non-dropped state row that "followed" means (D-010).
func followedShow(t *testing.T, svc *Service, userID, tmdbID int64, title string) int64 {
	t.Helper()
	show := videotest.SeedShow(t, svc.DB, tmdbID, title)
	e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
	videotest.SeedWatch(t, svc.DB, userID, "episode", e1, "2026-08-01T20:00:00Z")
	videotest.SeedShowStatus(t, svc.DB, userID, show, "watching")
	return show
}

func TestUpcomingEpisodes(t *testing.T) {
	t.Parallel()

	t.Run("followed show lists its dated future episodes in calendar order", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := followedShow(t, svc, user, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, videotest.Ptr("2026-08-25"))
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-19"))
		// Yesterday, undated, and a special: none of them is a calendar entry.
		videotest.SeedEpisode(t, svc.DB, show, 1, 4, videotest.Ptr("2026-08-17"))
		videotest.SeedEpisode(t, svc.DB, show, 1, 5, nil)
		videotest.SeedEpisode(t, svc.DB, show, 0, 1, videotest.Ptr("2026-08-20"))

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}),
			"2026-08-19 episode Alpha S1E2",
			"2026-08-25 episode Alpha S1E3")
	})

	t.Run("today's episode is listed until it is watched", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := followedShow(t, svc, user, 100, "Alpha")
		today := videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-18"))

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}), "2026-08-18 episode Alpha S1E2")

		videotest.SeedWatch(t, svc.DB, user, "episode", today, "2026-08-18T21:00:00Z")
		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}))
	})

	t.Run("a watchlisted show the user never started is included", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 200, "Beta")
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-09-01"))
		videotest.SeedWatchlist(t, svc.DB, user, "show", show, "2026-08-01T00:00:00Z")

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}), "2026-09-01 episode Beta S1E1")
	})

	t.Run("a show both followed and watchlisted appears once per episode", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := followedShow(t, svc, user, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-20"))
		videotest.SeedWatchlist(t, svc.DB, user, "show", show, "2026-08-01T00:00:00Z")

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}), "2026-08-20 episode Alpha S1E2")
	})

	t.Run("a dropped show is excluded even when watchlisted", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-20"))
		videotest.SeedShowStatus(t, svc.DB, user, show, "dropped")
		videotest.SeedWatchlist(t, svc.DB, user, "show", show, "2026-08-01T00:00:00Z")

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}))
	})

	t.Run("an unfollowed, unlisted show is absent", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-08-20"))

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}))
	})

	t.Run("same-day entries order by title then show id, movies after episodes", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		// Two shows sharing a title: the id tie-break keeps each show's run
		// contiguous, which the SPA's collapse relies on.
		twinA := followedShow(t, svc, user, 100, "Twin")
		twinB := followedShow(t, svc, user, 101, "Twin")
		videotest.SeedEpisode(t, svc.DB, twinB, 2, 1, videotest.Ptr("2026-08-20"))
		videotest.SeedEpisode(t, svc.DB, twinA, 1, 2, videotest.Ptr("2026-08-20"))
		videotest.SeedEpisode(t, svc.DB, twinB, 2, 2, videotest.Ptr("2026-08-20"))
		videotest.SeedEpisode(t, svc.DB, twinA, 1, 3, videotest.Ptr("2026-08-20"))
		zed := followedShow(t, svc, user, 102, "zed")
		videotest.SeedEpisode(t, svc.DB, zed, 1, 2, videotest.Ptr("2026-08-20"))
		movie := videotest.SeedMovie(t, svc.DB, 900, "twin", videotest.Ptr("2026-08-20"), nil, `{}`)
		videotest.SeedWatchlist(t, svc.DB, user, "movie", movie.ID, "2026-08-01T00:00:00Z")

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}),
			"2026-08-20 episode Twin S1E2",
			"2026-08-20 episode Twin S1E3",
			"2026-08-20 episode Twin S2E1",
			"2026-08-20 episode Twin S2E2",
			"2026-08-20 movie twin",
			"2026-08-20 episode zed S1E2")
	})

	t.Run("marker, poster fallback and season id come from the season row", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShowWithPoster(t, svc.DB, 100, "Alpha", videotest.Ptr("/show.jpg"))
		videotest.SeedShowStatus(t, svc.DB, user, show, "watching")
		seasonID := videotest.SeedSeasonSnapshot(t, svc.DB, show, 100, 1, nil,
			`{"episodes": [{"episode_number": 2, "episode_type": "finale"}]}`)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-20"))

		entries := upcoming(t, svc, user, UpcomingParams{})
		if len(entries) != 1 {
			t.Fatalf("len = %d, want 1", len(entries))
		}
		e := entries[0]
		if e.Movie != nil || e.Show == nil || e.Episode == nil || e.Kind != "episode" {
			t.Fatalf("union = %+v, want the episode arm only", e)
		}
		if e.Episode.Marker == nil || *e.Episode.Marker != "season_finale" {
			t.Errorf("marker = %v, want season_finale", e.Episode.Marker)
		}
		if e.Episode.SeasonID == nil || *e.Episode.SeasonID != seasonID {
			t.Errorf("season_id = %v, want %d", e.Episode.SeasonID, seasonID)
		}
		// The season has no poster, so the show's is served — through the
		// proxy, since nothing is on disk.
		if e.Poster == nil || *e.Poster != "api/v1/video/posters/w185/show.jpg" {
			t.Errorf("poster = %v, want the show poster proxy URL", e.Poster)
		}
		// The full variant follows the same fallback as the thumb.
		if e.PosterFull == nil || *e.PosterFull != "api/v1/video/posters/w500/show.jpg" {
			t.Errorf("poster_full = %v, want the show poster's w500 proxy URL", e.PosterFull)
		}
	})

	t.Run("a season poster wins over the show's at both sizes", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShowWithPoster(t, svc.DB, 100, "Alpha", videotest.Ptr("/show.jpg"))
		videotest.SeedShowStatus(t, svc.DB, user, show, "watching")
		videotest.SeedSeasonSnapshot(t, svc.DB, show, 100, 1, videotest.Ptr("/s1.jpg"), `{"episodes": []}`)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-20"))

		entries := upcoming(t, svc, user, UpcomingParams{})
		if len(entries) != 1 {
			t.Fatalf("len = %d, want 1", len(entries))
		}
		e := entries[0]
		if e.Poster == nil || *e.Poster != "api/v1/video/posters/w185/s1.jpg" ||
			e.PosterFull == nil || *e.PosterFull != "api/v1/video/posters/w500/s1.jpg" {
			t.Errorf("poster pair = (%v, %v), want the season poster at w185 and w500", e.Poster, e.PosterFull)
		}
	})
}

func TestUpcomingMovies(t *testing.T) {
	t.Parallel()

	t.Run("watchlisted unreleased movie is listed; released, watched or unlisted are not", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		soon := videotest.SeedMovie(t, svc.DB, 900, "Soon", videotest.Ptr("2026-09-10"), videotest.Ptr("/soon.jpg"), `{}`)
		released := videotest.SeedMovie(t, svc.DB, 901, "Released", videotest.Ptr("2026-08-17"), nil, `{}`)
		premiere := videotest.SeedMovie(t, svc.DB, 902, "Premiere", videotest.Ptr("2026-08-30"), nil, `{}`)
		videotest.SeedMovie(t, svc.DB, 903, "Unlisted", videotest.Ptr("2026-08-30"), nil, `{}`)
		undated := videotest.SeedMovie(t, svc.DB, 904, "Undated", nil, nil, `{}`)
		for _, m := range []common.MovieRow{soon, released, premiere, undated} {
			videotest.SeedWatchlist(t, svc.DB, user, "movie", m.ID, "2026-08-01T00:00:00Z")
		}
		// Seen at a premiere before the public release: still a watch.
		videotest.SeedWatch(t, svc.DB, user, "movie", premiere.ID, "2026-08-10T20:00:00Z")

		entries := upcoming(t, svc, user, UpcomingParams{})
		wantKeys(t, entries, "2026-09-10 movie Soon")
		e := entries[0]
		if e.Show != nil || e.Episode != nil || e.Kind != "movie" {
			t.Fatalf("union = %+v, want the movie arm only", e)
		}
		if e.Poster == nil || *e.Poster != "api/v1/video/posters/w185/soon.jpg" {
			t.Errorf("poster = %v, want the movie poster proxy URL", e.Poster)
		}
		if e.PosterFull == nil || *e.PosterFull != "api/v1/video/posters/w500/soon.jpg" {
			t.Errorf("poster_full = %v, want the movie poster's w500 proxy URL", e.PosterFull)
		}
	})

	t.Run("a movie watch never makes the same-numbered episode look watched", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := followedShow(t, svc, user, 100, "Alpha")
		ep := videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-20"))
		// A movie watch on the episode's id (D-013): the target_type predicate
		// is what keeps the episode listed.
		videotest.SeedWatch(t, svc.DB, user, "movie", ep, "2026-08-10T20:00:00Z")

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}), "2026-08-20 episode Alpha S1E2")
	})
}

func TestUpcomingWindow(t *testing.T) {
	t.Parallel()

	t.Run("the horizon is from inclusive to from plus days exclusive", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := followedShow(t, svc, user, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-18")) // today
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, videotest.Ptr("2026-08-31")) // today + 13
		videotest.SeedEpisode(t, svc.DB, show, 1, 4, videotest.Ptr("2026-09-01")) // today + 14

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{Days: "14"}),
			"2026-08-18 episode Alpha S1E2",
			"2026-08-31 episode Alpha S1E3")
	})

	t.Run("the default horizon is 90 days", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := followedShow(t, svc, user, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-11-15")) // today + 89
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, videotest.Ptr("2026-11-16")) // today + 90

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{}), "2026-11-15 episode Alpha S1E2")
	})

	t.Run("from moves the window to the client's day", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := followedShow(t, svc, user, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-17"))
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, videotest.Ptr("2026-08-18"))

		// A viewer whose local day is still the 17th sees the 17th's episode;
		// one whose day is already the 19th sees neither.
		wantKeys(t, upcoming(t, svc, user, UpcomingParams{From: "2026-08-17"}),
			"2026-08-17 episode Alpha S1E2",
			"2026-08-18 episode Alpha S1E3")
		wantKeys(t, upcoming(t, svc, user, UpcomingParams{From: "2026-08-19"}))
	})

	t.Run("type narrows to one arm", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := followedShow(t, svc, user, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-20"))
		movie := videotest.SeedMovie(t, svc.DB, 900, "Soon", videotest.Ptr("2026-08-21"), nil, `{}`)
		videotest.SeedWatchlist(t, svc.DB, user, "movie", movie.ID, "2026-08-01T00:00:00Z")

		wantKeys(t, upcoming(t, svc, user, UpcomingParams{Type: "episode"}), "2026-08-20 episode Alpha S1E2")
		wantKeys(t, upcoming(t, svc, user, UpcomingParams{Type: "movie"}), "2026-08-21 movie Soon")
		wantKeys(t, upcoming(t, svc, user, UpcomingParams{Type: "all"}),
			"2026-08-20 episode Alpha S1E2",
			"2026-08-21 movie Soon")
	})

	t.Run("another user's follows and watchlist never leak", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		a := videotest.SeedUser(t, svc.DB, "a@example.com")
		b := videotest.SeedUser(t, svc.DB, "b@example.com")
		show := followedShow(t, svc, a, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-20"))
		movie := videotest.SeedMovie(t, svc.DB, 900, "Soon", videotest.Ptr("2026-08-21"), nil, `{}`)
		videotest.SeedWatchlist(t, svc.DB, a, "movie", movie.ID, "2026-08-01T00:00:00Z")

		if got := upcoming(t, svc, b, UpcomingParams{}); len(got) != 0 {
			t.Fatalf("user b sees %v, want nothing", upcomingKeys(got))
		}
	})

	t.Run("an empty result is an empty slice, never nil", func(t *testing.T) {
		t.Parallel()
		svc := upcomingService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		if got := upcoming(t, svc, user, UpcomingParams{}); got == nil || len(got) != 0 {
			t.Fatalf("entries = %#v, want an empty non-nil slice", got)
		}
	})
}

func TestUpcomingValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		p    UpcomingParams
	}{
		{"unknown type", UpcomingParams{Type: "season"}},
		{"days zero", UpcomingParams{Days: "0"}},
		{"days over a year", UpcomingParams{Days: "367"}},
		{"days not a number", UpcomingParams{Days: "soon"}},
		{"from not a date", UpcomingParams{From: "2026-13-01"}},
		{"from with a time", UpcomingParams{From: "2026-08-18T00:00:00Z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// No DB: validation must reject before anything could reach the store.
			_, err := testService().Upcoming(context.Background(), 1, tc.p)
			if !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}
