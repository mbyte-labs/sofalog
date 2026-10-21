package ratings

import (
	"context"
	"errors"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/details"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// rateParams is the movie-by-tmdb_id body every case here starts from.
func rateParams(tmdbID int64, rating int) RateParams {
	return RateParams{
		TrackParams: common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(tmdbID)},
		Rating:      videotest.Ptr(rating),
	}
}

func TestRate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a tmdb_id materialises the row and stores the rating", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.Rate(ctx, userID, rateParams(603, 7))
		if err != nil {
			t.Fatalf("Rate: %v", err)
		}
		if res.TargetType != "movie" || res.TargetID < 1 || res.Rating != 7 {
			t.Fatalf("result = %+v", res)
		}
		row, err := common.GetMovieByTMDBID(ctx, svc.DB, 603)
		if err != nil || row.ID != res.TargetID {
			t.Fatalf("rating did not materialise the catalog row: %v / %+v", err, row)
		}
		// A rating alone is a reference (D-008), so the movie is now tracked.
		referenced, err := common.MovieReferenced(ctx, svc.DB, row.ID)
		if err != nil || !referenced {
			t.Errorf("movieReferenced = %v (%v), want true", referenced, err)
		}
	})

	t.Run("re-rating overwrites in place and keeps no history", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		first, err := svc.Rate(ctx, userID, rateParams(603, 4))
		if err != nil {
			t.Fatalf("first Rate: %v", err)
		}
		second, err := svc.Rate(ctx, userID,
			RateParams{TrackParams: common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(first.TargetID)}, Rating: videotest.Ptr(9)})
		if err != nil {
			t.Fatalf("second Rate: %v", err)
		}
		if second.TargetID != first.TargetID || second.Rating != 9 {
			t.Fatalf("second result = %+v", second)
		}
		rows := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_video_ratings WHERE user_id = ? AND target_type = 'movie' AND target_id = ?`,
			userID, first.TargetID)
		if rows != 1 {
			t.Errorf("rating rows = %d, want 1 — a re-rating overwrites (D-009)", rows)
		}
		d, err := details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{ID: videotest.JSONNumber(first.TargetID)})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.UserRating == nil || *d.UserRating != 9 {
			t.Errorf("user_rating = %v, want 9", d.UserRating)
		}
	})

	t.Run("rating never sets a watch status", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.Rate(ctx, userID, rateParams(603, 10))
		if err != nil {
			t.Fatalf("Rate: %v", err)
		}
		// An opinion is not a claim to have watched it (D-010): status is
		// derived from the watch log, so no state row exists at all yet.
		states := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_movie_states WHERE user_id = ? AND movie_id = ?`, userID, res.TargetID)
		if states != 0 {
			t.Errorf("user_movie_states rows = %d, want 0", states)
		}
		d, err := details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{ID: videotest.JSONNumber(res.TargetID)})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.Watched || d.Watchlisted {
			t.Errorf("watched = %v, watchlisted = %v, want both false", d.Watched, d.Watchlisted)
		}
	})

	t.Run("one user's rating is invisible to another", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		alice, bob := videotest.SeedUser(t, svc.DB, "alice"), videotest.SeedUser(t, svc.DB, "bob")

		res, err := svc.Rate(ctx, alice, rateParams(603, 8))
		if err != nil {
			t.Fatalf("Rate: %v", err)
		}
		d, err := details.NewService(svc.Core).MovieDetails(ctx, bob, details.MovieDetailsParams{ID: videotest.JSONNumber(res.TargetID)})
		if err != nil {
			t.Fatalf("MovieDetails as bob: %v", err)
		}
		if d.UserRating != nil {
			t.Errorf("bob sees user_rating = %v, want null", *d.UserRating)
		}
		// Bob rating it does not disturb alice's value.
		if _, err := svc.Rate(ctx, bob,
			RateParams{TrackParams: common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(res.TargetID)}, Rating: videotest.Ptr(2)}); err != nil {
			t.Fatalf("Rate as bob: %v", err)
		}
		d, err = details.NewService(svc.Core).MovieDetails(ctx, alice, details.MovieDetailsParams{ID: videotest.JSONNumber(res.TargetID)})
		if err != nil {
			t.Fatalf("MovieDetails as alice: %v", err)
		}
		if d.UserRating == nil || *d.UserRating != 8 {
			t.Errorf("alice's user_rating = %v, want 8", d.UserRating)
		}
	})

	t.Run("an episode rating with a colliding id never surfaces on the movie", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")
		// Episode <movie.ID> rated 3; the bare id needs no episodes row,
		// because polymorphic columns have no FK (D-013).
		videotest.SeedRating(t, svc.DB, userID, "episode", movie.ID, 3)

		st, err := common.UserMovieFlags(ctx, svc.DB, userID, movie.ID)
		if err != nil {
			t.Fatalf("userMovieFlags: %v", err)
		}
		if st.Rating != nil {
			t.Errorf("movie rating = %v, want null — an episode rating leaked through", *st.Rating)
		}
	})

	t.Run("an out-of-range or missing rating is a validation error", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		userID := videotest.SeedUser(t, svc.DB, "alice")

		for _, p := range []RateParams{
			rateParams(603, 0),
			rateParams(603, 11),
			rateParams(603, -1),
			{TrackParams: common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}},
		} {
			if _, err := svc.Rate(ctx, userID, p); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("Rate(%v) error = %v, want ErrValidation", p.Rating, err)
			}
		}
		// Rejected before any provider call, so nothing was written either.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings`); n != 0 {
			t.Errorf("rating rows = %d, want 0", n)
		}
	})

	t.Run("TMDB down on a first rating writes nothing and is 503", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/movie/603": videotest.ErrTimeout}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		if _, err := svc.Rate(ctx, userID, rateParams(603, 6)); !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("error = %v, want ErrProviderUnavailable", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings`); n != 0 {
			t.Errorf("rating rows = %d, want 0", n)
		}
	})
}

func TestRatingRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("clearing removes the row and is idempotent", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		userID := videotest.SeedUser(t, svc.DB, "alice")
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)

		id := videotest.JSONNumber(movie.ID)
		if err := svc.RatingRemove(ctx, userID, "movie", id); err != nil {
			t.Fatalf("RatingRemove: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings`); n != 0 {
			t.Errorf("rating rows = %d, want 0", n)
		}
		// Clearing what was never rated is a success, not an error.
		if err := svc.RatingRemove(ctx, userID, "movie", id); err != nil {
			t.Errorf("second RatingRemove: %v", err)
		}
		// The catalog row survives: untracking never hard-deletes (D-008).
		if _, err := common.GetMovieByID(ctx, svc.DB, movie.ID); err != nil {
			t.Errorf("catalog row gone after unrating: %v", err)
		}
	})

	t.Run("clearing only touches this user and this target type", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		alice, bob := videotest.SeedUser(t, svc.DB, "alice"), videotest.SeedUser(t, svc.DB, "bob")
		videotest.SeedRating(t, svc.DB, alice, "movie", movie.ID, 8)
		videotest.SeedRating(t, svc.DB, bob, "movie", movie.ID, 5)
		videotest.SeedRating(t, svc.DB, alice, "episode", movie.ID, 3)

		if err := svc.RatingRemove(ctx, alice, "movie", videotest.JSONNumber(movie.ID)); err != nil {
			t.Fatalf("RatingRemove: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings`); n != 2 {
			t.Errorf("remaining rating rows = %d, want 2 (bob's movie, alice's episode)", n)
		}
	})

	t.Run("a bad target is a validation error", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		userID := videotest.SeedUser(t, svc.DB, "alice")

		// 'season' is the refused target type (D-020), not 'show' — shows and
		// episodes are rateable.
		for _, tc := range [][2]string{{"season", "1"}, {"game", "1"}, {"movie", "0"}, {"movie", "abc"}, {"movie", "-3"}} {
			if err := svc.RatingRemove(ctx, userID, tc[0], tc[1]); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("RatingRemove(%q, %q) error = %v, want ErrValidation", tc[0], tc[1], err)
			}
		}
	})
}
