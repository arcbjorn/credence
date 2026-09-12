package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/arcbjorn/credence/gateway/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// docs/04 requires WAL and foreign keys. A database without them would
// silently accept orphaned evidence and lose crash durability.
func TestPragmasEnforced(t *testing.T) {
	s := open(t)
	mode, fk, err := s.PragmaCheck(context.Background())
	if err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %s, want wal", mode)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestSchemaIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twice.db")
	ctx := context.Background()

	s1, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s1.Close()

	s2, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen must not fail on existing schema: %v", err)
	}
	s2.Close()
}

// docs/05: one strictly increasing sequence per binding across both kinds.
// The uniqueness constraint makes a duplicate a storage error, not a
// silently accepted second record.
func TestSequenceUniquePerBinding(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	seedBinding(t, s)

	insert := func(id string, seq int) error {
		_, err := s.DB().ExecContext(ctx,
			`INSERT INTO assessments (assessment_id, binding_key, input_hash, model_hash,
			 policy_hash, assessment_kind, decision, risk_permission, sequence,
			 assessment_hash, created_at, payload)
			 VALUES (?, 'b1', 'snap1', 'm', 'p', 'Origination', 'Approved', 'Allowed', ?, 'h', 1, x'00')`,
			id, seq)
		return err
	}
	if err := insert("a1", 1); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := insert("a2", 1); err == nil {
		t.Fatal("duplicate sequence for a binding must be rejected")
	}
	if err := insert("a3", 2); err != nil {
		t.Fatalf("increasing sequence must be accepted: %v", err)
	}
}

// Foreign keys must actually be enforced, not merely declared.
func TestForeignKeysRejectOrphans(t *testing.T) {
	s := open(t)
	_, err := s.DB().ExecContext(context.Background(),
		`INSERT INTO evidence_edges (child_evidence_id, parent_evidence_id, method_version)
		 VALUES ('missing-child', 'missing-parent', 'v1')`)
	if err == nil {
		t.Fatal("orphaned lineage edge must be rejected")
	}
}

// docs/04: origin is constrained, so an acquisition failure cannot relabel
// observed evidence as synthetic through a stray write.
func TestOriginIsConstrained(t *testing.T) {
	s := open(t)
	_, err := s.DB().ExecContext(context.Background(),
		`INSERT INTO observations (evidence_id, source, origin, state, coverage,
		 retrieved_at, content_hash, payload)
		 VALUES ('e1', 'perpl', 'Invented', 'Present', 'Complete', 1, 'h', x'00')`)
	if err == nil {
		t.Fatal("unknown origin must be rejected")
	}
}

func TestTxRollsBackOnError(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	seedBinding(t, s)

	wantErr := context.Canceled
	err := s.Tx(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx,
			`INSERT INTO models (model_hash, model_id, created_at, params)
			 VALUES ('m9', 'v1', 1, x'00')`); e != nil {
			return e
		}
		return wantErr
	})
	if err != wantErr {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}

	var n int
	s.DB().QueryRowContext(ctx, `SELECT count(*) FROM models WHERE model_hash='m9'`).Scan(&n)
	if n != 0 {
		t.Fatal("failed transaction must not persist rows")
	}
}

func seedBinding(t *testing.T, s *store.Store) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.DB().ExecContext(ctx,
		`INSERT INTO agents (agent_key, identity_registry_chain_id, identity_registry, agent_id, created_at)
		 VALUES ('ag1', 10143, '0xreg', '184', 1)`); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx,
		`INSERT INTO bindings (binding_key, agent_key, operator, credit_account, facility_id,
		 config_version, config_hash, policy_epoch, policy_hash, created_at)
		 VALUES ('b1', 'ag1', '0xop', '0xacct', '1', 1, 'ch', 1, 'ph', 1)`); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx,
		`INSERT INTO snapshots (input_hash, binding_key, schema_version, created_at, payload)
		 VALUES ('snap1', 'b1', 'v1', 1, x'00')`); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
}
