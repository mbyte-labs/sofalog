package stats

import (
	"fmt"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestStatsRatingsSummary(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	m := movie(t, svc, 1, "Alpha", nil)
	show := videotest.SeedShow(t, svc.DB, 10, "Show")
	e := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
	seedRatingAt(t, svc.DB, user, "movie", m, 8, "2026-03-01T20:00:00Z")
	seedRatingAt(t, svc.DB, user, "show", show, 6, "2026-03-02T20:00:00Z")
	seedRatingAt(t, svc.DB, user, "episode", e, 10, "2026-03-03T20:00:00Z")

	got := statsFor(t, svc, user, "all").Ratings
	if got == nil || got.Count != 3 || got.Average != 8 {
		t.Fatalf("ratings = %+v", got)
	}
	if got.Distribution != [10]int{0, 0, 0, 0, 0, 1, 0, 1, 0, 1} {
		t.Fatalf("distribution = %v", got.Distribution)
	}
	if *got.MoviesAverage != 8 || *got.ShowsAverage != 6 || *got.EpisodesAverage != 10 {
		t.Fatalf("per type = %v %v %v", *got.MoviesAverage, *got.ShowsAverage, *got.EpisodesAverage)
	}
	if statsFor(t, svc, user, "2025").Ratings != nil {
		t.Fatal("a year without ratings must be null")
	}
}

func TestStatsTopRated(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	show := videotest.SeedShow(t, svc.DB, 10, "Show")
	e := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
	seedRatingAt(t, svc.DB, user, "episode", e, 10, "2026-03-03T20:00:00Z")
	seedRatingAt(t, svc.DB, user, "show", show, 9, "2026-03-01T20:00:00Z")
	for i := int64(1); i <= 11; i++ {
		m := movie(t, svc, i, fmt.Sprintf("M%02d", i), nil)
		seedRatingAt(t, svc.DB, user, "movie", m, 9, fmt.Sprintf("2026-03-%02dT20:00:00Z", i))
	}

	got := statsFor(t, svc, user, "all").TopRated
	if len(got) != 10 {
		t.Fatalf("len = %d, want the cap of 10", len(got))
	}
	// Rating 9 everywhere: most recently rated first; the show rated on the
	// 1st ties M01 on the instant and sorts before it by kind.
	if got[0].Title.Title != "M11" || got[9].Title.Title != "M02" {
		t.Fatalf("order = %v", titleKeysOf(got))
	}
	for _, r := range got {
		if r.Title.Kind == "show" {
			t.Fatalf("the show rated on the 1st must be pushed out by the cap: %v", titleKeysOf(got))
		}
	}
}

func titleKeysOf(rows []StatsTopRated) []string {
	titles := make([]StatsTitle, 0, len(rows))
	for _, r := range rows {
		titles = append(titles, r.Title)
	}
	return titleKeys(titles)
}

func TestStatsRatingTrend(t *testing.T) {
	t.Parallel()

	t.Run("recent against earlier, rounded before the verdict", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		// Earlier (before 2026-05-20T12:00Z): 6, 7, 7, 7 → 6.75.
		// Recent: 7, 7, 7 → 7.0. Delta 0.25 rounds to 0.3 → kinder.
		for i, r := range []int{6, 7, 7, 7} {
			m := movie(t, svc, int64(i+1), "E", nil)
			seedRatingAt(t, svc.DB, user, "movie", m, r, fmt.Sprintf("2026-02-%02dT20:00:00Z", i+1))
		}
		for i := 0; i < 3; i++ {
			m := movie(t, svc, int64(10+i), "R", nil)
			seedRatingAt(t, svc.DB, user, "movie", m, 7, fmt.Sprintf("2026-07-%02dT20:00:00Z", i+1))
		}
		show := videotest.SeedShow(t, svc.DB, 99, "Show")
		e := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
		seedRatingAt(t, svc.DB, user, "episode", e, 1, "2026-07-10T20:00:00Z") // episodes are left out

		got := statsFor(t, svc, user, "all").RatingTrend
		if got == nil {
			t.Fatal("trend null")
		}
		if got.Recent.Count != 3 || *got.Recent.Average != 7 || got.Earlier.Count != 4 || *got.Earlier.Average != 6.75 {
			t.Fatalf("recent = %+v, earlier = %+v", got.Recent, got.Earlier)
		}
		if got.Delta == nil || *got.Delta != 0.3 || got.Verdict == nil || *got.Verdict != "kinder" {
			t.Fatalf("delta = %v, verdict = %v", got.Delta, got.Verdict)
		}
		if got.ReferenceAt != "2026-08-18T12:00:00Z" {
			t.Fatalf("reference = %q", got.ReferenceAt)
		}
		if len(got.Months) != 12 || got.Months[5].Month != "2026-02" || got.Months[5].Count != 4 || *got.Months[5].Average != 6.75 || got.Months[0].Average != nil {
			t.Fatalf("months = %+v", got.Months)
		}
	})

	t.Run("under three a side there is no delta", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		for i := 0; i < 3; i++ {
			m := movie(t, svc, int64(i+1), "E", nil)
			seedRatingAt(t, svc.DB, user, "movie", m, 5, fmt.Sprintf("2026-02-%02dT20:00:00Z", i+1))
		}
		for i := 0; i < 2; i++ {
			m := movie(t, svc, int64(10+i), "R", nil)
			seedRatingAt(t, svc.DB, user, "movie", m, 9, fmt.Sprintf("2026-07-%02dT20:00:00Z", i+1))
		}
		got := statsFor(t, svc, user, "all").RatingTrend
		if got.Delta != nil || got.Verdict != nil {
			t.Fatalf("delta = %v, verdict = %v, want null", got.Delta, got.Verdict)
		}
	})

	t.Run("a past year's window ends with the year", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		// 90 days before 2026-01-01 is 2025-10-03: a rating on the 2nd is earlier.
		for i, at := range []string{"2025-10-02T20:00:00Z", "2025-10-03T00:00:00Z", "2025-12-31T23:59:59Z"} {
			m := movie(t, svc, int64(i+1), "M", nil)
			seedRatingAt(t, svc.DB, user, "movie", m, 7, at)
		}
		got := statsFor(t, svc, user, "2025").RatingTrend
		if got.ReferenceAt != "2026-01-01T00:00:00Z" || got.Recent.Count != 2 || got.Earlier.Count != 1 {
			t.Fatalf("reference = %q, recent = %d, earlier = %d", got.ReferenceAt, got.Recent.Count, got.Earlier.Count)
		}
	})

	t.Run("verdicts", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			recent, earlier int
			want            string
		}{
			{8, 8, "steady"}, {9, 6, "kinder"}, {5, 8, "harsher"},
		}
		for _, tc := range cases {
			t.Run(tc.want, func(t *testing.T) {
				t.Parallel()
				svc := statsService(t)
				user := videotest.SeedUser(t, svc.DB, "a@example.com")
				for i := 0; i < 3; i++ {
					m := movie(t, svc, int64(i+1), "E", nil)
					seedRatingAt(t, svc.DB, user, "movie", m, tc.earlier, fmt.Sprintf("2026-02-%02dT20:00:00Z", i+1))
					r := movie(t, svc, int64(10+i), "R", nil)
					seedRatingAt(t, svc.DB, user, "movie", r, tc.recent, fmt.Sprintf("2026-07-%02dT20:00:00Z", i+1))
				}
				got := statsFor(t, svc, user, "all").RatingTrend
				if got.Verdict == nil || *got.Verdict != tc.want {
					t.Fatalf("verdict = %v, want %s", got.Verdict, tc.want)
				}
			})
		}
	})
}

