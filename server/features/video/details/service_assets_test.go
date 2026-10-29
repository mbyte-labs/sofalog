package details

import (
	"context"
	"strings"
	"testing"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/media"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/features/video/watches"
)

func jobRow(t *testing.T, svc *Service, dest string) (status, source string, ok bool) {
	t.Helper()
	err := svc.DB.ReadRow(context.Background(),
		`SELECT status, source_url FROM asset_jobs WHERE dest_path = ?`, dest).
		Scan(&status, &source)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "", "", false
		}
		t.Fatalf("reading job: %v", err)
	}
	return status, source, true
}

func TestSyncMovieAssets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("tracking enqueues the movie's full media set", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}))
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		id := res.TargetID

		// The exact file set the details page renders: hero poster and
		// backdrop, both galleries (both variants), cast photos, video thumbs.
		wantSources := map[string]string{
			assets.MoviePoster(id, true):                                 "w185/m.jpg",
			assets.MoviePoster(id, false):                                "w500/m.jpg",
			assets.MovieBackdrop(id, true):                               "w300/b.jpg",
			assets.MovieBackdrop(id, false):                              "w1280/b.jpg",
			assets.MovieGalleryImage(id, "backdrops", "best.jpg", false): "w1280/best.jpg",
			assets.MovieGalleryImage(id, "backdrops", "best.jpg", true):  "w300/best.jpg",
			assets.MovieGalleryImage(id, "posters", "p-top.jpg", true):   "w185/p-top.jpg",
			assets.MovieCastPhoto(id, "n.jpg"):                           "w185/n.jpg",
			assets.MovieVideoThumb(id, "vKQi3bBA1y8"):                    "vKQi3bBA1y8",
		}
		for dest, source := range wantSources {
			status, got, ok := jobRow(t, svc, dest)
			if !ok {
				t.Errorf("no job for %s", dest)
				continue
			}
			if status != "pending" || got != source {
				t.Errorf("job %s = %s %q, want pending %q", dest, status, got, source)
			}
		}
		// "fan-cut" is not a usable YouTube key; the empty-file_path backdrop
		// addresses nothing: neither becomes a job.
		if _, _, ok := jobRow(t, svc, assets.MovieVideoThumb(id, "fan-cut")); ok {
			t.Error("an unusable video key became a job")
		}
		total := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM asset_jobs WHERE media_type = 'movie' AND item_id = ?`, id)
		// 2 poster + 2 backdrop + 3 gallery backdrops ×2 + 2 gallery posters ×2
		// + 2 cast + 3 video thumbs (of 4 YouTube entries, one key unusable).
		if want := 2 + 2 + 6 + 4 + 2 + 3; total != want {
			t.Errorf("jobs = %d, want %d", total, want)
		}
	})

	t.Run("a refresh that renames artwork deletes the old files and jobs", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, clock := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}))
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		id := res.TargetID

		// Pretend the queue landed one gallery backdrop and the poster.
		oldGallery := assets.MovieGalleryImage(id, "backdrops", "best.jpg", false)
		for _, dest := range []string{oldGallery, assets.MoviePoster(id, false)} {
			if err := svc.Tree.Write([]byte("old-bytes"), dest); err != nil {
				t.Fatalf("seeding tree: %v", err)
			}
		}

		// TMDB replaces the artwork: new poster path, new gallery set.
		stub.Mu.Lock()
		stub.Bodies["/3/movie/603"] = `{"id": 603, "title": "The Matrix",
			"poster_path": "/new-poster.jpg",
			"external_ids": {"imdb_id": ""},
			"videos": {"results": []},
			"images": {"backdrops": [
				{"file_path": "/brand-new.jpg", "width": 1920, "height": 1080, "vote_average": 5, "vote_count": 1}
			], "posters": []}}`
		stub.Mu.Unlock()
		*clock = clock.Add(49 * time.Hour)

		if _, err := svc.MovieDetails(ctx, userID, MovieDetailsParams{TMDBID: "603"}); err != nil {
			t.Fatalf("MovieDetails (refresh): %v", err)
		}

		// The removed gallery image: file deleted, job row gone.
		if _, ok, _ := svc.Tree.Read(oldGallery); ok {
			t.Error("replaced gallery file still on disk")
		}
		if _, _, ok := jobRow(t, svc, oldGallery); ok {
			t.Error("job row for removed artwork survived the sync")
		}
		// The new gallery image is enqueued.
		if status, source, ok := jobRow(t, svc, assets.MovieGalleryImage(id, "backdrops", "brand-new.jpg", false)); !ok || status != "pending" || source != "w1280/brand-new.jpg" {
			t.Errorf("new gallery job = %q %q %v", status, source, ok)
		}
		// The poster keeps its dest (a slot, not a hash) but the source moved,
		// so the on-disk copy stays serveable while a re-download is pending.
		if status, source, ok := jobRow(t, svc, assets.MoviePoster(id, false)); !ok || status != "pending" || source != "w500/new-poster.jpg" {
			t.Errorf("poster job = %q %q %v", status, source, ok)
		}
		if _, ok, _ := svc.Tree.Read(assets.MoviePoster(id, false)); !ok {
			t.Error("poster file removed although its slot is still wanted")
		}

		// Re-running the sync on an unchanged snapshot is a no-op.
		row, err := common.GetMovieByTMDBID(ctx, svc.DB, 603)
		if err != nil {
			t.Fatalf("getMovieByTMDBID: %v", err)
		}
		before := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM asset_jobs`)
		svc.SyncMovieAssets(ctx, row)
		if after := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM asset_jobs`); after != before {
			t.Errorf("idempotent re-sync changed jobs: %d -> %d", before, after)
		}
	})

	t.Run("an unreferenced row gets no assets", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)

		// A row exists (materialised) but nothing references it.
		row, err := svc.RefreshMovie(ctx, 603)
		if err != nil {
			t.Fatalf("refreshMovie: %v", err)
		}
		svc.SyncMovieAssets(ctx, row)
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM asset_jobs`); n != 0 {
			t.Errorf("jobs = %d, want 0 for an unreferenced movie", n)
		}
	})

	t.Run("the DTO serves asset URLs exactly for files on disk", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}))
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		id := res.TargetID

		// Queue not drained yet: everything still rides the cache proxy.
		d, err := svc.MovieDetails(ctx, userID, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.Poster == nil || *d.Poster != "api/v1/video/posters/w185/m.jpg" {
			t.Errorf("poster before download = %v, want the proxy URL", d.Poster)
		}

		// The thumb lands on disk: only that slot switches to the asset URL.
		if err := svc.Tree.Write([]byte("img"), assets.MoviePoster(id, true)); err != nil {
			t.Fatalf("seeding tree: %v", err)
		}
		d, err = svc.MovieDetails(ctx, userID, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.Poster == nil || !strings.HasSuffix(*d.Poster, "/assets/poster.thumb.jpg") {
			t.Errorf("poster after download = %v, want the asset URL", d.Poster)
		}
		if d.PosterFull == nil || *d.PosterFull != "api/v1/video/posters/w500/m.jpg" {
			t.Errorf("poster_full = %v, want still the proxy URL", d.PosterFull)
		}

		// And the asset route serves the file.
		data, contentType, err := media.NewService(svc.Core).MovieAsset(ctx, videotest.JSONNumber(id), "poster.thumb.jpg")
		if err != nil || string(data) != "img" || contentType != "image/jpeg" {
			t.Errorf("MovieAsset = %q %q %v", data, contentType, err)
		}
	})
}
