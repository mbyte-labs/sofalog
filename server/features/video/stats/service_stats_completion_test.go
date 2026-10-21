package stats

import (
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

func TestStatsCompletion(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")

	// Done: 2 of 2 aired (a third episode is unaired, a special is ignored).
	done := videotest.SeedShow(t, svc.DB, 10, "Done")
	d1 := videotest.SeedEpisode(t, svc.DB, done, 1, 1, videotest.Ptr("2026-01-01"))
	d2 := videotest.SeedEpisode(t, svc.DB, done, 1, 2, videotest.Ptr("2026-01-02"))
	videotest.SeedEpisode(t, svc.DB, done, 1, 3, videotest.Ptr("2026-12-01"))
	videotest.SeedEpisode(t, svc.DB, done, 0, 1, videotest.Ptr("2026-01-01"))
	videotest.SeedWatch(t, svc.DB, user, "episode", d1, "2026-03-01T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "episode", d2, "2026-03-02T20:00:00Z")
	videotest.SeedWatch(t, svc.DB, user, "episode", d2, "2026-03-03T20:00:00Z") // a rewatch never exceeds aired
	videotest.SeedShowStatus(t, svc.DB, user, done, "watched")

	// Close: 3 of 4, one left.
	closeShow := videotest.SeedShow(t, svc.DB, 11, "Close")
	var ce []int64
	for n := 1; n <= 4; n++ {
		ce = append(ce, videotest.SeedEpisode(t, svc.DB, closeShow, 1, n, videotest.Ptr("2026-01-01")))
	}
	for _, id := range ce[:3] {
		videotest.SeedWatch(t, svc.DB, user, "episode", id, "2026-03-01T20:00:00Z")
	}
	videotest.SeedShowStatus(t, svc.DB, user, closeShow, "watching")

	// Half: 1 of 2, one left too — ties on left, loses on the ratio.
	half := videotest.SeedShow(t, svc.DB, 12, "Half")
	h1 := videotest.SeedEpisode(t, svc.DB, half, 1, 1, videotest.Ptr("2026-01-01"))
	videotest.SeedEpisode(t, svc.DB, half, 1, 2, videotest.Ptr("2026-01-01"))
	videotest.SeedWatch(t, svc.DB, user, "episode", h1, "2026-03-01T20:00:00Z")
	videotest.SeedShowStatus(t, svc.DB, user, half, "watching")

	// Nothing aired yet: tracked, in progress, out of the average.
	fresh := videotest.SeedShow(t, svc.DB, 13, "Fresh")
	videotest.SeedEpisode(t, svc.DB, fresh, 1, 1, nil)
	videotest.SeedShowStatus(t, svc.DB, user, fresh, "watching")

	// Dropped: excluded entirely.
	dropped := videotest.SeedShow(t, svc.DB, 14, "Dropped")
	dr := videotest.SeedEpisode(t, svc.DB, dropped, 1, 1, videotest.Ptr("2026-01-01"))
	videotest.SeedWatch(t, svc.DB, user, "episode", dr, "2026-03-01T20:00:00Z")
	videotest.SeedShowStatus(t, svc.DB, user, dropped, "dropped")

	// A MOVIE watch carrying an episode's id must never count (D-013).
	m := movie(t, svc, 1, "Movie", nil)
	videotest.SeedWatch(t, svc.DB, user, "movie", m, "2026-03-01T20:00:00Z")

	got := statsFor(t, svc, user, "all").Completion
	if got == nil {
		t.Fatal("completion null")
	}
	if got.Tracked != 4 || got.Completed != 1 || got.InProgress != 3 {
		t.Fatalf("tracked = %d, completed = %d, in progress = %d", got.Tracked, got.Completed, got.InProgress)
	}
	// (1 + 0.75 + 0.5) / 3
	if want := 0.75; got.AverageCompletion < want-1e-9 || got.AverageCompletion > want+1e-9 {
		t.Fatalf("average = %v, want %v", got.AverageCompletion, want)
	}
	if len(got.AlmostThere) != 2 || got.AlmostThere[0].Show.Title != "Close" || got.AlmostThere[1].Show.Title != "Half" {
		t.Fatalf("almost there = %+v", got.AlmostThere)
	}
	if a := got.AlmostThere[0]; a.Watched != 3 || a.Aired != 4 || a.Left != 1 {
		t.Fatalf("close = %+v", a)
	}

	if statsFor(t, svc, user, "2026").Completion != nil {
		t.Fatal("a year has no completion section")
	}
}

func TestStatsCompletionAlmostThereCap(t *testing.T) {
	t.Parallel()
	svc := statsService(t)
	user := videotest.SeedUser(t, svc.DB, "a@example.com")
	for i := int64(1); i <= 5; i++ {
		show := videotest.SeedShow(t, svc.DB, 10+i, "S")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-01-01"))
		for n := 2; n <= 1+int(i); n++ {
			videotest.SeedEpisode(t, svc.DB, show, 1, n, videotest.Ptr("2026-01-01"))
		}
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-03-01T20:00:00Z")
		videotest.SeedShowStatus(t, svc.DB, user, show, "watching")
	}
	got := statsFor(t, svc, user, "all").Completion
	if len(got.AlmostThere) != 3 || got.AlmostThere[0].Left != 1 || got.AlmostThere[2].Left != 3 {
		t.Fatalf("almost there = %+v", got.AlmostThere)
	}
}
