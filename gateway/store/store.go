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
	"errors"
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

	// Migrations run BEFORE the schema, not after. The schema creates an
	// index over `accepted_at`, so on a database written by an earlier
	// release it fails on a column the migration is what adds -- leaving
	// the migration unreachable and the database permanently unopenable.
	//
	// Each migration below is a no-op on a fresh database, where the
	// tables it would alter do not exist yet and the schema creates them
	// correctly a moment later.
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
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
	); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") &&
		!strings.Contains(err.Error(), "no such table") {
		return fmt.Errorf("migrate bindings.indexed_from: %w", err)
	}
	// The assessments table once carried UNIQUE(binding_key, sequence),
	// which reserved a sequence for an evaluation that may never have
	// been published. Rebuild it so the constraint covers only ACCEPTED
	// assessments; SQLite cannot drop a table constraint in place.
	if err := migrateAssessmentSequence(ctx, db); err != nil {
		return err
	}
	return nil
}

// migrateAssessmentSequence rebuilds `assessments` when it still carries
// the old table-level uniqueness.
func migrateAssessmentSequence(ctx context.Context, db *sql.DB) error {
	var ddl string
	err := db.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'assessments'`,
	).Scan(&ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect assessments: %w", err)
	}
	if !strings.Contains(ddl, "UNIQUE (binding_key, sequence)") {
		return nil
	}

	// Copy through a new table. Existing rows were all published under
	// the old rule, so they are marked accepted.
	stmts := []string{
		`ALTER TABLE assessments RENAME TO assessments_old`,
		`CREATE TABLE assessments (
			assessment_id   TEXT PRIMARY KEY,
			binding_key     TEXT    NOT NULL REFERENCES bindings(binding_key),
			input_hash      TEXT    NOT NULL REFERENCES snapshots(input_hash),
			model_hash      TEXT    NOT NULL,
			policy_hash     TEXT    NOT NULL,
			assessment_kind TEXT    NOT NULL CHECK (assessment_kind IN ('Origination','Monitoring')),
			decision        TEXT    NOT NULL CHECK (decision IN ('Approved','Denied','Unavailable')),
			risk_permission TEXT    NOT NULL CHECK (risk_permission IN ('Allowed','Blocked')),
			sequence        INTEGER NOT NULL,
			assessment_hash TEXT    NOT NULL,
			created_at      INTEGER NOT NULL,
			payload         BLOB    NOT NULL,
			accepted_at     INTEGER
		)`,
		`INSERT INTO assessments
		   (assessment_id, binding_key, input_hash, model_hash, policy_hash,
		    assessment_kind, decision, risk_permission, sequence,
		    assessment_hash, created_at, payload, accepted_at)
		 SELECT assessment_id, binding_key, input_hash, model_hash, policy_hash,
		        assessment_kind, decision, risk_permission, sequence,
		        assessment_hash, created_at, payload, created_at
		 FROM assessments_old`,
		`DROP TABLE assessments_old`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_assessments_accepted_sequence
		    ON assessments(binding_key, sequence) WHERE accepted_at IS NOT NULL`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migrate assessments: %w", err)
		}
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
