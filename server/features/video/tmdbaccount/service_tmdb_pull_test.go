package tmdbaccount

import (
	"context"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/ratings"
	"mbyte.ro/sofalog/features/video/videotest"
)

// The setup pull (D-036), driven synchronously through runTMDBPull: ratings
// overwrite rounded, listings are additive, unknown items materialise through
// the tracking gate, nothing is ever queued back, and the job's failure
// modes leave the setup to be run again.

func TestRoundTMDBRating(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   float64
		want int
	}{
		{0, 1}, {0.5, 1}, {1, 1}, {7.4, 7}, {7.5, 8}, {9.5, 10}, {10, 10}, {11, 10},
	}
	for _, tt := range tests {
		if got := roundTMDBRating(tt.in); got != tt.want {
			t.Errorf("roundTMDBRating(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// linkWithSwitches links the user and turns every switch on WITHOUT the
// setup, so a leaked enqueue would show and setup_done stays the pull's to
// set.
func linkWithSwitches(t *testing.T, svc *Service, userID int64) {
	t.Helper()
	linkOnly(t, svc, userID)
	if _, err := svc.SetTMDBSyncKinds(context.Background(), userID, TMDBSyncKinds{Ratings: true, Watchlist: true, Favorites: true}); err != nil {
		t.Fatalf("SetTMDBSyncKinds: %v", err)
	}
}

// runPull drives one pull to the end and returns the job's record.
func runPull(t *testing.T, svc *Service, userID int64, sections tmdbPullSections) TMDBPullJob {
	t.Helper()
	ctx := context.Background()
	job := newTMDBPullJob(time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC))
	svc.runTMDBPull(ctx, userID, "sess-1", 42, sections, job)
	return job.snapshot()
}

func TestRunTMDBPull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const page1 = `{"page":1,"total_pages":1,"results":[%s]}`

	t.Run("a rated movie overwrites the local rating, rounded", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(map[string]string{"/3/account/42/rated/movies": `{"page":1,"total_pages":1,"results":[{"id":603,"rating":7.5}]}`})
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB) // tmdb 603
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 5)
		linkWithSwitches(t, svc, userID)

		job := runPull(t, svc, userID, tmdbPullSections{Ratings: true})
		if job.State != tmdbPullStateDone || job.Report.RatingsImported != 1 || job.Report.MoviesMaterialised != 0 {
			t.Fatalf("job = %+v", job)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings WHERE user_id = ? AND target_type = 'movie' AND target_id = ? AND rating = 8`, userID, movie.ID); n != 1 {
			t.Errorf("rating rows at 8 = %d, want 1", n)
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); !st.SetupDone {
			t.Error("setup not marked done after a finished pull")
		}
	})

	t.Run("pulled writes never queue a push", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(map[string]string{
			"/3/account/42/rated/movies":     `{"page":1,"total_pages":1,"results":[{"id":603,"rating":9}]}`,
			"/3/account/42/watchlist/movies": `{"page":1,"total_pages":1,"results":[{"id":603}]}`,
			"/3/account/42/favorite/movies":  `{"page":1,"total_pages":1,"results":[{"id":603}]}`,
		})
		svc, userID, _ := syncService(t, stub)
		videotest.SeedHistoryMovie(t, svc.DB)
		linkWithSwitches(t, svc, userID)

		job := runPull(t, svc, userID, tmdbPullSections{Ratings: true, Watchlist: true, Favorites: true})
		if job.State != tmdbPullStateDone || job.Report.RatingsImported != 1 || job.Report.WatchlistAdded != 1 || job.Report.FavoritesAdded != 1 {
			t.Fatalf("job = %+v", job)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows = %d, want 0", n)
		}
	})

	t.Run("a rated show and a rated episode resolve locally; an unknown episode is skipped", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(map[string]string{
			"/3/account/42/rated/tv":          `{"page":1,"total_pages":1,"results":[{"id":1399,"rating":8.5}]}`,
			"/3/account/42/rated/tv/episodes": `{"page":1,"total_pages":1,"results":[{"show_id":1399,"season_number":1,"episode_number":1,"rating":6},{"show_id":1399,"season_number":9,"episode_number":9,"rating":6}]}`,
		})
		svc, userID, _ := syncService(t, stub)
		showID := videotest.SeedShow(t, svc.DB, 1399, "Thrones")
		epID := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2011-04-17"))
		linkWithSwitches(t, svc, userID)

		job := runPull(t, svc, userID, tmdbPullSections{Ratings: true})
		if job.State != tmdbPullStateDone || job.Report.RatingsImported != 1 || job.Report.EpisodeRatingsImported != 1 || job.Report.SkippedTotal != 1 || job.Report.SkippedByReason[common.SkipEpisodeNotFound] != 1 {
			t.Fatalf("job = %+v", job)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings WHERE user_id = ? AND target_type = 'show' AND target_id = ? AND rating = 9`, userID, showID); n != 1 {
			t.Errorf("show rating rows at 9 = %d, want 1", n)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings WHERE user_id = ? AND target_type = 'episode' AND target_id = ? AND rating = 6`, userID, epID); n != 1 {
			t.Errorf("episode rating rows = %d, want 1", n)
		}
		if stub.CallCount("/3/tv/1399") != 0 {
			t.Errorf("a tracked show was fetched %d times", stub.CallCount("/3/tv/1399"))
		}
	})

	t.Run("an untracked movie is materialised through the tracking gate, once", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(map[string]string{
			"/3/account/42/rated/movies":     `{"page":1,"total_pages":1,"results":[{"id":603,"rating":7}]}`,
			"/3/account/42/watchlist/movies": `{"page":1,"total_pages":1,"results":[{"id":603}]}`,
			"/3/movie/603":                   videotest.MatrixDetailsBody,
		})
		svc, userID, _ := syncService(t, stub)
		linkWithSwitches(t, svc, userID)

		job := runPull(t, svc, userID, tmdbPullSections{Ratings: true, Watchlist: true})
		if job.State != tmdbPullStateDone || job.Report.MoviesMaterialised != 1 || job.Report.RatingsImported != 1 || job.Report.WatchlistAdded != 1 {
			t.Fatalf("job = %+v", job)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM movies WHERE tmdb_id = 603`); n != 1 {
			t.Errorf("movie rows = %d, want 1", n)
		}
		if stub.CallCount("/3/movie/603") != 1 {
			t.Errorf("movie fetched %d times, want 1", stub.CallCount("/3/movie/603"))
		}
	})

	t.Run("listings are additive and a second page is read", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(map[string]string{
			"/3/account/42/watchlist/movies?page=1": `{"page":1,"total_pages":2,"results":[{"id":603}]}`,
			"/3/account/42/watchlist/movies?page=2": `{"page":2,"total_pages":2,"results":[{"id":603}]}`,
		})
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedWatchlist(t, svc.DB, userID, "movie", movie.ID, "2026-08-01T10:00:00Z")
		linkWithSwitches(t, svc, userID)

		job := runPull(t, svc, userID, tmdbPullSections{Watchlist: true})
		if job.State != tmdbPullStateDone || job.Report.WatchlistAdded != 0 || job.Progress.ItemsDone != 2 {
			t.Fatalf("job = %+v, want nothing added (already listed) over two pages", job)
		}
		if got := stub.CallCount("/3/account/42/watchlist/movies"); got != 2 {
			t.Errorf("list calls = %d, want 2", got)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_watchlist WHERE user_id = ?`, userID); n != 1 {
			t.Errorf("watchlist rows = %d, want 1", n)
		}
	})

	t.Run("three outages open the breaker and the rest are skipped unfetched", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(map[string]string{
			"/3/account/42/rated/movies": `{"page":1,"total_pages":1,"results":[{"id":604,"rating":7},{"id":605,"rating":7},{"id":606,"rating":7},{"id":607,"rating":7}]}`,
		})
		for _, id := range []string{"604", "605", "606", "607"} {
			stub.Errs = map[string]error{}
			_ = id
		}
		stub.Errs = map[string]error{"/3/movie/604": videotest.ErrTimeout, "/3/movie/605": videotest.ErrTimeout, "/3/movie/606": videotest.ErrTimeout, "/3/movie/607": videotest.ErrTimeout}
		svc, userID, _ := syncService(t, stub)
		linkWithSwitches(t, svc, userID)

		job := runPull(t, svc, userID, tmdbPullSections{Ratings: true})
		if job.State != tmdbPullStateDone || job.Report.SkippedByReason[common.SkipMovieUnavailable] != 3 || job.Report.SkippedByReason[common.SkipProviderUnavailable] != 1 {
			t.Fatalf("job = %+v", job)
		}
		if stub.CallCount("/3/movie/607") != 0 {
			t.Error("the fourth movie was fetched with the breaker open")
		}
	})

	t.Run("a refused session breaks the link and fails the job", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(map[string]string{"/3/account/42/rated/movies": `{"status_code":3,"status_message":"Authentication failed"}`})
		stub.Statuses = map[string]int{"/3/account/42/rated/movies": 401}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 5)
		linkWithSwitches(t, svc, userID)
		if _, err := ratings.NewService(svc.Core).Rate(ctx, userID, ratings.RateParams{TrackParams: common.TrackParams{TargetType: "movie", TargetID: &movie.ID}, Rating: videotest.Ptr(5)}); err != nil {
			t.Fatalf("Rate: %v", err)
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); st.Pending != 1 {
			t.Fatalf("pending before = %d", st.Pending)
		}

		job := runPull(t, svc, userID, tmdbPullSections{Ratings: true})
		if job.State != tmdbPullStateFailed || job.Error == nil || job.Error.Code != tmdbPullErrSession {
			t.Fatalf("job = %+v", job)
		}
		st, _ := svc.TMDBLinkStatus(ctx, userID)
		if !st.Broken || st.Pending != 0 || st.SetupDone {
			t.Errorf("status = %+v, want broken, outbox discarded, setup not done", st)
		}
	})

	t.Run("a list that cannot be read fails the job as an outage", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(nil)
		stub.Errs = map[string]error{"/3/account/42/favorite/tv": videotest.ErrTimeout}
		svc, userID, _ := syncService(t, stub)
		linkWithSwitches(t, svc, userID)
		job := runPull(t, svc, userID, tmdbPullSections{Favorites: true})
		if job.State != tmdbPullStateFailed || job.Error == nil || job.Error.Code != tmdbPullErrUnavailable {
			t.Fatalf("job = %+v", job)
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); st.SetupDone {
			t.Error("setup marked done by a failed pull")
		}
	})

	t.Run("a cancelled context fails the job as cancelled", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, pullStub(nil))
		linkWithSwitches(t, svc, userID)
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		job := newTMDBPullJob(time.Now())
		svc.runTMDBPull(cctx, userID, "sess-1", 42, tmdbPullSections{Ratings: true}, job)
		if snap := job.snapshot(); snap.State != tmdbPullStateFailed || snap.Error == nil || snap.Error.Code != tmdbPullErrCancelled {
			t.Fatalf("job = %+v", snap)
		}
	})
	_ = page1
}
