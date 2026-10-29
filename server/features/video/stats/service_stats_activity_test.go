package stats

import (
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestStatsMonthsAndHeatmapWindows(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	m := movie(t, svc, 1, "Alpha", nil)
	videotest.SeedWatch(t, svc.DB, user, "movie", m, "2025-09-15T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-18T09:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "movie", m, "2024-02-29T09:00:00Z")

	all := statsFor(t, svc, user, "all").Activity
	if all.Months[0].Month != "2025-09" || all.Months[0].Plays != 1 || all.Months[11].Month != "2026-08" || all.Months[11].Plays != 1 {
		t.Fatalf("all months = %+v", all.Months)
	}
	if all.Heatmap.Start != "2025-08-19" || len(all.Heatmap.Plays) != 365 || all.Heatmap.Plays[364] != 1 {
		t.Fatalf("all heatmap start = %q, len = %d, last = %d", all.Heatmap.Start, len(all.Heatmap.Plays), all.Heatmap.Plays[364])
	}

	leap := statsFor(t, svc, user, "2024").Activity
	if leap.Months[0].Month != "2024-01" || leap.Months[11].Month != "2024-12" || leap.Months[1].Plays != 1 {
		t.Fatalf("2024 months = %+v", leap.Months)
	}
	if leap.Heatmap.Start != "2024-01-01" || len(leap.Heatmap.Plays) != 366 || leap.Heatmap.Plays[59] != 1 {
		t.Fatalf("2024 heatmap start = %q, len = %d", leap.Heatmap.Start, len(leap.Heatmap.Plays))
	}
	if leap.ActiveDays == nil || *leap.ActiveDays != 1 || all.ActiveDays != nil {
		t.Fatalf("active days: year = %v, all = %v", leap.ActiveDays, all.ActiveDays)
	}
}

func TestHeatLevels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		plays []int
		want  []int
	}{
		{"quartiles", []int{1, 1, 2, 3, 10, 0}, []int{1, 1, 2, 3, 4, 0}},
		{"all equal is the busiest", []int{2, 0, 2, 2}, []int{4, 0, 4, 4}},
		{"single day", []int{0, 7, 0}, []int{0, 4, 0}},
		{"nothing", []int{0, 0}, []int{0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := heatLevels(tc.plays); !sameInts(got, tc.want) {
				t.Fatalf("levels(%v) = %v, want %v", tc.plays, got, tc.want)
			}
		})
	}
}

func TestStatsWeekdayAndTimeOfDay(t *testing.T) {
	t.Parallel()

	t.Run("local weekday and part of day, exact midnight excluded", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m := movie(t, svc, 1, "Alpha", nil)
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-10T00:00:00Z") // Monday, bulk air-date stamp
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-10T00:00:01Z") // Monday, night
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-12T06:30:00Z") // Wednesday, morning
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-16T21:59:00Z") // Sunday, evening
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-16T22:00:00Z") // Sunday, night

		a := statsFor(t, svc, user, "all").Activity
		if a.Weekday != [7]int{2, 0, 1, 0, 0, 0, 2} {
			t.Fatalf("weekday = %v", a.Weekday)
		}
		want := StatsTimeOfDay{Morning: 1, Evening: 1, Night: 2, Dominant: "night"}
		if a.TimeOfDay == nil || *a.TimeOfDay != want {
			t.Fatalf("time of day = %+v, want %+v", a.TimeOfDay, want)
		}
	})

	t.Run("a tie goes to the earlier part", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m := movie(t, svc, 1, "Alpha", nil)
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-12T13:00:00Z")
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-12T23:00:00Z")
		if got := statsFor(t, svc, user, "all").Activity.TimeOfDay; got == nil || got.Dominant != "afternoon" {
			t.Fatalf("dominant = %+v, want afternoon", got)
		}
	})

	t.Run("midnight is the user's midnight", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		videotest.SetUserTimezone(t, svc.DB, user, "Europe/Bucharest")
		m := movie(t, svc, 1, "Alpha", nil)
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-09T21:00:00Z") // 00:00 on the 10th in Bucharest
		a := statsFor(t, svc, user, "all").Activity
		if a.TimeOfDay != nil {
			t.Fatalf("time of day = %+v, want null", a.TimeOfDay)
		}
		if a.Weekday != [7]int{1, 0, 0, 0, 0, 0, 0} {
			t.Fatalf("weekday = %v, want the play on Monday the 10th", a.Weekday)
		}
		if a.Heatmap.Plays[len(a.Heatmap.Plays)-9] != 1 { // the 10th, today being the 18th
			t.Fatalf("heatmap did not land on the user's day")
		}
	})
}

