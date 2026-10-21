package videotest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
)

// SeedCopy writes one copies row directly, so reads and reference counts can be
// tested without the write path — including rows on the CHECK-admitted target
// types the API refuses (show, episode), which the write path cannot produce.
func SeedCopy(t *testing.T, database *db.DB, userID int64, targetType string, targetID int64, kind string) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO user_video_copies (user_id, target_type, target_id, kind, added_at)
			 VALUES (?, ?, ?, ?, ?)`,
			userID, targetType, targetID, kind, "2026-08-19T12:00:00Z")
		return err
	})
	if err != nil {
		t.Fatalf("seeding copy: %v", err)
	}
}

// SeedFavorite writes one favorites row directly, so the read path can be
// tested without the write path that produced it — including a row pointing
// at nothing, which the write path cannot produce.
func SeedFavorite(t *testing.T, database *db.DB, userID int64, targetType string, targetID int64, at string) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO user_video_favorites (user_id, target_type, target_id, added_at)
			 VALUES (?, ?, ?, ?)`,
			userID, targetType, targetID, at)
		return err
	})
	if err != nil {
		t.Fatalf("seeding favorite: %v", err)
	}
}

// SeedMovie inserts one catalog movie a feed can hang off, returning the stored
// row with its assigned id.
func SeedMovie(t *testing.T, database *db.DB, tmdbID int64, title string, releaseDate, posterPath *string, metadataJSON string) common.MovieRow {
	t.Helper()
	var row common.MovieRow
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		var err error
		row, err = common.UpsertMovieByTMDBID(context.Background(), tx, common.MovieRow{
			TMDBID:       tmdbID,
			Title:        title,
			ReleaseDate:  releaseDate,
			PosterPath:   posterPath,
			MetadataJSON: metadataJSON,
			FetchedAt:    "2026-08-18T00:00:00Z",
		})
		return err
	})
	if err != nil {
		t.Fatalf("seeding movie: %v", err)
	}
	return row
}

// SeedHistoryMovie is the fully populated default seed: a poster, a community
// score, and every appended-section key present so a details view of it never
// triggers the missing-sections refresh (the tests using it wire no TMDB client).
func SeedHistoryMovie(t *testing.T, database *db.DB) common.MovieRow {
	t.Helper()
	return SeedMovie(t, database, 603, "The Matrix", Ptr("1999-03-30"), Ptr("/m.jpg"),
		`{"vote_average": 8.2, "external_ids": {}, "images": {},
			"watch/providers": {"sofalog_region": "US", "results": {}},
			"recommendations": {"results": []}}`)
}

// SeedUser inserts a bare user row (raw SQL is fine in tests — the layering
// grep excludes _test.go) so FK-checked per-user rows can be written.
func SeedUser(t *testing.T, database *db.DB, email string) int64 {
	t.Helper()
	var id int64
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		return tx.QueryRow(context.Background(),
			`INSERT INTO users (email, created_at) VALUES (?, ?) RETURNING id`,
			email, "2026-08-18T00:00:00Z").Scan(&id)
	})
	if err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	return id
}

// SetUserTimezone stores a user's chosen IANA zone (D-038) directly, the way
// auth's SetTimezone would; "" clears it back to the server default.
func SetUserTimezone(t *testing.T, database *db.DB, userID int64, zone string) {
	t.Helper()
	var v any
	if zone != "" {
		v = zone
	}
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE users SET timezone = ? WHERE id = ?`, v, userID)
		return err
	})
	if err != nil {
		t.Fatalf("setting timezone: %v", err)
	}
}

// LateEveningClock is 2026-08-18 22:30 UTC — still the 18th in UTC and New
// York, already the 19th in Bucharest (UTC+3 in summer). The one instant the
// D-038 tests need: where "today" ends depends on whose day it is.
func LateEveningClock() time.Time {
	return time.Date(2026, 8, 18, 22, 30, 0, 0, time.UTC)
}

// SeedWatch inserts one watch-log row directly, so a feed fixture can state
// its instants outright instead of driving MarkWatched once per row.
func SeedWatch(t *testing.T, database *db.DB, userID int64, targetType string, targetID int64, at string) int64 {
	t.Helper()
	var id int64
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		return tx.QueryRow(context.Background(),
			`INSERT INTO user_watch_history (user_id, target_type, target_id, watched_at, watch_type)
			 VALUES (?, ?, ?, ?, 'watch') RETURNING id`,
			userID, targetType, targetID, at).Scan(&id)
	})
	if err != nil {
		t.Fatalf("seeding watch: %v", err)
	}
	return id
}

// SeedRating writes one rating row directly, so a read path can be tested
// without going through the write path that produced it.
func SeedRating(t *testing.T, database *db.DB, userID int64, targetType string, targetID int64, rating int) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO user_video_ratings (user_id, target_type, target_id, rating, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			userID, targetType, targetID, rating, "2026-08-18T00:00:00Z")
		return err
	})
	if err != nil {
		t.Fatalf("seeding rating: %v", err)
	}
}

// SetShowProductionStatus writes TMDB's status column — the one the two classes
// split on. nil is "TMDB said nothing".
func SetShowProductionStatus(t *testing.T, database *db.DB, showID int64, status *string) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE shows SET status = ? WHERE id = ?`, status, showID)
		return err
	})
	if err != nil {
		t.Fatalf("setting show status: %v", err)
	}
}

