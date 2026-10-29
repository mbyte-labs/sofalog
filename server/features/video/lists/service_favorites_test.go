package lists

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/details"
	"mbyte.ro/sofalog/features/video/refresh"
	"mbyte.ro/sofalog/features/video/tmdbaccount"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// TestFavoriteAddRemove is the write path: a favorite is a tracking action
// (D-015), idempotent both ways, never a status, and never a hard delete.
func TestFavoriteAddRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a movie materialises on the way in and its row survives removal", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.FavoriteAdd(ctx, userID, common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)})
		if err != nil || !res.Created || res.TargetType != "movie" {
			t.Fatalf("FavoriteAdd = %+v, %v", res, err)
		}
		again, err := svc.FavoriteAdd(ctx, userID, common.TrackParams{TargetType: "movie", TargetID: &res.TargetID})
		if err != nil || again.Created {
			t.Fatalf("second FavoriteAdd = %+v, %v — want created=false", again, err)
		}

		d, err := details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.ID == nil || !d.Tracked || !d.Favorited || d.Watchlisted || d.Watched {
			t.Errorf("details = id %v tracked %v favorited %v watchlisted %v watched %v",
				d.ID, d.Tracked, d.Favorited, d.Watchlisted, d.Watched)
		}
		// A favorite is not a status (D-030) and not a watchlist row (D-011).
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_movie_states`); n != 0 {
			t.Errorf("state rows = %d, want 0", n)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_watchlist`); n != 0 {
			t.Errorf("watchlist rows = %d, want 0", n)
		}
		// The whole point of the axis: the row is a D-008 reference on its own.
		if ref, err := common.MovieReferenced(ctx, svc.DB, res.TargetID); err != nil || !ref {
			t.Errorf("movieReferenced = %v, %v — want true on a favorite alone", ref, err)
		}

		for i := 0; i < 2; i++ { // twice: removing an absent row is a success
			if err := svc.FavoriteRemove(ctx, userID, "movie", fmt.Sprint(res.TargetID)); err != nil {
				t.Fatalf("FavoriteRemove #%d: %v", i+1, err)
			}
		}
		if _, err := common.GetMovieByTMDBID(ctx, svc.DB, 603); err != nil {
			t.Errorf("catalog row gone after favorite removal: %v", err)
		}
		if d, err = details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{TMDBID: "603"}); err != nil {
			t.Fatalf("MovieDetails after removal: %v", err)
		}
		if d.Favorited || d.Tracked {
			t.Errorf("favorited = %v tracked = %v after removal, want false", d.Favorited, d.Tracked)
		}
	})

	t.Run("a show materialises whole and reads back favorited", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.FavoriteAdd(ctx, userID, common.TrackParams{TargetType: "show", TMDBID: videotest.PtrInt64(1396)})
		if err != nil || !res.Created || res.TargetType != "show" || res.TargetID < 1 {
			t.Fatalf("FavoriteAdd = %+v, %v", res, err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes`); n == 0 {
			t.Error("episode rows = 0, want the materialised show's episodes")
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_show_states`); n != 0 {
			t.Errorf("show state rows = %d, want 0", n)
		}
		if ref, err := common.ShowReferenced(ctx, svc.DB, res.TargetID); err != nil || !ref {
			t.Errorf("showReferenced = %v, %v — want true on a favorite alone", ref, err)
		}

		d, err := details.NewService(svc.Core).ShowDetails(ctx, userID, details.ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails: %v", err)
		}
		if !d.Tracked || !d.Favorited || d.Watchlisted || d.Watched {
			t.Errorf("details = tracked %v favorited %v watchlisted %v watched %v",
				d.Tracked, d.Favorited, d.Watchlisted, d.Watched)
		}

		if err := svc.FavoriteRemove(ctx, userID, "show", fmt.Sprint(res.TargetID)); err != nil {
			t.Fatalf("FavoriteRemove: %v", err)
		}
		if ref, err := common.ShowReferenced(ctx, svc.DB, res.TargetID); err != nil || ref {
			t.Errorf("showReferenced = %v, %v after removal — want false", ref, err)
		}
		if _, err := common.GetShowByTMDBID(ctx, svc.DB, 1396); err != nil {
			t.Errorf("catalog row gone after favorite removal: %v", err)
		}
	})

	t.Run("seasons and episodes are refused", func(t *testing.T) {
		t.Parallel()
		svc, _ := detailsService(t, &videotest.ProviderStub{})
		for _, tt := range []string{"episode", "season"} {
			_, err := svc.FavoriteAdd(ctx, 1, common.TrackParams{TargetType: tt, TargetID: videotest.PtrInt64(1)})
			if !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("FavoriteAdd(%s) err = %v, want ErrValidation", tt, err)
			}
		}
		for _, tc := range [][2]string{{"episode", "1"}, {"season", "1"}, {"movie", "abc"}, {"movie", "0"}} {
			if err := svc.FavoriteRemove(ctx, 1, tc[0], tc[1]); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("FavoriteRemove(%s, %s) err = %v, want ErrValidation", tc[0], tc[1], err)
			}
		}
	})
}

