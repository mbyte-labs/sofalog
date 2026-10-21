package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func migFS(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

func tableExists(t *testing.T, d *DB, name string) bool {
	t.Helper()
	var n int
	err := d.ReadRow(context.Background(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	if err != nil {
		t.Fatalf("checking table %s: %v", name, err)
	}
	return n > 0
}

func TestMigrate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	twoMigrations := map[string]string{
		"0001_first.sql":  `CREATE TABLE a (id INTEGER PRIMARY KEY); CREATE TABLE b (id INTEGER PRIMARY KEY);`,
		"0002_second.sql": `CREATE TABLE c (id INTEGER PRIMARY KEY);`,
	}

	t.Run("empty dir is a no-op", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		n, err := Migrate(ctx, d, migFS(nil))
		if err != nil || n != 0 {
			t.Fatalf("Migrate = (%d, %v), want (0, nil)", n, err)
		}
		if !tableExists(t, d, "schema_migrations") {
			t.Error("schema_migrations not created")
		}
	})

	t.Run("applies in order and records versions", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		n, err := Migrate(ctx, d, migFS(twoMigrations))
		if err != nil || n != 2 {
			t.Fatalf("Migrate = (%d, %v), want (2, nil)", n, err)
		}
		for _, table := range []string{"a", "b", "c"} {
			if !tableExists(t, d, table) {
				t.Errorf("table %s missing", table)
			}
		}
	})

	t.Run("re-run is idempotent", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		if _, err := Migrate(ctx, d, migFS(twoMigrations)); err != nil {
			t.Fatal(err)
		}
		n, err := Migrate(ctx, d, migFS(twoMigrations))
		if err != nil || n != 0 {
			t.Fatalf("second Migrate = (%d, %v), want (0, nil)", n, err)
		}
	})

	t.Run("only new files applied on upgrade", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		if _, err := Migrate(ctx, d, migFS(map[string]string{
			"0001_first.sql": twoMigrations["0001_first.sql"],
		})); err != nil {
			t.Fatal(err)
		}
		n, err := Migrate(ctx, d, migFS(twoMigrations))
		if err != nil || n != 1 {
			t.Fatalf("upgrade Migrate = (%d, %v), want (1, nil)", n, err)
		}
	})

	t.Run("failing statement rolls the whole file back", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		_, err := Migrate(ctx, d, migFS(map[string]string{
			"0001_broken.sql": `CREATE TABLE ok (id INTEGER PRIMARY KEY); THIS IS NOT SQL;`,
		}))
		if err == nil {
			t.Fatal("want error from broken migration")
		}
		if tableExists(t, d, "ok") {
			t.Error("table from failed migration exists — file was not applied atomically")
		}
		var n int
		if err := d.ReadRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 0 {
			t.Errorf("schema_migrations rows = %d (err %v), want 0", n, err)
		}
	})

	t.Run("recorded migration missing from fs fails", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		if _, err := Migrate(ctx, d, migFS(twoMigrations)); err != nil {
			t.Fatal(err)
		}
		_, err := Migrate(ctx, d, migFS(map[string]string{
			"0002_second.sql": twoMigrations["0002_second.sql"],
		}))
		if err == nil || !strings.Contains(err.Error(), "missing from the binary") {
			t.Fatalf("want missing-migration error, got %v", err)
		}
	})

	t.Run("new file sorting before applied ones fails", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		if _, err := Migrate(ctx, d, migFS(map[string]string{
			"0002_second.sql": twoMigrations["0002_second.sql"],
		})); err != nil {
			t.Fatal(err)
		}
		_, err := Migrate(ctx, d, migFS(twoMigrations))
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("want append-only error, got %v", err)
		}
	})

	t.Run("bad filename rejected", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		_, err := Migrate(ctx, d, migFS(map[string]string{"init.sql": `SELECT 1;`}))
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("want filename error, got %v", err)
		}
	})

	t.Run("dotfiles ignored", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		fsys := migFS(nil)
		fsys[".gitkeep"] = &fstest.MapFile{Data: nil}
		n, err := Migrate(ctx, d, fsys)
		if err != nil || n != 0 {
			t.Fatalf("Migrate = (%d, %v), want (0, nil)", n, err)
		}
	})
	t.Run("edited migration fails instead of being skipped", func(t *testing.T) {
		t.Parallel()
		d := openTestDB(t)
		if _, err := Migrate(ctx, d, migFS(map[string]string{
			"0001_first.sql": `CREATE TABLE a (id INTEGER PRIMARY KEY);`,
		})); err != nil {
			t.Fatal(err)
		}
		// The pre-deploy baseline workflow: the same file grows a table.
		_, err := Migrate(ctx, d, migFS(map[string]string{
			"0001_first.sql": `CREATE TABLE a (id INTEGER PRIMARY KEY); CREATE TABLE b (id INTEGER PRIMARY KEY);`,
		}))
		if err == nil || !strings.Contains(err.Error(), "has changed since it was applied") {
			t.Fatalf("want changed-migration error, got %v", err)
		}
		if tableExists(t, d, "b") {
			t.Error("table b exists — the edited file must not be re-applied")
		}
	})
}