func TestStatsCrowd(t *testing.T) {
	t.Parallel()

	rated := func(t *testing.T, svc *Service, user, tmdbID int64, title, body string, rating int) int64 {
		t.Helper()
		m := videotest.SeedMovie(t, svc.DB, tmdbID, title, nil, nil, body).ID
		seedRatingAt(t, svc.DB, user, "movie", m, rating, "2026-03-01T20:00:00Z")
		return m
	}

	t.Run("compared titles, averages, verdict and lists", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		for i := int64(1); i <= 4; i++ {
			rated(t, svc, user, i, fmt.Sprintf("Up%d", i), `{"vote_average": 7.0, "vote_count": 100}`, 8)
		}
		rated(t, svc, user, 5, "Down", `{"vote_average": 8.0, "vote_count": 500}`, 6)
		rated(t, svc, user, 6, "Match", `{"vote_average": 7.0, "vote_count": 100}`, 7)
		rated(t, svc, user, 7, "Few votes", `{"vote_average": 1.0, "vote_count": 10}`, 10)
		rated(t, svc, user, 8, "No score", `{}`, 10)
		show := videotest.SeedShow(t, svc.DB, 10, "Show")
		seedRatingAt(t, svc.DB, user, "show", show, 2, "2026-03-01T20:00:00Z") // body without votes

		got := statsFor(t, svc, user, "all").Crowd
		if got == nil {
			t.Fatal("crowd null")
		}
		// Compared: 4×8 + 6 + 7 = 45 / 6 = 7.5; crowd: 4×7 + 8 + 7 = 43 / 6 ≈ 7.1667
		if got.Compared != 6 || got.TotalRated != 9 || got.UserAverage != 7.5 {
			t.Fatalf("compared = %d, total = %d, user avg = %v", got.Compared, got.TotalRated, got.UserAverage)
		}
		if got.Verdict != "in_sync" || got.Delta < 0.33 || got.Delta > 0.34 {
			t.Fatalf("verdict = %s, delta = %v", got.Verdict, got.Delta)
		}
		if len(got.LovedMore) != 3 || len(got.LovedLess) != 1 || got.LovedLess[0].Title.Title != "Down" || got.LovedLess[0].Gap != -2 {
			t.Fatalf("loved more = %d, loved less = %+v", len(got.LovedMore), got.LovedLess)
		}
		if got.LovedMore[0].Title.Title != "Up1" || got.LovedMore[0].TMDBVotes != 100 || got.LovedMore[0].UserRating != 8 {
			t.Fatalf("loved more = %+v", got.LovedMore)
		}
	})

	t.Run("fewer than five compared is null", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		for i := int64(1); i <= 4; i++ {
			rated(t, svc, user, i, "M", `{"vote_average": 7.0, "vote_count": 100}`, 8)
		}
		rated(t, svc, user, 5, "Thin", `{"vote_average": 7.0, "vote_count": 49}`, 8)
		if statsFor(t, svc, user, "all").Crowd != nil {
			t.Fatal("4 compared must be null")
		}
	})

	t.Run("thresholds", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			rating int
			want   string
		}{{8, "kinder"}, {7, "in_sync"}, {6, "harsher"}}
		for _, tc := range cases {
			t.Run(tc.want, func(t *testing.T) {
				t.Parallel()
				svc := statsService(t)
				user := videotest.SeedUser(t, svc.DB, "a@example.com")
				for i := int64(1); i <= 5; i++ {
					rated(t, svc, user, i, "M", `{"vote_average": 7.2, "vote_count": 100}`, tc.rating)
				}
				if got := statsFor(t, svc, user, "all").Crowd; got == nil || got.Verdict != tc.want {
					t.Fatalf("verdict = %+v, want %s", got, tc.want)
				}
			})
		}
	})
}
