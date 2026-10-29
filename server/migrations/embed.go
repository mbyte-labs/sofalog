// Package migrations embeds the numbered SQL migration files that db.Migrate
// applies at startup. Files live in sql/ and are named NNNN_snake_case.sql.
//
// Pre-deploy there is exactly one — the baseline sql/0001_init.sql — and it is
// edited in place; append-only takes over at the first deployed release, marked
// by creating the FROZEN file next to this one (D-016).
package migrations

import (
	"embed"
	"io/fs"
)

// The all: prefix keeps dotfiles embedded; the runner skips them.
//
//go:embed all:sql
var files embed.FS

// FS returns the migration files with sql/ as the root. FROZEN lives outside
// sql/, so the runner never sees it as a migration.
func FS() fs.FS {
	sub, err := fs.Sub(files, "sql")
	if err != nil {
		panic(err) // impossible: "sql" is embedded above
	}
	return sub
}
