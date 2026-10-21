package stats

import (
	"context"
	"errors"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

func TestStatsValidation(t *testing.T) {
	t.Parallel()
	cases := []string{"2026-01", "abc", "20261", "-1", " 2026", "ALL", "all ", "202"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			_, err := testService().Stats(context.Background(), 1, StatsParams{Period: raw})
			if !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("period %q: err = %v, want ErrValidation", raw, err)
			}
		})
	}
}

func TestStatsPeriodDefault(t *testing.T) {
	t.Parallel()

	t.Run("current year with a play", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m := movie(t, svc, 1, "Alpha", nil)
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-03-01T20:00:00Z")
		if got := statsFor(t, svc, user, "").Period; got != "2026" {
			t.Fatalf("period = %q, want 2026", got)
		}
	})

	t.Run("plays only in an earlier year fall back to all", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m := movie(t, svc, 1, "Alpha", nil)
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2025-03-01T20:00:00Z")
		if got := statsFor(t, svc, user, "").Period; got != "all" {
			t.Fatalf("period = %q, want all", got)
		}
	})

	t.Run("ratings alone never pick the year", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m := movie(t, svc, 1, "Alpha", nil)
		seedRatingAt(t, svc.DB, user, "movie", m, 7, "2026-03-01T20:00:00Z")
		if got := statsFor(t, svc, user, "").Period; got != "all" {
			t.Fatalf("period = %q, want all", got)
		}
	})

	t.Run("an explicit period is echoed", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		if got := statsFor(t, svc, user, "all").Period; got != "all" {
			t.Fatalf("period = %q, want all", got)
		}
		if got := statsFor(t, svc, user, "1999").Period; got != "1999" {
			t.Fatalf("period = %q, want 1999", got)
		}
	})
}

func TestStatsYears(t *testing.T) {
	t.Parallel()

	t.Run("from the earlier of first play and first rating", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m := movie(t, svc, 1, "Alpha", nil)
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2023-06-01T20:00:00Z")
		seedRatingAt(t, svc.DB, user, "movie", m, 7, "2021-02-01T20:00:00Z")
		got := statsFor(t, svc, user, "all").Years
		if !sameInts(got, []int{2026, 2025, 2024, 2023, 2022, 2021}) {
			t.Fatalf("years = %v", got)
		}
	})

	t.Run("nothing recorded offers an empty list", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		got := statsFor(t, svc, user, "all").Years
		if got == nil || len(got) != 0 {
			t.Fatalf("years = %#v, want []", got)
		}
	})

	t.Run("a year outside the list is computed and empty", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m := movie(t, svc, 1, "Alpha", nil)
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-03-01T20:00:00Z")
		got := statsFor(t, svc, user, "1999")
		if got.Headline.Plays != 0 || got.Ratings != nil || got.Completion != nil || len(got.TopRated) != 0 {
			t.Fatalf("1999 should be empty: %+v", got.Headline)
		}
	})
}

func TestStatsEmptyShape(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	got := statsFor(t, svc, user, "all")
	if got.Today != "2026-08-18" {
		t.Fatalf("today = %q", got.Today)
	}
	if got.Headline != (StatsHeadline{}) {
		t.Fatalf("headline = %+v, want zeros", got.Headline)
	}
	for name, isNil := range map[string]bool{
		"most_watched_show": got.MostWatchedShow == nil,
		"ratings":           got.Ratings == nil,
		"rating_trend":      got.RatingTrend == nil,
		"crowd":             got.Crowd == nil,
		"unrated_movies":    got.UnratedMovies == nil,
		"decades":           got.Decades == nil,
		"completion":        got.Completion == nil,
		"genres":            got.Genres == nil,
		"time_of_day":       got.Activity.TimeOfDay == nil,
		"tracking_days":     got.Activity.TrackingDays == nil,
		"active_days":       got.Activity.ActiveDays == nil,
		"first_play":        got.Activity.FirstPlay == nil,
	} {
		if !isNil {
			t.Errorf("%s should be null", name)
		}
	}
	if got.TopRated == nil || got.MostRewatched == nil {
		t.Fatal("lists must be empty, never null")
	}
	if got.Activity.CurrentStreak == nil || *got.Activity.CurrentStreak != 0 {
		t.Fatalf("current streak = %v, want 0", got.Activity.CurrentStreak)
	}
	if len(got.Activity.Months) != 12 || got.Activity.Months[0].Month != "2025-09" || got.Activity.Months[11].Month != "2026-08" {
		t.Fatalf("months = %+v", got.Activity.Months)
	}
	if got.Activity.Heatmap.Start != "2025-08-19" || len(got.Activity.Heatmap.Plays) != 365 || len(got.Activity.Heatmap.Levels) != 365 {
		t.Fatalf("heatmap start = %q, len = %d", got.Activity.Heatmap.Start, len(got.Activity.Heatmap.Plays))
	}
}

