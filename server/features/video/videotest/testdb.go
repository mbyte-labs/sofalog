package videotest

import (
	"context"
	"path/filepath"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/migrations"
)

// ptr builds a pointer to a literal in one expression; the seed helpers and
// nullable-field assertions across the feature's tests all use it.
func Ptr[T any](v T) *T { return &v }

// DB opens a real temp-file SQLite database with the real migrations
// applied. Service and store tests run against actual SQL — there are no
// store interfaces to mock (see the go-backend skill).
// WatchRegion is the watch region every test Service is built with, so a
// fixture's "watch/providers" section has one country to be projected from.
const WatchRegion = "US"

// LinkRedirectURI is the TMDB link callback every test Service is built
// with, and Cookie the handler's cookie attributes (D-029).
const LinkRedirectURI = "http://sofalog.test/api/v1/video/tmdb/link/callback"

var Cookie = common.CookieSettings{Path: "/"}

func DB(t *testing.T) *db.DB {
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