// favoritesFixture is the read path's world: alice favorited three movies
// and a show (the show's id collides with a movie's — D-013), bob has his
// own row that must stay invisible, and one row points at nothing.
type favoritesFixture struct {
	svc                    *Service
	alice, bob             int64
	alien, bright, undated common.MovieRow
	mars                   common.ShowRow
}

func newFavoritesFixture(t *testing.T) favoritesFixture {
	t.Helper()
	f := favoritesFixture{svc: &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}}
	f.alien = videotest.SeedMovie(t, f.svc.DB, 348, "alien electric", videotest.Ptr("1979-05-25"), videotest.Ptr("/a.jpg"),
		`{"vote_average": 8.4, "external_ids": {}, "images": {}}`)
	f.bright = videotest.SeedMovie(t, f.svc.DB, 349, "Bright Wire", videotest.Ptr("2001-01-09"), videotest.Ptr("/b.jpg"),
		`{"vote_average": 6.1, "external_ids": {}, "images": {}}`)
	f.undated = videotest.SeedMovie(t, f.svc.DB, 350, "Zero Hour", nil, nil,
		`{"external_ids": {}, "images": {}}`)
	f.mars = seedShowSnapshot(t, f.svc.DB, 1399, "Mars Signal", videotest.Ptr("1990-03-04"), videotest.Ptr("/m.jpg"),
		`{"vote_average": 7.7, "external_ids": {}, "images": {}}`)
	f.alice = videotest.SeedUser(t, f.svc.DB, "alice")
	f.bob = videotest.SeedUser(t, f.svc.DB, "bob")

	videotest.SeedFavorite(t, f.svc.DB, f.alice, "movie", f.alien.ID, "2026-06-02T20:15:00Z")
	videotest.SeedFavorite(t, f.svc.DB, f.alice, "movie", f.bright.ID, "2026-06-02T20:15:00Z")
	videotest.SeedFavorite(t, f.svc.DB, f.alice, "movie", f.undated.ID, "2026-08-15T21:05:00Z")
	videotest.SeedFavorite(t, f.svc.DB, f.alice, "show", f.mars.ID, "2026-07-20T10:00:00Z")

	videotest.SeedWatch(t, f.svc.DB, f.alice, "movie", f.alien.ID, "2026-07-01T20:00:00Z")
	videotest.SeedRating(t, f.svc.DB, f.alice, "movie", f.alien.ID, 9)
	// Same ids, wrong target_type: both must stay out of alien's entry.
	videotest.SeedWatch(t, f.svc.DB, f.alice, "episode", f.alien.ID, "2026-07-02T20:00:00Z")
	videotest.SeedRating(t, f.svc.DB, f.alice, "episode", f.bright.ID, 2)

	// A watchlist row is NOT a favorite: bright is on alice's watchlist too,
	// which changes nothing here, and undated's watchlist row belongs to bob.
	videotest.SeedWatchlist(t, f.svc.DB, f.alice, "movie", f.bright.ID, "2026-06-03T10:00:00Z")
	videotest.SeedFavorite(t, f.svc.DB, f.bob, "movie", f.bright.ID, "2026-06-03T10:00:00Z")
	videotest.SeedFavorite(t, f.svc.DB, f.alice, "movie", 999999, "2026-07-03T20:00:00Z")
	return f
}

func (f favoritesFixture) favorites(t *testing.T, p FavoritesParams) []FavoriteEntry {
	t.Helper()
	entries, err := f.svc.Favorites(context.Background(), f.alice, p)
	if err != nil {
		t.Fatalf("Favorites(%+v) error = %v", p, err)
	}
	return entries
}

