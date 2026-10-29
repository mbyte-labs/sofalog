package watches

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/details"
	"mbyte.ro/sofalog/features/video/lists"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

func TestMarkWatched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a tmdb_id materialises the row, logs the watch and sets the status", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}))
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		if !res.Created || res.TargetType != "movie" || res.TargetID < 1 {
			t.Fatalf("result = %+v", res)
		}
		row, err := common.GetMovieByTMDBID(ctx, svc.DB, 603)
		if err != nil || row.ID != res.TargetID {
			t.Fatalf("tracking did not materialise the catalog row: %v / %+v", err, row)
		}
		if got := videotest.MovieStatus(t, svc.DB, userID, row.ID); got != "watched" {
			t.Errorf("status = %q, want watched", got)
		}

		// Idempotent without a watched_at: a double-submit logs nothing new.
		// A deliberate rewatch is the dated-log mode, tested below (D-009).
		again, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}))
		if err != nil {
			t.Fatalf("second MarkWatched: %v", err)
		}
		if again.Created || again.TargetID != res.TargetID {
			t.Errorf("second result = %+v, want created=false, same target", again)
		}
		watches := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_watch_history WHERE user_id = ? AND target_type = 'movie' AND target_id = ?`,
			userID, row.ID)
		if watches != 1 {
			t.Errorf("watch rows = %d, want 1", watches)
		}

		// The details view now reports the tracked state truthfully.
		d, err := details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.ID == nil || *d.ID != row.ID || !d.Tracked || !d.Watched || d.Watchlisted {
			t.Errorf("details after tracking = id %v tracked %v watched %v watchlisted %v",
				d.ID, d.Tracked, d.Watched, d.Watchlisted)
		}
	})

	t.Run("TMDB down on first track writes nothing and is 503", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/movie/603": errors.New("dial tcp: timeout")}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		_, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}))
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want ErrProviderUnavailable", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM movies`); n != 0 {
			t.Errorf("movies rows = %d, want 0", n)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 0 {
			t.Errorf("watch rows = %d, want 0", n)
		}
	})

	t.Run("a target_id miss is 404 and never touches TMDB", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		_, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(42)}))
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if n := stub.CallCount("/3/movie/42"); n != 0 {
			t.Errorf("a target_id lookup reached TMDB %d times", n)
		}
	})

	t.Run("a movie can never be given a non-watched status", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		// 'watched' is the only value user_movie_states.status permits: a movie
		// is one sitting, so there is nothing to abandon partway (SPEC.md §6).
		// The CHECK is what makes that unrepresentable rather than merely
		// unwritten, so assert the constraint itself — sticky-dropped is show
		// behaviour and gets its own test against user_show_states in M2.
		for _, status := range []string{"dropped", "watching"} {
			err := svc.DB.InTx(ctx, func(tx db.Tx) error {
				_, err := tx.Exec(ctx,
					`INSERT INTO user_movie_states (user_id, movie_id, status, updated_at)
					 VALUES (?, ?, ?, ?)`, userID, movie.ID, status, "2026-08-01T00:00:00Z")
				return err
			})
			if err == nil {
				t.Errorf("inserting status %q succeeded, want the CHECK to reject it", status)
			} else if !strings.Contains(err.Error(), "CHECK constraint failed") {
				// Assert the REASON, not just that it failed — a PK collision or
				// a typo'd column would otherwise green this test for free.
				t.Errorf("status %q was rejected by something other than the CHECK: %v", status, err)
			}
		}
	})

	t.Run("the status collapses when the last watch disappears", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		if _, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TargetID: &movie.ID})); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		// Delete the watch and recompute — the non-dropped row must collapse:
		// "tracked but unwatched" is a watchlist row, not a status.
		err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			if _, err := tx.Exec(ctx,
				`DELETE FROM user_watch_history WHERE user_id = ? AND target_type = 'movie' AND target_id = ?`,
				userID, movie.ID); err != nil {
				return err
			}
			return common.RecomputeMovieStatus(ctx, tx, userID, movie.ID, "2026-08-19T13:00:00Z")
		})
		if err != nil {
			t.Fatalf("recompute after delete: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_movie_states WHERE user_id = ? AND movie_id = ?`,
			userID, movie.ID); n != 0 {
			t.Errorf("state rows = %d, want 0 after the last watch is gone", n)
		}
	})

	t.Run("an episode watch with a colliding id does not mark the movie", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		// Episode <movie.ID> and movie <movie.ID>: the id alone identifies
		// nothing (D-013). Polymorphic columns have no FK, so the bare episode
		// id needs no episodes row.
		videotest.SeedWatch(t, svc.DB, userID, "episode", movie.ID, "2026-08-10T20:00:00Z")

		d, err := details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{ID: fmt.Sprint(movie.ID)})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.Watched || d.Tracked {
			t.Errorf("watched = %v tracked = %v, want false — the watch is on an episode", d.Watched, d.Tracked)
		}
	})

	t.Run("one user's watch is invisible to another", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		bob := videotest.SeedUser(t, svc.DB, "bob")

		if _, err := svc.MarkWatched(ctx, alice, watchNow(common.TrackParams{TargetType: "movie", TargetID: &movie.ID})); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		d, err := details.NewService(svc.Core).MovieDetails(ctx, bob, details.MovieDetailsParams{ID: fmt.Sprint(movie.ID)})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		// Tracked is catalog-wide (any user references it); watched is bob's own.
		if !d.Tracked || d.Watched {
			t.Errorf("tracked = %v watched = %v, want tracked && !watched for bob", d.Tracked, d.Watched)
		}
	})

	invalid := []struct {
		name   string
		params common.TrackParams
	}{
		{"missing target_type", common.TrackParams{TMDBID: videotest.PtrInt64(603)}},
		{"unsupported target_type", common.TrackParams{TargetType: "season", TMDBID: videotest.PtrInt64(603)}},
		{"neither id", common.TrackParams{TargetType: "movie"}},
		{"both ids", common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603), TargetID: videotest.PtrInt64(1)}},
		{"non-positive tmdb_id", common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(0)}},
		{"non-positive target_id", common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(-1)}},
	}
	for _, tt := range invalid {
		t.Run(tt.name+" is a validation error", func(t *testing.T) {
			t.Parallel()
			svc, _ := detailsService(t, &videotest.ProviderStub{})
			if _, err := svc.MarkWatched(ctx, 1, watchNow(tt.params)); !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("MarkWatched err = %v, want ErrValidation", err)
			}
			if _, err := lists.NewService(svc.Core).WatchlistAdd(ctx, 1, tt.params); !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("WatchlistAdd err = %v, want ErrValidation", err)
			}
		})
	}

	// The two operations' allow-lists differ by exactly this target: a show is
	// watchlistable, but marking one watched is the bulk route's job, because
	// its honest answer is a count and not a boolean.
	t.Run("a show target is watchlistable but not markable", func(t *testing.T) {
		t.Parallel()
		svc, _ := detailsService(t, &videotest.ProviderStub{})
		p := common.TrackParams{TargetType: "show", TargetID: videotest.PtrInt64(1)}
		if _, err := svc.MarkWatched(ctx, 1, watchNow(p)); !errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("MarkWatched err = %v, want ErrValidation", err)
		}
		if _, err := lists.NewService(svc.Core).WatchlistAdd(ctx, 1, p); errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("WatchlistAdd err = %v, want anything but ErrValidation", err)
		}
	})
}