// A play late on 31 December UTC is already New Year's in Bucharest: the year
// it lands in is the user's, and so is "today".
func TestStatsZone(t *testing.T) {
	t.Parallel()
	svc := statsServiceAt(t, videotest.LateEveningClock)
	ro := videotest.SeedUser(t, svc.DB, "ro@example.com")
	videotest.SetUserTimezone(t, svc.DB, ro, "Europe/Bucharest")
	utc := videotest.SeedUser(t, svc.DB, "utc@example.com")
	m := movie(t, svc, 1, "Alpha", nil)
	videotest.SeedWatch(t, svc.DB, ro, "movie", m, "2025-12-31T22:30:00Z")
	videotest.SeedWatch(t, svc.DB, utc, "movie", m, "2025-12-31T22:30:00Z")

	if got := statsFor(t, svc, ro, "2026"); got.Headline.Plays != 1 || got.Today != "2026-08-19" {
		t.Fatalf("Bucharest 2026: plays = %d, today = %q", got.Headline.Plays, got.Today)
	}
	if got := statsFor(t, svc, ro, "2025"); got.Headline.Plays != 0 {
		t.Fatalf("Bucharest 2025: plays = %d, want 0", got.Headline.Plays)
	}
	if got := statsFor(t, svc, utc, "2025"); got.Headline.Plays != 1 || got.Today != "2026-08-18" {
		t.Fatalf("UTC 2025: plays = %d, today = %q", got.Headline.Plays, got.Today)
	}
	if got := statsFor(t, svc, ro, ""); got.Period != "2026" {
		t.Fatalf("Bucharest default period = %q, want 2026", got.Period)
	}
	if got := statsFor(t, svc, utc, ""); got.Period != "all" {
		t.Fatalf("UTC default period = %q, want all", got.Period)
	}
}

func TestStatsHalfOpenYearBounds(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	m := movie(t, svc, 1, "Alpha", nil)
	videotest.SeedWatch(t, svc.DB, user, "movie", m, "2025-12-31T23:59:59Z")
	videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-01-01T00:00:00Z")
	if got := statsFor(t, svc, user, "2025").Headline.Plays; got != 1 {
		t.Fatalf("2025 plays = %d, want 1", got)
	}
	if got := statsFor(t, svc, user, "2026").Headline.Plays; got != 1 {
		t.Fatalf("2026 plays = %d, want 1", got)
	}
}

// Two users: everything A wrote is invisible to B.
func TestStatsIsolation(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	a := videotest.SeedUser(t, svc.DB, "a@example.com")
	b := videotest.SeedUser(t, svc.DB, "b@example.com")
	m := movie(t, svc, 1, "Alpha", videotest.Ptr("1999-03-30"))
	show := videotest.SeedShow(t, svc.DB, 10, "Beta")
	ep := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
	videotest.SeedWatch(t, svc.DB, a, "movie", m, "2026-03-01T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, a, "episode", ep, "2026-03-02T20:00:00Z")
	videotest.SeedShowStatus(t, svc.DB, a, show, "watching")
	seedRatingAt(t, svc.DB, a, "movie", m, 9, "2026-03-01T21:00:00Z")

	got := statsFor(t, svc, b, "all")
	if got.Headline.Plays != 0 || got.Ratings != nil || got.Completion != nil || got.Decades != nil ||
		got.MostWatchedShow != nil || len(got.TopRated) != 0 || len(got.Years) != 0 {
		t.Fatalf("user B sees A's state: %+v", got)
	}
	if statsFor(t, svc, a, "all").Headline.Plays != 2 {
		t.Fatal("user A's own plays missing")
	}
}