func TestFavoritesList(t *testing.T) {
	t.Parallel()
	f := newFavoritesFixture(t)
	alien := "movie:" + videotest.JSONNumber(f.alien.ID)
	bright := "movie:" + videotest.JSONNumber(f.bright.ID)
	undated := "movie:" + videotest.JSONNumber(f.undated.ID)
	mars := "show:" + videotest.JSONNumber(f.mars.ID)

	tests := []struct {
		name string
		p    FavoritesParams
		want []string
	}{
		// Newest first is the default; the tie on added_at breaks on the
		// (target_type, target_id) pair, descending like the key.
		{"default: most recently favorited first", FavoritesParams{}, []string{undated, mars, bright, alien}},
		{"added asc", FavoritesParams{Order: "asc"}, []string{alien, bright, mars, undated}},
		{"title asc, case-folded", FavoritesParams{OrderBy: "title", Order: "asc"}, []string{alien, bright, mars, undated}},
		{"release date asc, undated last", FavoritesParams{OrderBy: "release_date", Order: "asc"}, []string{alien, mars, bright, undated}},
		{"release date desc, undated still last", FavoritesParams{OrderBy: "release_date"}, []string{bright, mars, alien, undated}},
		{"movies only", FavoritesParams{Type: "movie"}, []string{undated, bright, alien}},
		{"shows only", FavoritesParams{Type: "show"}, []string{mars}},
		{"paged", FavoritesParams{Limit: "2", Offset: "1"}, []string{mars, bright}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := watchlistKeys(f.favorites(t, tt.p))
			if !slices.Equal(got, tt.want) {
				t.Errorf("keys = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("projects each target's own state", func(t *testing.T) {
		byKey := map[string]FavoriteEntry{}
		for _, e := range f.favorites(t, FavoritesParams{}) {
			byKey[watchlistKeys([]WatchlistEntry{e})[0]] = e
		}
		a := byKey[alien]
		if !a.Watched || a.UserRating == nil || *a.UserRating != 9 || a.Rating == nil || *a.Rating != 8.4 {
			t.Errorf("alien = watched %v user_rating %v rating %v", a.Watched, a.UserRating, a.Rating)
		}
		// The episode watch and rating with alien's/bright's ids are not theirs.
		if b := byKey[bright]; b.Watched || b.UserRating != nil {
			t.Errorf("bright = watched %v user_rating %v, want neither", b.Watched, b.UserRating)
		}
		if m := byKey[mars]; m.Show == nil || m.Movie != nil || m.Show.Name != "Mars Signal" || m.Poster == nil || m.PosterFull == nil {
			t.Errorf("mars = %+v, want the show arm filled with both poster sizes", m)
		}
	})

	t.Run("is per user", func(t *testing.T) {
		entries, err := f.svc.Favorites(context.Background(), f.bob, FavoritesParams{})
		if err != nil {
			t.Fatal(err)
		}
		if got := watchlistKeys(entries); !slices.Equal(got, []string{bright}) {
			t.Errorf("bob's favorites = %v, want only bright", got)
		}
	})

	t.Run("invalid params are 422", func(t *testing.T) {
		for _, p := range []FavoritesParams{
			{OrderBy: "rating"}, {Order: "up"}, {Type: "episode"}, {Limit: "0"}, {Offset: "-1"},
		} {
			if _, err := f.svc.Favorites(context.Background(), f.alice, p); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("Favorites(%+v) err = %v, want ErrValidation", p, err)
			}
		}
	})
}

// TestFavoriteRefreshClass: a favorite alone is the ACTIVE cadence (D-025),
// whatever the item's watch state, and it keeps the item out of ARCHIVE.
func TestFavoriteRefreshClass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := videotest.DB(t)
	alice, bob := videotest.SeedUser(t, database, "alice"), videotest.SeedUser(t, database, "bob")
	at := "2026-08-10T20:00:00Z"

	// A watched movie would be archive; bob's favorite makes it active.
	movie := videotest.SeedMovie(t, database, 1, "Loved", nil, nil, "{}")
	videotest.SeedWatch(t, database, alice, "movie", movie.ID, at)
	videotest.SeedFavorite(t, database, bob, "movie", movie.ID, at)
	// D-013: a show favorite with the movie's id is not the movie's.
	stranger := videotest.SeedMovie(t, database, 2, "Stranger", nil, nil, "{}")
	videotest.SeedFavorite(t, database, alice, "show", stranger.ID, at)

	// An ended show alice finished would be archive; her favorite keeps it active.
	show := videotest.SeedShow(t, database, 100, "Ended favorite")
	videotest.SetShowProductionStatus(t, database, show, videotest.Ptr("Ended"))
	videotest.SeedShowStatus(t, database, alice, show, "watched")
	videotest.SeedFavorite(t, database, alice, "show", show, at)
	// A favorited show nobody ever started is active too.
	fresh := videotest.SeedShow(t, database, 101, "Never started")
	videotest.SetShowProductionStatus(t, database, fresh, videotest.Ptr("Ended"))
	videotest.SeedFavorite(t, database, bob, "show", fresh, at)

	cutoff := "2026-08-19T00:00:00Z"
	if got, err := refresh.DueMovies(ctx, database, refresh.RefreshActive, cutoff); err != nil || !slices.Equal(movieIDs(got), []int64{movie.ID}) {
		t.Errorf("active movies = %v, %v; want [%d]", movieIDs(got), err, movie.ID)
	}
	if got, err := refresh.DueMovies(ctx, database, refresh.RefreshArchive, cutoff); err != nil || len(got) != 0 {
		t.Errorf("archive movies = %v, %v; want none", movieIDs(got), err)
	}
	if got, err := refresh.DueShows(ctx, database, refresh.RefreshActive, cutoff); err != nil || !slices.Equal(videotest.ShowIDs(got), []int64{show, fresh}) {
		t.Errorf("active shows = %v, %v; want [%d %d]", videotest.ShowIDs(got), err, show, fresh)
	}
	if got, err := refresh.DueShows(ctx, database, refresh.RefreshArchive, cutoff); err != nil || len(got) != 0 {
		t.Errorf("archive shows = %v, %v; want none", videotest.ShowIDs(got), err)
	}
}

