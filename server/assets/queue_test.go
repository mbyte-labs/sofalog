package assets

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/migrations"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := db.Migrate(ctx, database, migrations.FS()); err != nil {
		t.Fatalf("migrating test db: %v", err)
	}
	return database
}

func testJob(dest string) Job {
	return Job{
		Provider:  "tmdb",
		MediaType: "movie",
		ItemID:    12,
		Kind:      "poster",
		Size:      "full",
		SourceURL: "w500/abc.jpg",
		DestPath:  dest,
	}
}

func enqueue(t *testing.T, database *db.DB, jobs ...Job) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		return UpsertJobs(context.Background(), tx, jobs, "2026-08-19T12:00:00Z")
	})
	if err != nil {
		t.Fatalf("enqueueing: %v", err)
	}
}

func jobState(t *testing.T, database *db.DB, dest string) (status string, attempts int) {
	t.Helper()
	err := database.ReadRow(context.Background(),
		`SELECT status, attempts FROM asset_jobs WHERE dest_path = ?`, dest).
		Scan(&status, &attempts)
	if err != nil {
		t.Fatalf("reading job state: %v", err)
	}
	return status, attempts
}

// testQueue wires a queue over a temp DB/tree/cache with a fixed, movable
// clock and the given fetchers.
func testQueue(t *testing.T, fetchers map[string]Fetcher) (*Queue, *time.Time) {
	t.Helper()
	dataDir := t.TempDir()
	q := NewQueue(testDB(t), NewTree(dataDir), NewCache(dataDir), fetchers)
	clock := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	q.now = func() time.Time { return clock }
	return q, &clock
}

// step claims the next due job (if any) and processes it, reporting whether
// one was claimed — the worker loop's body, without the loop's timing.
func step(t *testing.T, q *Queue) bool {
	t.Helper()
	ctx := context.Background()
	job, ok, err := claimNext(ctx, q.db, q.now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("claimNext: %v", err)
	}
	if !ok {
		return false
	}
	q.process(ctx, job)
	return true
}

func TestQueueDownloadsIntoTree(t *testing.T) {
	t.Parallel()
	fetched := 0
	q, _ := testQueue(t, map[string]Fetcher{
		"tmdb": func(ctx context.Context, source string) ([]byte, error) {
			fetched++
			if source != "w500/abc.jpg" {
				t.Errorf("source = %q", source)
			}
			return []byte("image-bytes"), nil
		},
	})
	dest := MoviePoster(12, false)
	enqueue(t, q.db, testJob(dest))

	if !step(t, q) {
		t.Fatal("no job claimed")
	}
	if status, _ := jobState(t, q.db, dest); status != "done" {
		t.Errorf("status = %q, want done", status)
	}
	data, ok, err := q.tree.Read(dest)
	if err != nil || !ok || string(data) != "image-bytes" {
		t.Errorf("tree read = %q %v %v", data, ok, err)
	}
	if fetched != 1 {
		t.Errorf("fetcher calls = %d, want 1", fetched)
	}
	// Drained: nothing left to claim.
	if step(t, q) {
		t.Error("claimed a job from a drained queue")
	}
}

func TestQueuePromotesFromCacheWithoutFetching(t *testing.T) {
	t.Parallel()
	q, _ := testQueue(t, map[string]Fetcher{
		"tmdb": func(ctx context.Context, source string) ([]byte, error) {
			t.Error("fetcher called despite a cache hit — promotion must not re-download")
			return nil, errors.New("unreachable")
		},
	})
	// The lazy image proxy already cached this file.
	if err := q.cache.Write([]byte("cached-bytes"), "tmdb", "posters", "w500", "abc.jpg"); err != nil {
		t.Fatalf("seeding cache: %v", err)
	}
	dest := MoviePoster(12, false)
	enqueue(t, q.db, testJob(dest))

	if !step(t, q) {
		t.Fatal("no job claimed")
	}
	data, ok, err := q.tree.Read(dest)
	if err != nil || !ok || string(data) != "cached-bytes" {
		t.Errorf("tree read = %q %v %v", data, ok, err)
	}
	if status, _ := jobState(t, q.db, dest); status != "done" {
		t.Errorf("status = %q, want done", status)
	}
}

