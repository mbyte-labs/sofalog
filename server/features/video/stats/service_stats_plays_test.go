package stats

import (
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestStatsHeadline(t *testing.T) {
	t.Parallel()

	t.Run("minutes from the movie, the episode, or the show's regular runtime", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m := movie(t, svc, 1, "Alpha", nil)
		setMovieFacts(t, svc.DB, m, videotest.Ptr(100), nil)
		unknown := movie(t, svc, 2, "Beta", nil)
		show := videotest.SeedShow(t, svc.DB, 10, "Gamma")
		setShowFacts(t, svc.DB, show, nil, nil, videotest.Ptr(`{"episode_run_time": [45]}`))
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
		setEpisodeRuntime(t, svc.DB, e1, videotest.Ptr(60))
		e2 := videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-01-02"))
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-03-01T20:00:00Z")
		videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-03-05T20:00:00Z") // rewatch
		videotest.SeedWatch(t, svc.DB, user, "movie", unknown, "2026-03-02T20:00:00Z")
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-03-03T20:00:00Z")
		videotest.SeedWatch(t, svc.DB, user, "episode", e2, "2026-03-04T20:00:00Z")
		videotest.SeedWatch(t, svc.DB, user, "episode", e2, "2026-03-06T20:00:00Z") // rewatch

		h := statsFor(t, svc, user, "all").Headline
		want := StatsHeadline{Hours: 5, Plays: 6, Movies: 2, Episodes: 2, MovieMinutes: 200, EpisodeMinutes: 150}
		if h != want {
			t.Fatalf("headline = %+v, want %+v", h, want)
		}
	})

	t.Run("no runtime anywhere is zero hours, not a failure", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 10, "Gamma")
		e := videotest.SeedEpisode(t, svc.DB, show, 1, 1, nil)
		videotest.SeedWatch(t, svc.DB, user, "episode", e, "2026-03-03T20:00:00Z")
		h := statsFor(t, svc, user, "all").Headline
		if h.Hours != 0 || h.EpisodeMinutes != 0 || h.Episodes != 1 {
			t.Fatalf("headline = %+v", h)
		}
	})
}

func TestStatsMostWatchedShow(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	long := videotest.SeedShow(t, svc.DB, 10, "Long")
	le := videotest.SeedEpisode(t, svc.DB, long, 1, 1, videotest.Ptr("2026-01-01"))
	setEpisodeRuntime(t, svc.DB, le, videotest.Ptr(90))
	many := videotest.SeedShow(t, svc.DB, 11, "Many")
	me1 := videotest.SeedEpisode(t, svc.DB, many, 1, 1, videotest.Ptr("2026-01-01"))
	me2 := videotest.SeedEpisode(t, svc.DB, many, 1, 2, videotest.Ptr("2026-01-02"))
	setEpisodeRuntime(t, svc.DB, me1, videotest.Ptr(20))
	setEpisodeRuntime(t, svc.DB, me2, videotest.Ptr(20))
	videotest.SeedWatch(t, svc.DB, user, "episode", le, "2026-03-01T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "episode", me1, "2026-03-02T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "episode", me2, "2026-03-03T20:00:00Z")

	got := statsFor(t, svc, user, "all").MostWatchedShow
	if got == nil || got.Show.Title != "Long" || got.Minutes != 90 || got.Plays != 1 || got.LastPlayedAt != "2026-03-01T20:00:00Z" {
		t.Fatalf("most watched = %+v, want Long by minutes", got)
	}

	// Without runtimes, plays decide.
	setEpisodeRuntime(t, svc.DB, le, nil)
	setEpisodeRuntime(t, svc.DB, me1, nil)
	setEpisodeRuntime(t, svc.DB, me2, nil)
	got = statsFor(t, svc, user, "all").MostWatchedShow
	if got == nil || got.Show.Title != "Many" || got.Plays != 2 || got.Minutes != 0 {
		t.Fatalf("most watched = %+v, want Many by plays", got)
	}

	if statsFor(t, svc, user, "2025").MostWatchedShow != nil {
		t.Fatal("a year without episode plays must be null")
	}
}