// TestTMDBSyncFavorites mirrors TestTMDBSyncWatchlist (D-029): the outbox
// fills once linked, linking backfills, and the worker hits the favorite
// route with the same body shape as the watchlist's.
func TestTMDBSyncFavorites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("an add or remove queues once linked, and never before", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		p := common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(movie.ID)}

		if _, err := svc.FavoriteAdd(ctx, userID, p); err != nil {
			t.Fatalf("FavoriteAdd: %v", err)
		}
		if err := svc.FavoriteRemove(ctx, userID, "movie", videotest.JSONNumber(movie.ID)); err != nil {
			t.Fatalf("FavoriteRemove: %v", err)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Fatalf("outbox rows before link = %d, want 0", n)
		}
		linkUser(t, svc, userID)

		if _, err := svc.FavoriteAdd(ctx, userID, p); err != nil {
			t.Fatalf("FavoriteAdd: %v", err)
		}
		if r := videotest.SyncRows(t, svc.DB, userID)["favorite/movie:"+videotest.JSONNumber(movie.ID)]; r.Op != "set" || r.Rating != nil {
			t.Errorf("add row = %+v", r)
		}
		if err := svc.FavoriteRemove(ctx, userID, "movie", videotest.JSONNumber(movie.ID)); err != nil {
			t.Fatalf("FavoriteRemove: %v", err)
		}
		rows := videotest.SyncRows(t, svc.DB, userID)
		if r := rows["favorite/movie:"+videotest.JSONNumber(movie.ID)]; len(rows) != 1 || r.Op != "delete" {
			t.Errorf("outbox = %+v, want one delete row replacing the add", rows)
		}
	})

	t.Run("linking backfills the existing favorites", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		showID := videotest.SeedShow(t, svc.DB, 1399, "Thrones")
		videotest.SeedFavorite(t, svc.DB, userID, "movie", movie.ID, "2026-08-01T00:00:00Z")
		videotest.SeedFavorite(t, svc.DB, userID, "show", showID, "2026-08-02T00:00:00Z")
		other := videotest.SeedUser(t, svc.DB, "bob")
		videotest.SeedFavorite(t, svc.DB, other, "movie", movie.ID, "2026-08-03T00:00:00Z")

		linkUser(t, svc, userID)

		rows := videotest.SyncRows(t, svc.DB, userID)
		if len(rows) != 2 {
			t.Fatalf("outbox = %+v, want two favorite rows", rows)
		}
		for _, key := range []string{"favorite/movie:" + videotest.JSONNumber(movie.ID), "favorite/show:" + videotest.JSONNumber(showID)} {
			if r, ok := rows[key]; !ok || r.Op != "set" || r.Rating != nil {
				t.Errorf("%s = %+v, want a favorite add", key, r)
			}
		}
		if n := len(videotest.SyncRows(t, svc.DB, other)); n != 0 {
			t.Errorf("the other user's outbox has %d rows, want 0", n)
		}
	})

	t.Run("the worker posts to the account's favorite route", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		showID := videotest.SeedShow(t, svc.DB, 1399, "Thrones")
		linkUser(t, svc, userID)
		stub.Bodies["/3/account/42/favorite"] = `{"status_code":1,"status_message":"Success."}`

		if _, err := svc.FavoriteAdd(ctx, userID, common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(movie.ID)}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.FavoriteAdd(ctx, userID, common.TrackParams{TargetType: "show", TargetID: videotest.PtrInt64(showID)}); err != nil {
			t.Fatal(err)
		}
		if rep := tmdbaccount.NewService(svc.Core).RunTMDBSyncBatch(ctx); rep.Sent != 2 || rep.Deferred+rep.Dropped+rep.Broken != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if n := stub.CallCount("/3/account/42/favorite"); n != 2 {
			t.Errorf("favorite route called %d times, want 2", n)
		}
		if n := stub.CallCount("/3/account/42/watchlist"); n != 0 {
			t.Errorf("watchlist route called %d times, want 0", n)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows = %d, want 0", n)
		}
	})
}
