// Package db opens the database, owns the pragmas and pools, runs
// migrations, and provides transactions. It is the ONLY package in the
// module that may import a database driver.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

// DB holds two pools. SQLite allows exactly one writer, so the write pool is
// capped at a single connection to avoid SQLITE_BUSY under concurrent writes;
// reads go through a separate multi-connection pool (fine under WAL).
type DB struct {
	write *sql.DB
	read  *sql.DB
}

// Open opens the SQLite database at path, creating it if absent.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := "file:" + url.PathEscape(path) +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)"

	write, err := open(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite write pool: %w", err)
	}
	write.SetMaxOpenConns(1)

	read, err := open(ctx, dsn)
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("opening sqlite read pool: %w", err)
	}
	read.SetMaxOpenConns(4)

	return &DB{write: write, read: read}, nil
}

func open(ctx context.Context, dsn string) (*sql.DB, error) {
	pool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func (d *DB) Close() error {
	rerr := d.read.Close()
	if werr := d.write.Close(); werr != nil {
		return werr
	}
	return rerr
}

// Tx is the handle store functions receive. Its methods rebind placeholders,
// so store code always writes `?` and never `$1`.
type Tx struct {
	tx *sql.Tx
}

func (t Tx) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, rebind(query), args...)
}

func (t Tx) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, rebind(query), args...)
}

func (t Tx) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, rebind(query), args...)
}

// InTx runs fn in one transaction on the write pool, rolling back on error or
// panic. Handlers never see a transaction; service code owns the boundary.
func (d *DB) InTx(ctx context.Context, fn func(tx Tx) error) error {
	tx, err := d.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if err := fn(Tx{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// Read runs read-only queries outside a transaction, on the read pool.
func (d *DB) Read(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.read.QueryContext(ctx, rebind(query), args...)
}

// ReadRow is Read for single-row queries.
func (d *DB) ReadRow(ctx context.Context, query string, args ...any) *sql.Row {
	return d.read.QueryRowContext(ctx, rebind(query), args...)
}

// rebind is the single seam where `?` placeholders will be rewritten to `$n`
// when the Postgres path lands. On SQLite it is the identity.
func rebind(query string) string {
	return query
}
