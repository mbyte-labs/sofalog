package lists

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

// seedBackdrop gives a catalog row a TMDB backdrop path directly — the seeders
// leave the column empty. table is a test literal, never input.
func seedBackdrop(t *testing.T, database *db.DB, table string, id int64, path string) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE `+table+` SET backdrop_path = ? WHERE id = ?`, path, id)
		return err
	})
	if err != nil {
		t.Fatalf("seeding backdrop: %v", err)
	}
}

func TestLists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("an empty user gets every list with zeros and no art", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		alice := videotest.SeedUser(t, svc.DB, "alice")

		got, err := svc.Lists(ctx, alice)
		if err != nil {
			t.Fatalf("Lists: %v", err)
		}
		if len(got) != 3 || got[0].Key != "watchlist" || got[1].Key != "favorites" || got[2].Key != "dropped" {
			t.Fatalf("Lists = %+v, want watchlist, favorites, dropped", got)
		}
		for _, l := range got {
			if l.Movies != 0 || l.Shows != 0 || l.Backdrops == nil || len(l.Backdrops) != 0 ||
				l.BackdropsFull == nil || len(l.BackdropsFull) != 0 {
				t.Errorf("%s = %+v, want zeros and non-nil empty backdrop slices", l.Key, l)
			}
		}
	})

	t.Run("counts split by kind, per list, per user, without dangling rows", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		alice := videotest.SeedUser(t, svc.DB, "alice")
		bob := videotest.SeedUser(t, svc.DB, "bob")
		m1 := videotest.SeedMovie(t, svc.DB, 1, "One", nil, nil, `{}`)
		m2 := videotest.SeedMovie(t, svc.DB, 2, "Two", nil, nil, `{}`)
		sh := seedShowSnapshot(t, svc.DB, 10, "Show", nil, nil, `{}`)

		videotest.SeedWatchlist(t, svc.DB, alice, "movie", m1.ID, "2026-08-01T00:00:00Z")
		videotest.SeedWatchlist(t, svc.DB, alice, "movie", m2.ID, "2026-08-02T00:00:00Z")
		videotest.SeedWatchlist(t, svc.DB, alice, "show", sh.ID, "2026-08-03T00:00:00Z")
		videotest.SeedWatchlist(t, svc.DB, alice, "movie", 9999, "2026-08-04T00:00:00Z") // dangling
		videotest.SeedFavorite(t, svc.DB, alice, "show", sh.ID, "2026-08-05T00:00:00Z")
		videotest.SeedWatchlist(t, svc.DB, bob, "movie", m1.ID, "2026-08-06T00:00:00Z")
		videotest.SeedFavorite(t, svc.DB, bob, "movie", m2.ID, "2026-08-07T00:00:00Z")
		watching := seedShowSnapshot(t, svc.DB, 11, "Watching", nil, nil, `{}`)
		seedShowState(t, svc.DB, alice, sh.ID, "dropped", "2026-08-08T00:00:00Z")
		seedShowState(t, svc.DB, alice, watching.ID, "watching", "2026-08-08T00:00:00Z")
		seedShowState(t, svc.DB, bob, watching.ID, "dropped", "2026-08-08T00:00:00Z")

		got, err := svc.Lists(ctx, alice)
		if err != nil {
			t.Fatalf("Lists: %v", err)
		}
		if w := got[0]; w.Movies != 2 || w.Shows != 1 {
			t.Errorf("watchlist = %d movies, %d shows; want 2, 1", w.Movies, w.Shows)
		}
		if f := got[1]; f.Movies != 0 || f.Shows != 1 {
			t.Errorf("favorites = %d movies, %d shows; want 0, 1", f.Movies, f.Shows)
		}
		if d := got[2]; d.Movies != 0 || d.Shows != 1 {
			t.Errorf("dropped = %d movies, %d shows; want 0, 1", d.Movies, d.Shows)
		}
	})

	t.Run("backdrops are newest first, capped, and skip items without one", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		alice := videotest.SeedUser(t, svc.DB, "alice")

		// Six movies listed one day apart; movie 3 has no backdrop, and a show
		// sharing movie 1's id sits newest of all.
		for i := int64(1); i <= 6; i++ {
			m := videotest.SeedMovie(t, svc.DB, i, "M", nil, nil, `{}`)
			if i != 3 {
				seedBackdrop(t, svc.DB, "movies", m.ID, fmt.Sprintf("/m%d.jpg", i))
			}
			videotest.SeedWatchlist(t, svc.DB, alice, "movie", m.ID, fmt.Sprintf("2026-08-0%dT00:00:00Z", i))
		}
		sh := seedShowSnapshot(t, svc.DB, 10, "Show", nil, nil, `{}`)
		seedBackdrop(t, svc.DB, "shows", sh.ID, "/s.jpg")
		videotest.SeedWatchlist(t, svc.DB, alice, "show", sh.ID, "2026-08-09T00:00:00Z")

		got, err := svc.Lists(ctx, alice)
		if err != nil {
			t.Fatalf("Lists: %v", err)
		}
		// No asset set on disk, so every URL is the cache proxy's w300.
		want := []string{
			"api/v1/video/posters/w300/s.jpg",
			"api/v1/video/posters/w300/m6.jpg",
			"api/v1/video/posters/w300/m5.jpg",
			"api/v1/video/posters/w300/m4.jpg",
		}
		if !slices.Equal(got[0].Backdrops, want) {
			t.Errorf("watchlist backdrops = %v, want %v", got[0].Backdrops, want)
		}
		// The full variants ride beside them, same order, same length.
		wantFull := []string{
			"api/v1/video/posters/w1280/s.jpg",
			"api/v1/video/posters/w1280/m6.jpg",
			"api/v1/video/posters/w1280/m5.jpg",
			"api/v1/video/posters/w1280/m4.jpg",
		}
		if !slices.Equal(got[0].BackdropsFull, wantFull) {
			t.Errorf("watchlist backdrops_full = %v, want %v", got[0].BackdropsFull, wantFull)
		}
		if got[0].Movies != 6 || got[0].Shows != 1 {
			t.Errorf("watchlist = %d movies, %d shows; want 6, 1", got[0].Movies, got[0].Shows)
		}
		if len(got[1].Backdrops) != 0 {
			t.Errorf("favorites backdrops = %v, want none", got[1].Backdrops)
		}
	})

	t.Run("dropped backdrops are newest drop first", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		alice := videotest.SeedUser(t, svc.DB, "alice")
		older := seedShowSnapshot(t, svc.DB, 1, "Older", nil, nil, `{}`)
		newer := seedShowSnapshot(t, svc.DB, 2, "Newer", nil, nil, `{}`)
		seedBackdrop(t, svc.DB, "shows", older.ID, "/older.jpg")
		seedBackdrop(t, svc.DB, "shows", newer.ID, "/newer.jpg")
		seedShowState(t, svc.DB, alice, older.ID, "dropped", "2026-08-01T00:00:00Z")
		seedShowState(t, svc.DB, alice, newer.ID, "dropped", "2026-08-02T00:00:00Z")

		got, err := svc.Lists(ctx, alice)
		if err != nil {
			t.Fatalf("Lists: %v", err)
		}
		want := []string{"api/v1/video/posters/w300/newer.jpg", "api/v1/video/posters/w300/older.jpg"}
		if !slices.Equal(got[2].Backdrops, want) {
			t.Errorf("dropped backdrops = %v, want %v", got[2].Backdrops, want)
		}
	})
}
