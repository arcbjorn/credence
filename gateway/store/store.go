// Package store owns the gateway's SQLite database.
//
// `docs/04`: the Go gateway is the only writer. WAL, foreign keys,
// migrations, and an explicit busy timeout are required. Source evidence
// and assessment snapshots are retained — a hash without retrievable
// evidence is insufficient for replay.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Store struct {
	db *sql.DB
}

// Open prepares the database and applies the schema.
//
// Busy timeout is set so a concurrent reader never fails the writer with
// SQLITE_BUSY under WAL; single-writer discipline is enforced by design,
// not by hoping contention never happens.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One writer. More connections would allow concurrent writes, which is
	// exactly what the single-writer rule forbids.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate applies changes CREATE TABLE IF NOT EXISTS cannot make to a
// database that already exists.
//
// Each step is written to succeed on a fresh database too, so there is one
// code path rather than a fresh-install branch that is never exercised.
func migrate(ctx context.Context, db *sql.DB) error {
	// A column added after the first release. Defaults to zero, which
	// reads as "coverage unproven" -- the conservative value for a
	// binding registered before indexing was tracked.
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE bindings ADD COLUMN indexed_from INTEGER NOT NULL DEFAULT 0`,
	); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migrate bindings.indexed_from: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

// Tx runs fn inside a transaction, rolling back on error.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// PragmaCheck reports the pragmas that must hold for correctness.
func (s *Store) PragmaCheck(ctx context.Context) (journalMode string, foreignKeys int, err error) {
	if err = s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys)
	return
}
