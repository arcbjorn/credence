package store_test

import (
	"context"
	"encoding/json"
	"testing"
)

// A retained evaluation that never published must not reserve its
// proposed sequence.
//
// Retention happens before publication (`docs/04` retains every decision,
// including those that fail to deliver). If a failed publish leaves the
// sequence taken, the next fresh snapshot at the same proposed sequence
// fails retention — and since retention failure returns before publishing,
// retries can never advance. The loan silently stops being monitored.
func TestUnpublishedSequenceDoesNotBlockReevaluation(t *testing.T) {
	_, r, s := derived(t)
	seedBindingFor(t, r, "b1")
	ctx := context.Background()

	snapshot := func(id string) []byte {
		raw, _ := json.Marshal(map[string]any{
			"context": map[string]any{"kind": "Monitoring"},
			"marker":  id,
		})
		return raw
	}
	assessment := func(id string) []byte {
		raw, _ := json.Marshal(map[string]any{
			"assessment_id": id,
			"model_hash":    "m",
			"policy_hash":   "p",
			"sequence":      7,
			"outcome":       map[string]any{"decision": "MonitoringBlocked"},
		})
		return raw
	}

	// First attempt: evaluated and retained, then publication fails.
	if err := r.Retain(ctx, "b1", "v1", "hash-a",
		snapshot("a"), assessment("aaa")); err != nil {
		t.Fatalf("first retain: %v", err)
	}

	// A fresh snapshot at the same proposed sequence, because the chain
	// never accepted the first one.
	if err := r.Retain(ctx, "b1", "v1", "hash-b",
		snapshot("b"), assessment("bbb")); err != nil {
		t.Fatalf("re-evaluation must retain, got: %v", err)
	}

	var n int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM assessments WHERE binding_key = ? AND sequence = 7`, "b1",
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("both attempts must be retained, got %d", n)
	}
}