func ShowIDs(rows []common.ShowRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// FixedClock pins "today" to 2026-08-18 so aired/unaired fixtures stay on
// their side of the line forever.
func FixedClock() time.Time {
	return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
}

// SeedShowWithPoster is SeedShow plus artwork, for the poster-fallback tests.
func SeedShowWithPoster(t *testing.T, database *db.DB, tmdbID int64, title string, posterPath *string) int64 {
	t.Helper()
	var row common.ShowRow
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		var err error
		row, err = common.UpsertShowByTMDBID(context.Background(), tx, common.ShowRow{
			TMDBID:       tmdbID,
			Title:        title,
			PosterPath:   posterPath,
			MetadataJSON: `{"external_ids": {}, "images": {}}`,
			FetchedAt:    "2026-08-18T00:00:00Z",
		})
		return err
	})
	if err != nil {
		t.Fatalf("seeding show: %v", err)
	}
	return row.ID
}

// SeedSeasonSnapshot inserts one season row carrying a poster and a snapshot
// whose episodes array is where an up-next card's community score and marker
// come from (episodes deliberately store no metadata_json of their own).
// It returns the seasons.id it minted — the handle an up-next entry carries so
// the card can link to the episode details page.
func SeedSeasonSnapshot(t *testing.T, database *db.DB, showID, showTMDBID int64, seasonNumber int, posterPath *string, metadataJSON string) int64 {
	t.Helper()
	var id int64
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		return tx.QueryRow(context.Background(), `
INSERT INTO seasons (show_id, show_tmdb_id, season_number, name, show_title, poster_path, metadata_json, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			showID, showTMDBID, seasonNumber, fmt.Sprintf("Season %d", seasonNumber), "S",
			posterPath, metadataJSON, "2026-08-18T00:00:00Z").Scan(&id)
	})
	if err != nil {
		t.Fatalf("seeding season snapshot: %v", err)
	}
	return id
}

// SeedWatchlist writes one watchlist row directly, so the read path can be
// tested without the write path that produced it — including a row pointing at
// nothing, which the write path cannot produce.
func SeedWatchlist(t *testing.T, database *db.DB, userID int64, targetType string, targetID int64, at string) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO user_video_watchlist (user_id, target_type, target_id, added_at)
			 VALUES (?, ?, ?, ?)`,
			userID, targetType, targetID, at)
		return err
	})
	if err != nil {
		t.Fatalf("seeding watchlist: %v", err)
	}
}

// RecomputeShowStatus is the single writer of user_show_states, and everything
// downstream — the show page's label, the bulk mark's answer, the eventual
// home view — is only as correct as it is. These tests drive it directly,
// without a service, so a failure points at the SQL and not at a caller.

const (
	StatusToday = "2026-08-18"
	StatusNow   = "2026-08-18T12:00:00Z"
)

// SeedShow inserts a catalog show and returns its id.
func SeedShow(t *testing.T, database *db.DB, tmdbID int64, title string) int64 {
	t.Helper()
	var row common.ShowRow
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		var err error
		row, err = common.UpsertShowByTMDBID(context.Background(), tx, common.ShowRow{
			TMDBID:       tmdbID,
			Title:        title,
			MetadataJSON: `{"external_ids": {}, "images": {}}`,
			FetchedAt:    "2026-08-18T00:00:00Z",
		})
		return err
	})
	if err != nil {
		t.Fatalf("seeding show: %v", err)
	}
	return row.ID
}

// SeedEpisode inserts one episode. airDate nil means TMDB has none — which must
// never read as aired.
func SeedEpisode(t *testing.T, database *db.DB, showID int64, season, number int, airDate *string) int64 {
	t.Helper()
	var id int64
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		return tx.QueryRow(context.Background(),
			`INSERT INTO episodes (show_id, season_number, episode_number, title, air_date)
			 VALUES (?, ?, ?, ?, ?) RETURNING id`,
			showID, season, number, "E", airDate).Scan(&id)
	})
	if err != nil {
		t.Fatalf("seeding episode: %v", err)
	}
	return id
}

func SeedShowStatus(t *testing.T, database *db.DB, userID, showID int64, status string) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO user_show_states (user_id, show_id, status, updated_at)
			 VALUES (?, ?, ?, ?)`, userID, showID, status, "2026-08-01T00:00:00Z")
		return err
	})
	if err != nil {
		t.Fatalf("seeding show status: %v", err)
	}
}

// ShowStatus reads the stored status, or "" when there is no row at all — the
// two are different states and the collapse tests turn on the difference.
func ShowStatus(t *testing.T, database *db.DB, userID, showID int64) string {
	t.Helper()
	var status string
	err := database.ReadRow(context.Background(),
		`SELECT status FROM user_show_states WHERE user_id = ? AND show_id = ?`,
		userID, showID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("reading show status: %v", err)
	}
	return status
}

func Recompute(t *testing.T, database *db.DB, userID, showID int64) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		return common.RecomputeShowStatus(context.Background(), tx, userID, showID, StatusToday, StatusNow)
	})
	if err != nil {
		t.Fatalf("recomputeShowStatus: %v", err)
	}
}