// TestLogWatch covers the dated-log mode: a watched_at makes the call an
// explicit record of one viewing rather than an idempotent first mark, which is
// what makes rewatch history reachable through the API (D-009).
func TestLogWatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Every case here backdates relative to FixedClock (2026-08-18T12:00:00Z).
	const past = "2026-07-04T21:30:00Z"

	watchedAts := func(t *testing.T, svc *Service, userID, movieID int64) []string {
		t.Helper()
		rows, err := svc.DB.Read(ctx,
			`SELECT watched_at FROM user_watch_history
			 WHERE user_id = ? AND target_type = 'movie' AND target_id = ?
			 ORDER BY watched_at, id`, userID, movieID)
		if err != nil {
			t.Fatalf("reading watches: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var at string
			if err := rows.Scan(&at); err != nil {
				t.Fatalf("scanning watch: %v", err)
			}
			out = append(out, at)
		}
		return out
	}

	t.Run("a backdated first watch stores the instant it was given", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.MarkWatched(ctx, userID, watchAt(
			common.TrackParams{TargetType: "movie", TargetID: &movie.ID}, past))
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		if !res.Created {
			t.Error("created = false, want true — a dated log always writes")
		}
		if got := watchedAts(t, svc, userID, movie.ID); len(got) != 1 || got[0] != past {
			t.Errorf("watched_at = %v, want [%s]", got, past)
		}
		// The status follows the log, not the clock: a watch a month ago still
		// means watched.
		if got := videotest.MovieStatus(t, svc.DB, userID, movie.ID); got != "watched" {
			t.Errorf("status = %q, want watched", got)
		}
	})

	t.Run("a second dated log is a rewatch, not a no-op", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")
		p := common.TrackParams{TargetType: "movie", TargetID: &movie.ID}

		// Marked the ordinary way first, so this asserts the two modes compose:
		// the idempotent guard must not swallow the deliberate log that follows.
		if _, err := svc.MarkWatched(ctx, userID, watchNow(p)); err != nil {
			t.Fatalf("first mark: %v", err)
		}
		again, err := svc.MarkWatched(ctx, userID, watchAt(p, past))
		if err != nil {
			t.Fatalf("dated log: %v", err)
		}
		if !again.Created {
			t.Error("created = false on a dated log of an already-watched movie")
		}
		got := watchedAts(t, svc, userID, movie.ID)
		want := []string{past, "2026-08-18T12:00:00Z"}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("watched_at rows = %v, want %v", got, want)
		}
	})

	t.Run("an offset and sub-second precision are normalised away", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		// The SPA builds the instant with Date.toISOString(), and a hand-written
		// client may send a local offset. Both must land as one UTC instant with
		// no offset, or the feed's plain string ordering breaks (SPEC §4).
		if _, err := svc.MarkWatched(ctx, userID, watchAt(
			common.TrackParams{TargetType: "movie", TargetID: &movie.ID},
			"2026-07-04T23:30:00.250+02:00")); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		if got := watchedAts(t, svc, userID, movie.ID); len(got) != 1 || got[0] != "2026-07-04T21:30:00Z" {
			t.Errorf("watched_at = %v, want [2026-07-04T21:30:00Z]", got)
		}
	})

	t.Run("a rating rides along in the same transaction", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		p := WatchParams{
			TrackParams: common.TrackParams{TargetType: "movie", TargetID: &movie.ID},
			WatchedAt:   videotest.Ptr(past),
			Rating:      videotest.PtrInt(8),
		}
		if _, err := svc.MarkWatched(ctx, userID, p); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		var rating int
		var updatedAt string
		err := svc.DB.ReadRow(ctx,
			`SELECT rating, updated_at FROM user_video_ratings
			 WHERE user_id = ? AND target_type = 'movie' AND target_id = ?`,
			userID, movie.ID).Scan(&rating, &updatedAt)
		if err != nil {
			t.Fatalf("reading rating: %v", err)
		}
		if rating != 8 {
			t.Errorf("rating = %d, want 8", rating)
		}
		// A rating is the current opinion, so its timestamp is the real clock
		// even when the viewing it came with is backdated.
		if updatedAt != "2026-08-18T12:00:00Z" {
			t.Errorf("updated_at = %q, want the real now, not the backdate", updatedAt)
		}
	})

	t.Run("a refused request writes nothing at all", func(t *testing.T) {
		t.Parallel()
		bad := []struct {
			name string
			p    WatchParams
		}{
			{"malformed watched_at", WatchParams{WatchedAt: videotest.Ptr("4 July 2026")}},
			{"a date rather than an instant", WatchParams{WatchedAt: videotest.Ptr("2026-07-04")}},
			// No lower bound — a watch can predate the library — but the future
			// is a typo, not a plan.
			{"watched_at in the future", WatchParams{WatchedAt: videotest.Ptr("2026-08-19T00:00:00Z")}},
			{"rating below the range", WatchParams{WatchedAt: videotest.Ptr(past), Rating: videotest.PtrInt(0)}},
			{"rating above the range", WatchParams{WatchedAt: videotest.Ptr(past), Rating: videotest.PtrInt(11)}},
			{"rating without a date", WatchParams{Rating: videotest.PtrInt(99)}},
		}
		for _, tc := range bad {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
				movie := videotest.SeedHistoryMovie(t, svc.DB)
				userID := videotest.SeedUser(t, svc.DB, "alice")

				p := tc.p
				p.TrackParams = common.TrackParams{TargetType: "movie", TargetID: &movie.ID}
				if _, err := svc.MarkWatched(ctx, userID, p); !errors.Is(err, httpx.ErrValidation) {
					t.Fatalf("err = %v, want ErrValidation", err)
				}
				if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 0 {
					t.Errorf("watch rows = %d, want 0", n)
				}
				if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings`); n != 0 {
					t.Errorf("rating rows = %d, want 0", n)
				}
			})
		}
	})
}

func TestWatchlist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("add is idempotent, remove is idempotent, the catalog row survives", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)})
		if err != nil || !res.Created {
			t.Fatalf("WatchlistAdd = %+v, %v", res, err)
		}
		again, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)})
		if err != nil || again.Created {
			t.Fatalf("second WatchlistAdd = %+v, %v — want created=false", again, err)
		}

		d, err := details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.ID == nil || !d.Tracked || !d.Watchlisted || d.Watched {
			t.Errorf("details = id %v tracked %v watchlisted %v watched %v", d.ID, d.Tracked, d.Watchlisted, d.Watched)
		}

		// Watchlisting alone fabricates no watch status (a watchlist row is an
		// independent axis, D-011).
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_movie_states`); n != 0 {
			t.Errorf("state rows = %d, want 0 — watchlist is not a status", n)
		}

		for i := 0; i < 2; i++ { // twice: removing an absent row is a success
			if err := lists.NewService(svc.Core).WatchlistRemove(ctx, userID, "movie", fmt.Sprint(res.TargetID)); err != nil {
				t.Fatalf("WatchlistRemove #%d: %v", i+1, err)
			}
		}
		// Untracking never hard-deletes (D-008): the catalog row stays.
		if _, err := common.GetMovieByTMDBID(ctx, svc.DB, 603); err != nil {
			t.Errorf("catalog row gone after watchlist removal: %v", err)
		}
		d, err = details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails after removal: %v", err)
		}
		if d.Watchlisted || d.Tracked {
			t.Errorf("watchlisted = %v tracked = %v after removal, want false", d.Watchlisted, d.Tracked)
		}
	})

	// A show is watchlistable, and adding an untracked one by tmdb_id is what
	// pulls it local — the same materialisation marking an episode performs.
	t.Run("a show round-trips and materialises on the way in", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, common.TrackParams{TargetType: "show", TMDBID: videotest.PtrInt64(1396)})
		if err != nil || !res.Created || res.TargetType != "show" || res.TargetID < 1 {
			t.Fatalf("WatchlistAdd = %+v, %v", res, err)
		}
		// The whole show came local, seasons and episodes included.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes`); n == 0 {
			t.Error("episode rows = 0, want the materialised show's episodes")
		}
		// And no watch status was fabricated: the watchlist is an independent
		// axis, never a status (D-011).
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_show_states`); n != 0 {
			t.Errorf("show state rows = %d, want 0", n)
		}

		again, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, common.TrackParams{TargetType: "show", TargetID: &res.TargetID})
		if err != nil || again.Created {
			t.Fatalf("second WatchlistAdd = %+v, %v — want created=false", again, err)
		}

		d, err := details.NewService(svc.Core).ShowDetails(ctx, userID, details.ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails: %v", err)
		}
		if !d.Tracked || !d.Watchlisted || d.Watched {
			t.Errorf("details = tracked %v watchlisted %v watched %v", d.Tracked, d.Watchlisted, d.Watched)
		}

		for i := 0; i < 2; i++ { // twice: removing an absent row is a success
			if err := lists.NewService(svc.Core).WatchlistRemove(ctx, userID, "show", fmt.Sprint(res.TargetID)); err != nil {
				t.Fatalf("WatchlistRemove #%d: %v", i+1, err)
			}
		}
		// Untracking never hard-deletes (D-008): the catalog row stays.
		if _, err := common.GetShowByTMDBID(ctx, svc.DB, 1396); err != nil {
			t.Errorf("catalog row gone after watchlist removal: %v", err)
		}
		if d, err = details.NewService(svc.Core).ShowDetails(ctx, userID, details.ShowDetailsParams{TMDBID: "1396"}); err != nil {
			t.Fatalf("ShowDetails after removal: %v", err)
		}
		if d.Watchlisted {
			t.Error("watchlisted = true after removal, want false")
		}
	})

	t.Run("remove validates its path parameters", func(t *testing.T) {
		t.Parallel()
		svc, _ := detailsService(t, &videotest.ProviderStub{})
		for _, tc := range [][2]string{{"episode", "1"}, {"season", "1"}, {"movie", "abc"}, {"movie", "0"}} {
			if err := lists.NewService(svc.Core).WatchlistRemove(ctx, 1, tc[0], tc[1]); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("WatchlistRemove(%s, %s) err = %v, want ErrValidation", tc[0], tc[1], err)
			}
		}
	})
}

// TestDeleteWatch covers the correction path: removing one row from the watch
// log. The interesting half is not the delete but what the recompute does
// afterwards — the collapse branches only became reachable with this service.
func TestDeleteWatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("deleting a movie's only watch collapses its status", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		if _, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TargetID: &movie.ID})); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		var watchID int64
		if err := svc.DB.ReadRow(ctx,
			`SELECT id FROM user_watch_history WHERE user_id = ? AND target_type = 'movie' AND target_id = ?`,
			userID, movie.ID).Scan(&watchID); err != nil {
			t.Fatalf("reading the watch id: %v", err)
		}

		if err := svc.DeleteWatch(ctx, userID, videotest.JSONNumber(watchID)); err != nil {
			t.Fatalf("DeleteWatch: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 0 {
			t.Errorf("watch rows = %d, want 0", n)
		}
		// "Tracked but unwatched" is a watchlist row, not a status, so the
		// state row goes rather than reverting to some other value.
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_movie_states WHERE user_id = ? AND movie_id = ?`,
			userID, movie.ID); n != 0 {
			t.Errorf("state rows = %d, want 0 after the last watch is gone", n)
		}
		// The catalog row survives: untracking never hard-deletes (D-008).
		if _, err := common.GetMovieByID(ctx, svc.DB, movie.ID); err != nil {
			t.Errorf("catalog row gone after deleting the watch: %v", err)
		}
	})

	t.Run("deleting one of two watches leaves the movie watched", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		// A rewatch is a second dated row (D-009), so removing one is a
		// correction, not an unmark.
		first := videotest.SeedWatch(t, svc.DB, userID, "movie", movie.ID, "2026-08-01T20:00:00Z")
		videotest.SeedWatch(t, svc.DB, userID, "movie", movie.ID, "2026-08-15T20:00:00Z")
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return common.RecomputeMovieStatus(ctx, tx, userID, movie.ID, videotest.StatusNow)
		}); err != nil {
			t.Fatalf("seeding the status: %v", err)
		}

		if err := svc.DeleteWatch(ctx, userID, videotest.JSONNumber(first)); err != nil {
			t.Fatalf("DeleteWatch: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 1 {
			t.Errorf("watch rows = %d, want 1", n)
		}
		if got := videotest.MovieStatus(t, svc.DB, userID, movie.ID); got != "watched" {
			t.Errorf("status = %q, want watched — a rewatch still remains", got)
		}
	})

	t.Run("deleting an episode watch demotes then collapses the show", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		userID := videotest.SeedUser(t, svc.DB, "alice")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		aired := videotest.Ptr("2026-01-01")
		e1 := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, aired)
		e2 := videotest.SeedEpisode(t, svc.DB, showID, 1, 2, aired)

		w1 := videotest.SeedWatch(t, svc.DB, userID, "episode", e1, videotest.StatusNow)
		w2 := videotest.SeedWatch(t, svc.DB, userID, "episode", e2, videotest.StatusNow)
		videotest.Recompute(t, svc.DB, userID, showID)
		if got := videotest.ShowStatus(t, svc.DB, userID, showID); got != "watched" {
			t.Fatalf("seeded status = %q, want watched", got)
		}

		// One watch away from complete: the show falls back to watching.
		if err := svc.DeleteWatch(ctx, userID, videotest.JSONNumber(w2)); err != nil {
			t.Fatalf("DeleteWatch: %v", err)
		}
		if got := videotest.ShowStatus(t, svc.DB, userID, showID); got != "watching" {
			t.Errorf("status = %q, want watching after one episode is un-watched", got)
		}
		// The last watch away: the derived row is not a state at all.
		if err := svc.DeleteWatch(ctx, userID, videotest.JSONNumber(w1)); err != nil {
			t.Fatalf("DeleteWatch: %v", err)
		}
		if got := videotest.ShowStatus(t, svc.DB, userID, showID); got != "" {
			t.Errorf("status = %q, want no row at all", got)
		}
	})

	t.Run("a dropped show keeps its row when its last watch goes", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		userID := videotest.SeedUser(t, svc.DB, "alice")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		e1 := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2026-01-01"))
		w1 := videotest.SeedWatch(t, svc.DB, userID, "episode", e1, videotest.StatusNow)
		videotest.SeedShowStatus(t, svc.DB, userID, showID, "dropped")

		if err := svc.DeleteWatch(ctx, userID, videotest.JSONNumber(w1)); err != nil {
			t.Fatalf("DeleteWatch: %v", err)
		}
		// dropped is user intent, not a derivation, so it survives a recompute
		// that has nothing left to derive from (D-010).
		if got := videotest.ShowStatus(t, svc.DB, userID, showID); got != "dropped" {
			t.Errorf("status = %q, want dropped to survive the collapse", got)
		}
	})

	t.Run("another user's watch is a 404 and survives", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		bob := videotest.SeedUser(t, svc.DB, "bob")
		watchID := videotest.SeedWatch(t, svc.DB, alice, "movie", movie.ID, videotest.StatusNow)

		// user_watch_history.id is a global sequence, so the store scopes on
		// user_id too — without it bob deletes alice's row.
		if err := svc.DeleteWatch(ctx, bob, videotest.JSONNumber(watchID)); !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 1 {
			t.Errorf("watch rows = %d, want alice's row untouched", n)
		}
	})

	t.Run("a missing id is 404 and a malformed one is a validation error", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		userID := videotest.SeedUser(t, svc.DB, "alice")

		// Unlike the watchlist and rating deletes this one is NOT idempotent:
		// it names a specific row, so a second call must not report success.
		if err := svc.DeleteWatch(ctx, userID, "999"); !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("DeleteWatch(999) err = %v, want ErrNotFound", err)
		}
		for _, id := range []string{"0", "-3", "abc", ""} {
			if err := svc.DeleteWatch(ctx, userID, id); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("DeleteWatch(%q) err = %v, want ErrValidation", id, err)
			}
		}
	})
}
