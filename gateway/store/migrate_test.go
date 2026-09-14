package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/arcbjorn/credence/gateway/store"
	_ "modernc.org/sqlite"
)

// A database written by an earlier release must still open.
//
// The schema creates an index over `accepted_at`. Applying it before the
// migration that ADDS that column fails on a column that does not exist
// yet — and since the failure returns early, the migration never runs and
// the database is permanently unopenable. An operator's retained evidence
// would be stranded by an upgrade.
//
// The fixture is the real schema from the release before `accepted_at`,
// not a hand-written approximation.
func TestLegacyDatabaseUpgrades(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy, err := os.ReadFile("testdata/legacy_schema.sql")
	if err != nil {
		t.Fatalf("read legacy schema: %v", err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := raw.ExecContext(ctx, string(legacy)); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	// The fixture must genuinely predate the column, or this test proves
	// nothing about upgrading.
	var n int
	if err := raw.QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_table_info('assessments') WHERE name = 'accepted_at'`,
	).Scan(&n); err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if n != 0 {
		t.Fatal("the fixture already has accepted_at; it is not a legacy schema")
	}
	raw.Close()

	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("a database from an earlier release must open: %v", err)
	}
	defer db.Close()

	// And must arrive fully migrated.
	if err := db.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM pragma_table_info('assessments') WHERE name = 'accepted_at'`,
	).Scan(&n); err != nil {
		t.Fatalf("inspect upgraded: %v", err)
	}
	if n != 1 {
		t.Fatal("accepted_at must exist after the upgrade")
	}

	if err := db.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master
		 WHERE type = 'index' AND name = 'idx_assessments_accepted_sequence'`,
	).Scan(&n); err != nil {
		t.Fatalf("inspect index: %v", err)
	}
	if n != 1 {
		t.Fatal("the accepted-sequence index must exist after the upgrade")
	}
}

// A fresh database must still open, since the migrations now run first
// against tables that do not exist yet.
func TestFreshDatabaseOpens(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("a fresh database must open: %v", err)
	}
	db.Close()
}
