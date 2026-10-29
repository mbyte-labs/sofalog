package stats

import (
	"context"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

// testService carries no DB at all: parameter validation must reject before
// anything could reach the store, and a nil pointer proves it does.
func testService() *Service {
	return &Service{Core: &common.Core{Now: videotest.FixedClock}}
}

// statsService is a Service over a fresh SQLite file with the fixed clock
// (2026-08-18 12:00 UTC) and no provider client — a render path reaching TMDB
// would panic here rather than pass.
func statsService(t *testing.T) *Service {
	t.Helper()
	return statsServiceAt(t, videotest.FixedClock)
}

func statsServiceAt(t *testing.T, clock func() time.Time) *Service {
	t.Helper()
	return &Service{Core: &common.Core{Now: clock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
}

// statsFor computes one page, failing the test on any error.
func statsFor(t *testing.T, svc *Service, userID int64, period string) *Stats {
	t.Helper()
	out, err := svc.Stats(context.Background(), userID, StatsParams{Period: period})
	if err != nil {
		t.Fatalf("Stats(%q): %v", period, err)
	}
	return out
}

func exec(t *testing.T, database *db.DB, q string, args ...any) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// setMovieFacts writes the runtime and genres SeedMovie leaves empty.
func setMovieFacts(t *testing.T, database *db.DB, id int64, runtime *int, genres *string) {
	t.Helper()
	exec(t, database, `UPDATE movies SET runtime_min = ?, genres = ? WHERE id = ?`, runtime, genres, id)
}

func setMovieBackdrop(t *testing.T, database *db.DB, id int64, path string) {
	t.Helper()
	exec(t, database, `UPDATE movies SET backdrop_path = ? WHERE id = ?`, path, id)
}

func setEpisodeRuntime(t *testing.T, database *db.DB, id int64, runtime *int) {
	t.Helper()
	exec(t, database, `UPDATE episodes SET runtime_min = ? WHERE id = ?`, runtime, id)
}

// setShowFacts writes the first air date, genres and (when non-nil) the stored
// body SeedShow leaves at their defaults.
func setShowFacts(t *testing.T, database *db.DB, id int64, firstAir, genres, metadataJSON *string) {
	t.Helper()
	exec(t, database, `UPDATE shows SET first_air_date = ?, genres = ? WHERE id = ?`, firstAir, genres, id)
	if metadataJSON != nil {
		exec(t, database, `UPDATE shows SET metadata_json = ? WHERE id = ?`, *metadataJSON, id)
	}
}

// seedRatingAt is SeedRating with the instant stated — the trend and the
// year filter turn on updated_at, which videotest pins.
func seedRatingAt(t *testing.T, database *db.DB, userID int64, targetType string, targetID int64, rating int, at string) {
	t.Helper()
	exec(t, database,
		`INSERT INTO user_video_ratings (user_id, target_type, target_id, rating, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (user_id, target_type, target_id) DO UPDATE SET rating = excluded.rating, updated_at = excluded.updated_at`,
		userID, targetType, targetID, rating, at)
}

// movie seeds a plain movie and returns its id.
func movie(t *testing.T, svc *Service, tmdbID int64, title string, release *string) int64 {
	t.Helper()
	return videotest.SeedMovie(t, svc.DB, tmdbID, title, release, nil, `{}`).ID
}

// titleKeys renders a list of titles as "kind:title" for one-line asserts.
func titleKeys(titles []StatsTitle) []string {
	out := make([]string, 0, len(titles))
	for _, t := range titles {
		out = append(out, t.Kind+":"+t.Title)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