func TestStatsMostRewatched(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	once := movie(t, svc, 1, "Once", nil)
	twice := movie(t, svc, 2, "Twice", nil)
	show := videotest.SeedShow(t, svc.DB, 10, "Show")
	e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
	e2 := videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-01-02"))
	videotest.SeedWatch(t, svc.DB, user, "movie", once, "2026-03-01T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "movie", twice, "2026-03-02T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "movie", twice, "2026-03-09T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-03-03T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "episode", e2, "2026-03-04T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-03-05T20:00:00Z")

	got := statsFor(t, svc, user, "all").MostRewatched
	if len(got) != 2 || got[0].Title.Title != "Show" || got[0].Plays != 3 || got[1].Title.Title != "Twice" || got[1].Plays != 2 {
		t.Fatalf("rewatched = %+v", got)
	}
	if got[0].LastPlayedAt != "2026-03-05T20:00:00Z" {
		t.Fatalf("last played = %q", got[0].LastPlayedAt)
	}
}

func TestStatsUnratedMovies(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	rated := movie(t, svc, 1, "Rated", nil)
	stale := movie(t, svc, 2, "Rated last year", nil)
	never := movie(t, svc, 3, "Never", nil)
	videotest.SeedWatch(t, svc.DB, user, "movie", rated, "2026-03-01T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "movie", stale, "2026-03-02T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "movie", never, "2026-03-03T20:00:00Z")
	seedRatingAt(t, svc.DB, user, "movie", rated, 8, "2026-03-01T21:00:00Z")
	seedRatingAt(t, svc.DB, user, "movie", stale, 8, "2025-06-01T21:00:00Z")

	all := statsFor(t, svc, user, "all").UnratedMovies
	if all == nil || all.Total != 1 || len(all.Recent) != 1 || all.Recent[0].Movie.Title != "Never" {
		t.Fatalf("all = %+v", all)
	}
	year := statsFor(t, svc, user, "2026").UnratedMovies
	if year == nil || year.Total != 2 || year.Recent[0].Movie.Title != "Never" || year.Recent[1].Movie.Title != "Rated last year" {
		t.Fatalf("2026 = %+v (rated in 2025 counts as unrated in the 2026 view)", year)
	}
	if year.Recent[0].LastPlayedAt != "2026-03-03T20:00:00Z" {
		t.Fatalf("last played = %q", year.Recent[0].LastPlayedAt)
	}

	seedRatingAt(t, svc.DB, user, "movie", never, 8, "2026-03-03T21:00:00Z")
	seedRatingAt(t, svc.DB, user, "movie", stale, 8, "2026-03-03T21:00:00Z")
	if statsFor(t, svc, user, "2026").UnratedMovies != nil {
		t.Fatal("everything rated must be null")
	}
}

func TestStatsDecades(t *testing.T) {
	t.Parallel()

	t.Run("contiguous buckets, favourite, oldest and newest", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		m94 := movie(t, svc, 1, "Nineties", videotest.Ptr("1994-05-01"))
		m99 := movie(t, svc, 2, "Late nineties", videotest.Ptr("1999-03-30"))
		undated := movie(t, svc, 3, "Undated", nil)
		show := videotest.SeedShow(t, svc.DB, 10, "Twenties")
		setShowFacts(t, svc.DB, show, videotest.Ptr("2021-01-01"), nil, nil)
		e := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2021-01-01"))
		for _, id := range []int64{m94, m99, undated} {
			videotest.SeedWatch(t, svc.DB, user, "movie", id, "2026-03-01T20:00:00Z")
		}
		videotest.SeedWatch(t, svc.DB, user, "episode", e, "2026-03-02T20:00:00Z")

		got := statsFor(t, svc, user, "all").Decades
		if got == nil {
			t.Fatal("decades null")
		}
		wantBuckets := []StatsDecade{{1990, 2}, {2000, 0}, {2010, 0}, {2020, 1}}
		if len(got.Buckets) != len(wantBuckets) {
			t.Fatalf("buckets = %+v", got.Buckets)
		}
		for i := range wantBuckets {
			if got.Buckets[i] != wantBuckets[i] {
				t.Fatalf("buckets = %+v, want %+v", got.Buckets, wantBuckets)
			}
		}
		if got.Favorite != 1990 || got.Covered != 3 || got.Total != 4 {
			t.Fatalf("favorite = %d, covered = %d, total = %d", got.Favorite, got.Covered, got.Total)
		}
		if got.Oldest == nil || got.Oldest.Title.Title != "Nineties" || got.Oldest.Year != 1994 {
			t.Fatalf("oldest = %+v", got.Oldest)
		}
		if got.Newest == nil || got.Newest.Title.Kind != "show" || got.Newest.Year != 2021 {
			t.Fatalf("newest = %+v", got.Newest)
		}
	})

	t.Run("a favourite tie goes to the more recent decade", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		a := movie(t, svc, 1, "A", videotest.Ptr("1985-01-01"))
		b := movie(t, svc, 2, "B", videotest.Ptr("2005-01-01"))
		videotest.SeedWatch(t, svc.DB, user, "movie", a, "2026-03-01T20:00:00Z")
		videotest.SeedWatch(t, svc.DB, user, "movie", b, "2026-03-02T20:00:00Z")
		got := statsFor(t, svc, user, "all").Decades
		if got.Favorite != 2000 {
			t.Fatalf("favorite = %d, want 2000", got.Favorite)
		}
	})

	t.Run("one dated title has no newest; none has no section", func(t *testing.T) {
		t.Parallel()
		svc := statsService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		undated := movie(t, svc, 3, "Undated", nil)
		videotest.SeedWatch(t, svc.DB, user, "movie", undated, "2026-03-01T20:00:00Z")
		if statsFor(t, svc, user, "all").Decades != nil {
			t.Fatal("no dated title must be null")
		}
		a := movie(t, svc, 1, "A", videotest.Ptr("1985-01-01"))
		videotest.SeedWatch(t, svc.DB, user, "movie", a, "2026-03-02T20:00:00Z")
		got := statsFor(t, svc, user, "all").Decades
		if got == nil || got.Oldest == nil || got.Newest != nil {
			t.Fatalf("decades = %+v", got)
		}
	})
}

