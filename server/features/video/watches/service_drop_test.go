package watches

import (
	"context"
	"errors"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/details"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// dropService is the whole point of these tests in one line: dropping never
// calls a provider, so the service under test has no TMDB client at all. A
// path that tried to materialise anything would nil-panic here.
func dropService(t *testing.T) *Service {
	t.Helper()
	return &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
}

func TestDropShow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	aired := videotest.Ptr("2026-01-01")
	unaired := videotest.Ptr("2027-01-01")

	t.Run("drops a started show and answers with the new state", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Abandoned")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, unaired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		videotest.Recompute(t, svc.DB, user, show)

		res, err := svc.DropShow(ctx, user, videotest.ShowPath(show))
		if err != nil {
			t.Fatalf("DropShow: %v", err)
		}
		if !res.Changed {
			t.Error("changed = false, want true")
		}
		if res.Show.Status == nil || *res.Show.Status != "dropped" {
			t.Fatalf("status = %v, want dropped", res.Show.Status)
		}
		if res.Show.ID != show {
			t.Errorf("show id = %d, want %d", res.Show.ID, show)
		}
		// Progress is derived and a drop changes nothing about the log, so the
		// figures ride back untouched: 1 of 2 aired, 3 in total.
		if p := res.Show.Progress; p.Watched != 1 || p.AiredTotal != 2 || p.Total != 3 {
			t.Errorf("progress = %+v, want 1/2 of 3", p)
		}
		if got := videotest.ShowStatus(t, svc.DB, user, show); got != "dropped" {
			t.Errorf("stored status = %q, want dropped", got)
		}
	})

	t.Run("dropping again is an idempotent no-op", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Abandoned")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		videotest.Recompute(t, svc.DB, user, show)

		if _, err := svc.DropShow(ctx, user, videotest.ShowPath(show)); err != nil {
			t.Fatalf("first DropShow: %v", err)
		}
		res, err := svc.DropShow(ctx, user, videotest.ShowPath(show))
		if err != nil {
			t.Fatalf("second DropShow: %v", err)
		}
		if res.Changed {
			t.Error("changed = true on a repeat, want false")
		}
		if res.Show.Status == nil || *res.Show.Status != "dropped" {
			t.Errorf("status = %v, want dropped", res.Show.Status)
		}
	})

	// The rule the SPA's button gate mirrors: no episode watch means no state
	// row, and a drop must not invent a watch status for a show never begun.
	t.Run("refuses a show with no episode watch", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Unstarted")
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		// Watchlisting and rating deliberately leave the status absent, so
		// neither of them makes a show droppable either.
		videotest.SeedWatchlist(t, svc.DB, user, "show", show, "2026-08-01T20:00:00Z")

		_, err := svc.DropShow(ctx, user, videotest.ShowPath(show))
		if !errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("err = %v, want ErrValidation", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_show_states`); n != 0 {
			t.Errorf("state rows = %d, want 0 — the refusal must write nothing", n)
		}
	})

	// A watched SPECIAL is a real watch and does create a state row, even
	// though it counts toward no progress figure (D-012). This is exactly the
	// case `progress.watched > 0` would get wrong.
	t.Run("a watched special is enough to drop on", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Specials only so far")
		special := videotest.SeedEpisode(t, svc.DB, show, 0, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", special, "2026-08-01T20:00:00Z")
		videotest.Recompute(t, svc.DB, user, show)

		res, err := svc.DropShow(ctx, user, videotest.ShowPath(show))
		if err != nil {
			t.Fatalf("DropShow: %v", err)
		}
		if !res.Changed || res.Show.Progress.Watched != 0 {
			t.Errorf("result = %+v, want changed with progress.watched 0", res)
		}
	})

	t.Run("an unknown show is a 404", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")

		_, err := svc.DropShow(ctx, user, "4242")
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("a malformed id is a 422", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		for _, id := range []string{"abc", "0", "-1", ""} {
			if _, err := svc.DropShow(ctx, 1, id); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("id %q: err = %v, want ErrValidation", id, err)
			}
			if _, err := svc.UndropShow(ctx, 1, id); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("id %q (undrop): err = %v, want ErrValidation", id, err)
			}
		}
	})

	// One user's drop is their own: it must not reach another user's row, and
	// it must not hide the show from their Up Next.
	t.Run("leaves another user's state alone", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		alice := videotest.SeedUser(t, svc.DB, "a@example.com")
		bob := videotest.SeedUser(t, svc.DB, "b@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Shared")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		for _, u := range []int64{alice, bob} {
			videotest.SeedWatch(t, svc.DB, u, "episode", e1, "2026-08-01T20:00:00Z")
			videotest.Recompute(t, svc.DB, u, show)
		}

		if _, err := svc.DropShow(ctx, alice, videotest.ShowPath(show)); err != nil {
			t.Fatalf("DropShow: %v", err)
		}
		if got := videotest.ShowStatus(t, svc.DB, bob, show); got != "watching" {
			t.Errorf("bob's status = %q, want watching", got)
		}
	})
}

func TestUndropShow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	aired := videotest.Ptr("2026-01-01")

	// SPEC §6: un-dropping recomputes from the log, so a show dropped after
	// finishing comes back as watched — not as the 'watching' the clear lands
	// on before the recompute runs.
	t.Run("a show dropped after finishing returns to watched", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Done")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		videotest.Recompute(t, svc.DB, user, show)
		if _, err := svc.DropShow(ctx, user, videotest.ShowPath(show)); err != nil {
			t.Fatalf("DropShow: %v", err)
		}

		res, err := svc.UndropShow(ctx, user, videotest.ShowPath(show))
		if err != nil {
			t.Fatalf("UndropShow: %v", err)
		}
		if !res.Changed {
			t.Error("changed = false, want true")
		}
		if res.Show.Status == nil || *res.Show.Status != "watched" {
			t.Fatalf("status = %v, want watched", res.Show.Status)
		}
	})

	t.Run("a partly watched show returns to watching", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Halfway")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		videotest.Recompute(t, svc.DB, user, show)
		if _, err := svc.DropShow(ctx, user, videotest.ShowPath(show)); err != nil {
			t.Fatalf("DropShow: %v", err)
		}

		res, err := svc.UndropShow(ctx, user, videotest.ShowPath(show))
		if err != nil {
			t.Fatalf("UndropShow: %v", err)
		}
		if res.Show.Status == nil || *res.Show.Status != "watching" {
			t.Fatalf("status = %v, want watching", res.Show.Status)
		}
	})

	t.Run("un-dropping a show that is not dropped changes nothing", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Watching")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		videotest.Recompute(t, svc.DB, user, show)

		res, err := svc.UndropShow(ctx, user, videotest.ShowPath(show))
		if err != nil {
			t.Fatalf("UndropShow: %v", err)
		}
		if res.Changed {
			t.Error("changed = true, want false")
		}
		if res.Show.Status == nil || *res.Show.Status != "watching" {
			t.Errorf("status = %v, want watching", res.Show.Status)
		}
	})

	t.Run("an unstarted show is a success that changed nothing", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Unstarted")
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)

		res, err := svc.UndropShow(ctx, user, videotest.ShowPath(show))
		if err != nil {
			t.Fatalf("UndropShow: %v", err)
		}
		if res.Changed || res.Show.Status != nil {
			t.Errorf("result = %+v, want no change and a null status", res)
		}
	})

	t.Run("an unknown show is a 404", func(t *testing.T) {
		t.Parallel()
		svc := dropService(t)
		if _, err := svc.UndropShow(ctx, 1, "4242"); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

// Stickiness end to end: 'dropped' is user intent, so a later watch through the
// ordinary marking path must not quietly resume the show (D-010). The SPA is
// what offers the un-drop; the API never infers it.
func TestDropSurvivesALaterWatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, user, show := trackedShowService(t)

	if _, err := svc.MarkWatched(ctx, user, watchNow(common.TrackParams{
		TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
	})); err != nil {
		t.Fatalf("first mark: %v", err)
	}
	if _, err := svc.DropShow(ctx, user, videotest.ShowPath(show.ID)); err != nil {
		t.Fatalf("DropShow: %v", err)
	}

	res, err := svc.MarkWatched(ctx, user, watchNow(common.TrackParams{
		TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(2), Episode: videotest.PtrInt(1),
	}))
	if err != nil {
		t.Fatalf("second mark: %v", err)
	}
	if res.Show == nil || res.Show.Status == nil || *res.Show.Status != "dropped" {
		t.Fatalf("res.Show = %+v, want status dropped", res.Show)
	}
	if got := videotest.ShowStatus(t, svc.DB, user, show.ID); got != "dropped" {
		t.Errorf("stored status = %q, want dropped", got)
	}
	// The watch itself landed — dropping suppresses the status change, not the
	// history — and progress still counts it.
	if res.Show.Progress.Watched != 2 {
		t.Errorf("progress.watched = %d, want 2", res.Show.Progress.Watched)
	}
}

// TestDropOnDetailViews covers the flag the season and episode pages read to
// show their "you dropped this show" notice. It lives here rather than in the
// two details test files because the guarantee is one thing: both views borrow
// the same flag off the same call, so they can never disagree — the shape
// TestCopiesInDetails and TestStreamingOnDetailViews already have.
func TestDropOnDetailViews(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// markPilot starts the show, which is what makes a state row exist and so
	// what makes the show droppable at all.
	markPilot := func(t *testing.T, svc *Service, userID int64) {
		t.Helper()
		if _, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{
			TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
		})); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
	}

	// bothViews reads the flag off the season view and the episode view of the
	// same season, and fails when they disagree.
	bothViews := func(t *testing.T, svc *Service, userID int64) bool {
		t.Helper()
		season, err := details.NewService(svc.Core).SeasonDetails(ctx, userID, details.SeasonDetailsParams{TMDBID: "1396", Season: "2"})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		episode, err := details.NewService(svc.Core).EpisodeDetails(ctx, userID, details.EpisodeDetailsParams{
			TMDBID: "1396", Season: "2", Episode: "1",
		})
		if err != nil {
			t.Fatalf("EpisodeDetails: %v", err)
		}
		if season.ShowDropped != episode.ShowDropped {
			t.Fatalf("season show_dropped = %v but episode = %v — the two views must borrow one flag",
				season.ShowDropped, episode.ShowDropped)
		}
		return season.ShowDropped
	}

	t.Run("both views read false while the show is merely started, true once dropped", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		markPilot(t, svc, userID)

		if bothViews(t, svc, userID) {
			t.Error("show_dropped = true on a started show, want false")
		}
		if _, err := svc.DropShow(ctx, userID, videotest.ShowPath(show.ID)); err != nil {
			t.Fatalf("DropShow: %v", err)
		}
		if !bothViews(t, svc, userID) {
			t.Error("show_dropped = false after DropShow, want true")
		}
	})

	// The one action reachable from those two pages is marking, and it must not
	// make the notice vanish — the DTO-level companion to the sticky-status
	// guarantee TestDropSurvivesALaterWatch states on the column.
	t.Run("a later episode mark leaves it set, and un-dropping clears it", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		markPilot(t, svc, userID)
		if _, err := svc.DropShow(ctx, userID, videotest.ShowPath(show.ID)); err != nil {
			t.Fatalf("DropShow: %v", err)
		}

		if _, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{
			TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(2), Episode: videotest.PtrInt(1),
		})); err != nil {
			t.Fatalf("MarkWatched after the drop: %v", err)
		}
		if !bothViews(t, svc, userID) {
			t.Error("show_dropped = false after a mark, want true — dropped is sticky (D-010)")
		}

		if _, err := svc.UndropShow(ctx, userID, videotest.ShowPath(show.ID)); err != nil {
			t.Fatalf("UndropShow: %v", err)
		}
		if bothViews(t, svc, userID) {
			t.Error("show_dropped = true after UndropShow, want false")
		}
	})

	t.Run("one user's drop does not surface on another's views", func(t *testing.T) {
		t.Parallel()
		svc, alice, show := trackedShowService(t)
		bob := videotest.SeedUser(t, svc.DB, "bob")
		markPilot(t, svc, alice)
		if _, err := svc.DropShow(ctx, alice, videotest.ShowPath(show.ID)); err != nil {
			t.Fatalf("DropShow: %v", err)
		}

		if !bothViews(t, svc, alice) {
			t.Error("alice's show_dropped = false, want true")
		}
		if bothViews(t, svc, bob) {
			t.Error("bob sees alice's drop")
		}
	})

	// An untracked show has no state row to be dropped in, and no show id to
	// read one by — false is the truth, not a placeholder, and reading it must
	// not write anything.
	t.Run("a live untracked view reads false and writes nothing", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		if bothViews(t, svc, userID) {
			t.Error("show_dropped = true on a live untracked view, want false")
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_show_states`); n != 0 {
			t.Errorf("user_show_states rows = %d, want 0 — a live view stores nothing", n)
		}
	})
}
