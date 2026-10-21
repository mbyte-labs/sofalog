package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"time"
)

// migrationName enforces the numbered convention: 0001_init.sql.
var migrationName = regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)

// appliedMigration is one recorded row of schema_migrations.
type appliedMigration struct {
	version  string
	checksum string
}

// Migrate applies every pending migration from fsys in filename order, each in
// its own transaction, and records it in schema_migrations. An empty fsys is a
// no-op. It returns the number of migrations applied.
//
// Three hard errors, all about the migration set changing under an existing
// database:
//
//   - a recorded migration missing from fsys;
//   - a new file that sorts before an already-applied one (once released,
//     migrations are append-only);
//   - an applied migration whose file contents have changed since.
//
// The third exists because sofalog is pre-deploy: the baseline schema is edited
// in place by design (D-016), and an already-recorded file is otherwise skipped
// silently, leaving a database that is missing the edit but looks healthy.
// Failing at startup turns that into one clear instruction.
func Migrate(ctx context.Context, d *DB, fsys fs.FS) (int, error) {
	const create = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL,
    checksum   TEXT NOT NULL DEFAULT ''
)`
	if _, err := d.write.ExecContext(ctx, create); err != nil {
		return 0, fmt.Errorf("creating schema_migrations: %w", err)
	}

	files, err := listMigrations(fsys)
	if err != nil {
		return 0, err
	}

	applied, err := appliedVersions(ctx, d)
	if err != nil {
		return 0, err
	}

	onDisk := make(map[string]bool, len(files))
	for _, f := range files {
		onDisk[f] = true
	}

	// applied is ordered by version, so the first offending file is reported
	// deterministically.
	appliedSet := make(map[string]bool, len(applied))
	lastApplied := ""
	for _, a := range applied {
		if !onDisk[a.version] {
			return 0, fmt.Errorf("migration %s is recorded as applied but missing from the binary — a released migration must never be removed or renamed; if you renamed the pre-deploy baseline, delete the database file and let it rebuild", a.version)
		}
		appliedSet[a.version] = true
		if a.version > lastApplied {
			lastApplied = a.version
		}
		if err := verifyUnchanged(fsys, a); err != nil {
			return 0, err
		}
	}

	count := 0
	for _, name := range files {
		if appliedSet[name] {
			continue
		}
		if name < lastApplied {
			return 0, fmt.Errorf("migration %s sorts before already-applied %s — migrations are append-only", name, lastApplied)
		}
		if err := applyOne(ctx, d, fsys, name); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// verifyUnchanged reports an error when an applied migration's file no longer
// matches what was recorded. An empty recorded checksum means the row predates
// checksumming and is accepted.
func verifyUnchanged(fsys fs.FS, a appliedMigration) error {
	if a.checksum == "" {
		return nil
	}
	body, err := fs.ReadFile(fsys, a.version)
	if err != nil {
		return fmt.Errorf("reading migration %s: %w", a.version, err)
	}
	if checksum(body) == a.checksum {
		return nil
	}
	return fmt.Errorf("migration %s has changed since it was applied, so this database is missing the change — pre-release, the baseline schema is edited in place, so delete the database file and let it rebuild; after release, restore the file and add a new numbered migration instead", a.version)
}

func checksum(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func listMigrations(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("reading migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name[0] == '.' {
			continue
		}
		if !migrationName.MatchString(name) {
			return nil, fmt.Errorf("migration filename %q does not match NNNN_snake_case.sql", name)
		}
		files = append(files, name)
	}
	slices.Sort(files)
	return files, nil
}

func appliedVersions(ctx context.Context, d *DB) ([]appliedMigration, error) {
	rows, err := d.write.QueryContext(ctx,
		`SELECT version, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	defer rows.Close()

	var versions []appliedMigration
	for rows.Next() {
		var a appliedMigration
		if err := rows.Scan(&a.version, &a.checksum); err != nil {
			return nil, err
		}
		versions = append(versions, a)
	}
	return versions, rows.Err()
}

// applyOne runs one migration file and its bookkeeping row in a single
// transaction, so a failing statement leaves nothing half-applied.
func applyOne(ctx context.Context, d *DB, fsys fs.FS, name string) error {
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("reading migration %s: %w", name, err)
	}
	err = d.InTx(ctx, func(tx Tx) error {
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version, applied_at, checksum) VALUES (?, ?, ?)`,
			name, time.Now().UTC().Format(time.RFC3339), checksum(body))
		return err
	})
	if err != nil {
		return fmt.Errorf("applying migration %s: %w", name, err)
	}
	return nil
}