func TestStatsGenres(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	drama := movie(t, svc, 1, "Drama", nil)
	setMovieFacts(t, svc.DB, drama, nil, videotest.Ptr(`["Drama","Drama","Action"]`))
	bare := movie(t, svc, 2, "Bare", nil)
	empty := movie(t, svc, 3, "Empty", nil)
	setMovieFacts(t, svc.DB, empty, nil, videotest.Ptr(`[]`))
	broken := movie(t, svc, 4, "Broken", nil)
	setMovieFacts(t, svc.DB, broken, nil, videotest.Ptr(`not json`))
	show := videotest.SeedShow(t, svc.DB, 10, "Show")
	setShowFacts(t, svc.DB, show, nil, videotest.Ptr(`["Drama","Comedy"]`), nil)
	e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
	e2 := videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-01-02"))
	for _, id := range []int64{drama, bare, empty, broken} {
		videotest.SeedWatch(t, svc.DB, user, "movie", id, "2026-03-01T20:00:00Z")
	}
	videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-03-02T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "episode", e2, "2025-03-02T20:00:00Z")

	got := statsFor(t, svc, user, "all").Genres
	if got == nil || got.Covered != 2 || got.Total != 5 {
		t.Fatalf("genres = %+v", got)
	}
	want := []StatsGenre{{"Drama", 2}, {"Action", 1}, {"Comedy", 1}}
	if len(got.Entries) != len(want) {
		t.Fatalf("entries = %+v", got.Entries)
	}
	for i := range want {
		if got.Entries[i] != want[i] {
			t.Fatalf("entries = %+v, want %+v", got.Entries, want)
		}
	}

	// A year counts only the titles played that year.
	year := statsFor(t, svc, user, "2025").Genres
	if year == nil || year.Total != 1 || year.Covered != 1 || len(year.Entries) != 2 {
		t.Fatalf("2025 genres = %+v", year)
	}
	if statsFor(t, svc, user, "2024").Genres != nil {
		t.Fatal("a year without titles must be null")
	}
}
