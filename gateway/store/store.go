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
	return &Store{db: db}, nil
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
