package details

import (
	"context"
	"slices"
	"testing"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/ratings"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/features/video/watches"
)

func TestSyncShowAssets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// jobDests lists what the queue was actually asked to fetch for one item.
	jobDests := func(t *testing.T, svc *Service, showID int64) []string {
		t.Helper()
		sources, err := assets.ListJobSources(ctx, svc.DB, "show", showID)
		if err != nil {
			t.Fatalf("ListJobSources: %v", err)
		}
		dests := make([]string, 0, len(sources))
		for dest := range sources {
			dests = append(dests, dest)
		}
		slices.Sort(dests)
		return dests
	}

	t.Run("an unreferenced show owns no permanent artwork", func(t *testing.T) {
		t.Parallel()
		svc, _, show := trackedShowService(t)
		// Materialising alone is not a reference: nobody has watched, rated or
		// listed it, so it keeps riding the cache proxy (D-008).
		svc.SyncShowAssets(ctx, show)
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM asset_jobs`); n != 0 {
			t.Errorf("jobs = %d, want 0", n)
		}
	})

	t.Run("a tracked show queues its season posters and episode stills", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		if _, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watchNow(common.TrackParams{
			TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
		})); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}

		// The fixture gives season 2 a poster and gives stills to S1E1 and
		// S2E1 only; the show root carries no artwork of its own.
		want := []string{
			assets.ShowEpisodeStill(show.ID, 1, 1, false),
			assets.ShowEpisodeStill(show.ID, 1, 1, true),
			assets.ShowEpisodeStill(show.ID, 2, 1, false),
			assets.ShowEpisodeStill(show.ID, 2, 1, true),
			assets.ShowSeasonPoster(show.ID, 2, false),
			assets.ShowSeasonPoster(show.ID, 2, true),
		}
		slices.Sort(want)
		if got := jobDests(t, svc, show.ID); !slices.Equal(got, want) {
			t.Errorf("queued dests =\n%v\nwant\n%v", got, want)
		}
	})

	// Every file under a show's directory reconciles as ONE unit. A season
	// poster filed as ('season', seasons.id) would be invisible to the diff.
	t.Run("every job is filed under the show, never the season or episode", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		if _, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watchNow(common.TrackParams{
			TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
		})); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM asset_jobs WHERE media_type <> 'show' OR item_id <> ?`, show.ID); n != 0 {
			t.Errorf("%d jobs filed outside (show, %d)", n, show.ID)
		}
	})

	t.Run("a rating is a reference too", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		if _, err := ratings.NewService(svc.Core).Rate(ctx, userID, ratings.RateParams{
			TrackParams: common.TrackParams{TargetType: "show", TargetID: &show.ID}, Rating: videotest.PtrInt(8),
		}); err != nil {
			t.Fatalf("Rate: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM asset_jobs`); n == 0 {
			t.Error("rating a show queued nothing — a rating is a reference (D-008)")
		}
	})

	t.Run("re-syncing is convergent", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		if _, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watchNow(common.TrackParams{
			TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
		})); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		before := jobDests(t, svc, show.ID)
		svc.SyncShowAssets(ctx, show)
		if after := jobDests(t, svc, show.ID); !slices.Equal(before, after) {
			t.Errorf("dests changed on re-sync:\n%v\n%v", before, after)
		}
	})
}

func TestShowReferenced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// D-008 counts NINE ways for a show. Missing any one arm deletes artwork
	// somebody still tracks, so each is exercised on its own.
	arms := map[string]func(t *testing.T, svc *Service, userID int64, show common.ShowRow){
		"a state row": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			videotest.SeedShowStatus(t, svc.DB, userID, show.ID, "watching")
		},
		"a watchlist row": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			videotest.SeedWatchlist(t, svc.DB, userID, "show", show.ID, "2026-08-19T12:00:00Z")
		},
		"a rating on the show": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			videotest.SeedRating(t, svc.DB, userID, "show", show.ID, 8)
		},
		"a watch on one episode": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			ep, err := common.GetEpisodeByShowKey(ctx, svc.DB, show.ID, 1, 1)
			if err != nil {
				t.Fatalf("getEpisodeByShowKey: %v", err)
			}
			videotest.SeedWatch(t, svc.DB, userID, "episode", ep.ID, "2026-08-19T12:00:00Z")
		},
		"a rating on one episode": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			ep, err := common.GetEpisodeByShowKey(ctx, svc.DB, show.ID, 1, 1)
			if err != nil {
				t.Fatalf("getEpisodeByShowKey: %v", err)
			}
			videotest.SeedRating(t, svc.DB, userID, "episode", ep.ID, 9)
		},
		"a rating on one season": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			season, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 2)
			if err != nil {
				t.Fatalf("getSeasonByTMDBKey: %v", err)
			}
			// Seasons are not rateable through the API (D-020), but the column
			// still admits one, so the reference check must see it.
			videotest.SeedRating(t, svc.DB, userID, "season", season.ID, 7)
		},
		"a copy on one season": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			season, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 2)
			if err != nil {
				t.Fatalf("getSeasonByTMDBKey: %v", err)
			}
			videotest.SeedCopy(t, svc.DB, userID, "season", season.ID, "dvd")
		},
		"a copy on the show": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			// A show-target copy is unwritable through the API — 'show' is a
			// fan-out scope, never a stored target (D-021) — but the column
			// admits one, so the reference check must see it.
			videotest.SeedCopy(t, svc.DB, userID, "show", show.ID, "jellyfin")
		},
		"a copy on one episode": func(t *testing.T, svc *Service, userID int64, show common.ShowRow) {
			ep, err := common.GetEpisodeByShowKey(ctx, svc.DB, show.ID, 1, 1)
			if err != nil {
				t.Fatalf("getEpisodeByShowKey: %v", err)
			}
			// Episode-target copies are unwritable through the API too — same
			// defensive reasoning as the show arm above.
			videotest.SeedCopy(t, svc.DB, userID, "episode", ep.ID, "digital")
		},
	}

	for name, arm := range arms {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, userID, show := trackedShowService(t)
			referenced, err := common.ShowReferenced(ctx, svc.DB, show.ID)
			if err != nil {
				t.Fatalf("showReferenced: %v", err)
			}
			if referenced {
				t.Fatal("a freshly materialised show is not yet referenced")
			}
			arm(t, svc, userID, show)
			referenced, err = common.ShowReferenced(ctx, svc.DB, show.ID)
			if err != nil {
				t.Fatalf("showReferenced: %v", err)
			}
			if !referenced {
				t.Errorf("%s did not count as a reference", name)
			}
		})
	}
}
