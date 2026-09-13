package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/arcbjorn/credence/gateway/canonical"
)

// Retain stores the exact bytes a decision was made from and produced.
//
// `docs/04` requires replay from retained evidence, which means the
// canonical snapshot and assessment themselves -- not a row that could be
// re-serialized differently later, and not just their hashes. Hashes prove
// a replay matches; only the payloads make a replay possible at all.
//
// Idempotent: a cycle recorded twice must not fail and must not rewrite
// what is already there. The first write wins, because the retained bytes
// are the evidence, and silently replacing them would defeat keeping them.
func (r *Registry) Retain(
	ctx context.Context,
	bindingKey, schemaVersion, inputHash string,
	snapshot, assessment []byte,
) error {
	if inputHash == "" || len(snapshot) == 0 || len(assessment) == 0 {
		return fmt.Errorf("retain: refusing to store an incomplete record")
	}

	var doc struct {
		AssessmentID string `json:"assessment_id"`
		ModelHash    string `json:"model_hash"`
		PolicyHash   string `json:"policy_hash"`
		Sequence     uint64 `json:"sequence"`
		Outcome      struct {
			Decision string `json:"decision"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(assessment, &doc); err != nil {
		return fmt.Errorf("retain: decode assessment: %w", err)
	}

	decision, permission, err := classify(doc.Outcome.Decision)
	if err != nil {
		return err
	}

	// The kind comes from the snapshot's own context, not from the
	// outcome: Unavailable and Denied occur in both contexts, so inferring
	// the kind from the decision would mislabel every blocked monitoring
	// cycle as an origination.
	var ctxProbe struct {
		Context struct {
			Kind string `json:"kind"`
		} `json:"context"`
	}
	if err := json.Unmarshal(snapshot, &ctxProbe); err != nil {
		return fmt.Errorf("retain: decode context: %w", err)
	}
	kind := ctxProbe.Context.Kind
	if kind != "Origination" && kind != "Monitoring" {
		return fmt.Errorf("retain: unknown assessment kind %q", kind)
	}

	// The assessment hash is computed over the canonical form rather than
	// read from a field: the engine does not carry its own hash, and a
	// value invented here would not be what anyone verifies against.
	// DecodeStrict, not json.Unmarshal into `any`: a float would reach the
	// canonical encoder and be rejected, and decoding loosely here would
	// turn that into a silently different hash.
	decoded, err := canonical.DecodeStrict(assessment)
	if err != nil {
		return fmt.Errorf("retain: decode for hashing: %w", err)
	}
	assessmentHash, err := canonical.HashHex(decoded)
	if err != nil {
		return fmt.Errorf("retain: hash assessment: %w", err)
	}

	now := time.Now().Unix()
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("retain: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The snapshot goes first: assessments reference it by foreign key, so
	// the opposite order would fail on a fresh record.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO snapshots (input_hash, binding_key, schema_version, created_at, payload)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(input_hash) DO NOTHING`,
		inputHash, bindingKey, schemaVersion, now, snapshot); err != nil {
		return fmt.Errorf("retain snapshot: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO assessments
		   (assessment_id, binding_key, input_hash, model_hash, policy_hash,
		    assessment_kind, decision, risk_permission, sequence,
		    assessment_hash, created_at, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(assessment_id) DO NOTHING`,
		doc.AssessmentID, bindingKey, inputHash, doc.ModelHash, doc.PolicyHash,
		kind, decision, permission, doc.Sequence,
		assessmentHash, now, assessment); err != nil {
		return fmt.Errorf("retain assessment: %w", err)
	}

	return tx.Commit()
}

// classify maps an outcome tag to the decision and permission the schema
// records.
//
// Derived rather than read: the engine carries one tag, and both columns
// must agree with it. An unknown tag is an error, never a default, because
// a silently mislabelled row would misreport what was decided.
//
// Permission follows `docs/04`: only an approval grants Allowed.
func classify(tag string) (decision, permission string, err error) {
	switch tag {
	case "ApprovedOrigination", "MonitoringAllowed":
		return "Approved", "Allowed", nil
	case "Denied", "MonitoringBlocked":
		return "Denied", "Blocked", nil
	case "Unavailable":
		return "Unavailable", "Blocked", nil
	default:
		return "", "", fmt.Errorf("retain: unknown outcome %q", tag)
	}
}