func TestQueueRetriesWithBackoffThenFailsTerminally(t *testing.T) {
	t.Parallel()
	q, clock := testQueue(t, map[string]Fetcher{
		"tmdb": func(ctx context.Context, source string) ([]byte, error) {
			return nil, errors.New("404 from the CDN")
		},
	})
	dest := MoviePoster(12, false)
	enqueue(t, q.db, testJob(dest))

	// First failure: pending again with one attempt and a future retry time.
	if !step(t, q) {
		t.Fatal("no job claimed")
	}
	status, attempts := jobState(t, q.db, dest)
	if status != "pending" || attempts != 1 {
		t.Fatalf("after one failure: status = %q attempts = %d", status, attempts)
	}
	// Not due yet: the claim skips it.
	if step(t, q) {
		t.Fatal("claimed a job still waiting out its backoff")
	}

	// Walk the clock past every backoff; each pass burns one attempt.
	for i := 1; i < maxAttempts; i++ {
		*clock = clock.Add(2 * time.Hour) // past the 1h cap
		if !step(t, q) {
			t.Fatalf("attempt %d: job not due after the backoff", i+1)
		}
	}
	status, attempts = jobState(t, q.db, dest)
	if status != "failed" || attempts != maxAttempts {
		t.Errorf("after the budget: status = %q attempts = %d, want failed/%d", status, attempts, maxAttempts)
	}
	// Terminal: never claimed again.
	*clock = clock.Add(24 * time.Hour)
	if step(t, q) {
		t.Error("claimed a terminally failed job")
	}
}

func TestQueueResetRunningResumes(t *testing.T) {
	t.Parallel()
	q, _ := testQueue(t, map[string]Fetcher{
		"tmdb": func(ctx context.Context, source string) ([]byte, error) {
			return []byte("bytes"), nil
		},
	})
	dest := MoviePoster(12, false)
	enqueue(t, q.db, testJob(dest))

	// Claim, then "crash": the row is stuck running and unclaimable.
	ctx := context.Background()
	if _, ok, err := claimNext(ctx, q.db, "2026-08-19T12:00:00Z"); err != nil || !ok {
		t.Fatalf("claimNext = %v %v", ok, err)
	}
	if step(t, q) {
		t.Fatal("claimed a job another worker holds")
	}

	// Startup recovery re-queues it; the next cycle finishes it.
	if err := ResetRunning(ctx, q.db); err != nil {
		t.Fatalf("ResetRunning: %v", err)
	}
	if !step(t, q) {
		t.Fatal("job not claimable after ResetRunning")
	}
	if status, _ := jobState(t, q.db, dest); status != "done" {
		t.Errorf("status = %q, want done", status)
	}
}