func TestStatsStreaks(t *testing.T) {
	t.Parallel()

	seed := func(t *testing.T, svc *Service, user int64, days ...string) {
		t.Helper()
		m := movie(t, svc, 1, "Alpha", nil)
		for _, d := range days {
			videotest.SeedWatch(t, svc.DB, user, "movie", m, d+"T20:00:00Z")
		}
	}

	t.Run("a run ending yesterday is current", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		seed(t, svc, user, "2026-08-01", "2026-08-15", "2026-08-16", "2026-08-17", "2026-08-17")
		a := statsFor(t, svc, user, "all").Activity
		if a.LongestStreak != 3 || a.CurrentStreak == nil || *a.CurrentStreak != 3 {
			t.Fatalf("longest = %d, current = %v", a.LongestStreak, a.CurrentStreak)
		}
	})

	t.Run("a run ending two days ago is over", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		seed(t, svc, user, "2026-08-14", "2026-08-15", "2026-08-16")
		a := statsFor(t, svc, user, "all").Activity
		if a.LongestStreak != 3 || a.CurrentStreak == nil || *a.CurrentStreak != 0 {
			t.Fatalf("longest = %d, current = %v", a.LongestStreak, a.CurrentStreak)
		}
	})

	t.Run("today counts and a past year has no current streak", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		seed(t, svc, user, "2026-08-18", "2025-12-30", "2025-12-31")
		a := statsFor(t, svc, user, "all").Activity
		if a.CurrentStreak == nil || *a.CurrentStreak != 1 {
			t.Fatalf("current = %v, want 1", a.CurrentStreak)
		}
		past := statsFor(t, svc, user, "2025").Activity
		if past.LongestStreak != 2 || past.CurrentStreak != nil {
			t.Fatalf("2025: longest = %d, current = %v", past.LongestStreak, past.CurrentStreak)
		}
	})
}

func TestStatsTrackingDaysAndFirstPlay(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	m := movie(t, svc, 1, "Alpha", nil)
	show := videotest.SeedShow(t, svc.DB, 10, "Show")
	e := videotest.SeedEpisode(t, svc.DB, show, 2, 3, videotest.Ptr("2026-01-01"))
	// Same instant: the lower id wins — the episode was inserted first.
	videotest.SeedWatch(t, svc.DB, user, "episode", e, "2026-08-08T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-08-08T20:00:00Z")

	a := statsFor(t, svc, user, "all").Activity
	if a.TrackingDays == nil || *a.TrackingDays != 10 {
		t.Fatalf("tracking days = %v, want 10", a.TrackingDays)
	}
	if a.FirstPlay == nil || a.FirstPlay.Title.Kind != "show" || a.FirstPlay.Title.Title != "Show" ||
		a.FirstPlay.Episode == nil || *a.FirstPlay.Episode != (StatsEpisodeRef{Season: 2, Number: 3}) ||
		a.FirstPlay.WatchedAt != "2026-08-08T20:00:00Z" {
		t.Fatalf("first play = %+v", a.FirstPlay)
	}
	if statsFor(t, svc, user, "2026").Activity.TrackingDays != nil {
		t.Fatal("a year has no tracking days")
	}

	today := statsServiceAt(t, func() time.Time { return time.Date(2026, 8, 8, 21, 0, 0, 0, time.UTC) })
	today.DB = svc.DB
	if got := statsFor(t, today, user, "all").Activity.TrackingDays; got == nil || *got != 1 {
		t.Fatalf("tracking days on the first day = %v, want 1", got)
	}
}