func TestUpsertJobsIdentityAndReset(t *testing.T) {
	t.Parallel()
	q, _ := testQueue(t, map[string]Fetcher{
		"tmdb": func(ctx context.Context, source string) ([]byte, error) {
			return []byte("bytes"), nil
		},
	})
	dest := MoviePoster(12, false)

	// Enqueueing twice is one row (dest_path identity).
	enqueue(t, q.db, testJob(dest))
	enqueue(t, q.db, testJob(dest))
	var count int
	if err := q.db.ReadRow(context.Background(),
		`SELECT COUNT(*) FROM asset_jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows = %d (%v), want 1", count, err)
	}

	// Re-enqueueing a finished job resets it: callers only enqueue files
	// missing on disk, so a done row arriving here means its file vanished
	// and the reset is what re-downloads it.
	if !step(t, q) {
		t.Fatal("no job claimed")
	}
	enqueue(t, q.db, testJob(dest))
	status, attempts := jobState(t, q.db, dest)
	if status != "pending" || attempts != 0 {
		t.Errorf("status after re-enqueue of a done job = %q/%d, want pending/0", status, attempts)
	}

	// A source change also resets, with a fresh retry budget.
	if !step(t, q) {
		t.Fatal("no job claimed after reset")
	}
	changed := testJob(dest)
	changed.SourceURL = "w500/new.jpg"
	enqueue(t, q.db, changed)
	status, attempts = jobState(t, q.db, dest)
	if status != "pending" || attempts != 0 {
		t.Errorf("status after source change = %q/%d, want pending/0", status, attempts)
	}
}

func TestDeleteJobsForItemDests(t *testing.T) {
	t.Parallel()
	q, _ := testQueue(t, nil)
	keepDest := MoviePoster(12, false)
	dropDest := MovieGalleryImage(12, "backdrops", "old.jpg", false)
	otherDest := MoviePoster(99, false)
	other := testJob(otherDest)
	other.ItemID = 99
	drop := testJob(dropDest)
	drop.Kind = "gallery_backdrop"
	enqueue(t, q.db, testJob(keepDest), drop, other)

	err := q.db.InTx(context.Background(), func(tx db.Tx) error {
		return DeleteJobsForItemDests(context.Background(), tx, "movie", 12, []string{dropDest})
	})
	if err != nil {
		t.Fatalf("DeleteJobsForItemDests: %v", err)
	}
	for dest, want := range map[string]bool{keepDest: true, dropDest: false, otherDest: true} {
		var n int
		if err := q.db.ReadRow(context.Background(),
			`SELECT COUNT(*) FROM asset_jobs WHERE dest_path = ?`, dest).Scan(&n); err != nil {
			t.Fatalf("counting %s: %v", dest, err)
		}
		if (n == 1) != want {
			t.Errorf("job %s present = %v, want %v", dest, n == 1, want)
		}
	}
}

func TestTreeRejectsEscapes(t *testing.T) {
	t.Parallel()
	tree := NewTree(t.TempDir())
	for _, rel := range []string{
		"cache/tmdb/x.jpg",                  // outside assets/
		"assets/../secret",                  // dot element
		"assets/video/movies/1/../../../x",  // dot run
		"",                                  // empty
		"assets",                            // no file element
		`assets/video/movies/1/a\..\poster`, // backslash
		"assets/video/movies/1//poster.jpg", // empty element
	} {
		if err := tree.Write([]byte("x"), rel); err == nil {
			t.Errorf("Write(%q) accepted an invalid path", rel)
		}
	}
	if err := tree.RemoveMovieFile(1, MoviePoster(2, false)); err == nil {
		t.Error("RemoveMovieFile accepted another movie's path")
	}
}

func TestTreeListAndRemove(t *testing.T) {
	t.Parallel()
	tree := NewTree(t.TempDir())

	// Empty (no directory yet) is an empty set, not an error.
	files, err := tree.ListMovieFiles(12)
	if err != nil || len(files) != 0 {
		t.Fatalf("empty list = %v %v", files, err)
	}

	poster := MoviePoster(12, false)
	gallery := MovieGalleryImage(12, "backdrops", "a.jpg", true)
	for _, rel := range []string{poster, gallery} {
		if err := tree.Write([]byte("x"), rel); err != nil {
			t.Fatalf("Write(%s): %v", rel, err)
		}
	}
	files, err = tree.ListMovieFiles(12)
	if err != nil || !files[poster] || !files[gallery] || len(files) != 2 {
		t.Fatalf("list = %v %v", files, err)
	}

	if err := tree.RemoveMovieFile(12, gallery); err != nil {
		t.Fatalf("RemoveMovieFile: %v", err)
	}
	// Removing again is a no-op.
	if err := tree.RemoveMovieFile(12, gallery); err != nil {
		t.Fatalf("second RemoveMovieFile: %v", err)
	}
	files, _ = tree.ListMovieFiles(12)
	if len(files) != 1 || !files[poster] {
		t.Errorf("after remove: %v", files)
	}
}

func TestPathBuilders(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		MovieDir(12):            "assets/video/movies/12",
		MoviePoster(12, true):   "assets/video/movies/12/poster.thumb.jpg",
		MoviePoster(12, false):  "assets/video/movies/12/poster.jpg",
		MovieBackdrop(12, true): "assets/video/movies/12/backdrop.thumb.jpg",
		MovieGalleryImage(12, "posters", "a.png", true): "assets/video/movies/12/gallery/posters/a.thumb.png",
		MovieCastPhoto(12, "c.jpg"):                     "assets/video/movies/12/cast/c.jpg",
		MovieVideoThumb(12, "aBcDeFgHiJk"):              "assets/video/movies/12/videos/aBcDeFgHiJk.jpg",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
	}
	// The builders and the fmt package must agree forever.
	if fmt.Sprintf("assets/video/movies/%d", int64(7)) != MovieDir(7) {
		t.Error("MovieDir drifted")
	}
}
