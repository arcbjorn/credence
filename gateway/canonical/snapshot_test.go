package canonical_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/arcbjorn/credence/gateway/canonical"
)

// The cross-language claim in docs/04 is that Go and Rust produce identical
// snapshot hashes. Rust emits this vector from its own RiskInput type; Go
// must reach the same input_hash by re-encoding the structured input with
// its own encoder. If either side drifts, this fails.
func TestSnapshotHashMatchesRust(t *testing.T) {
	raw, err := os.ReadFile("../../schemas/v1/snapshot-vector.json")
	if err != nil {
		t.Fatalf("read snapshot vector: %v", err)
	}
	var doc struct {
		Canonical string          `json:"canonical"`
		InputHash string          `json:"input_hash"`
		Input     json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse vector: %v", err)
	}

	// 1. Go's encoder, run over the structured input, must reproduce the
	//    canonical bytes Rust produced.
	val, err := canonical.DecodeStrict(doc.Input)
	if err != nil {
		t.Fatalf("decode input: %v", err)
	}
	enc, err := canonical.Encode(val)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(enc) != doc.Canonical {
		t.Fatalf("canonical bytes differ\n  rust: %s\n  go:   %s", doc.Canonical, enc)
	}

	// 2. And the hash over those bytes must match.
	sum := sha256.Sum256(enc)
	if got := hex.EncodeToString(sum[:]); got != doc.InputHash {
		t.Fatalf("input_hash differs\n  rust: %s\n  go:   %s", doc.InputHash, got)
	}
}

// Hashing the canonical bytes Rust emitted, byte for byte, must also match —
// isolating encoder agreement from hash agreement.
func TestHashOfRustBytesMatches(t *testing.T) {
	raw, _ := os.ReadFile("../../schemas/v1/snapshot-vector.json")
	var doc struct {
		Canonical string `json:"canonical"`
		InputHash string `json:"input_hash"`
	}
	json.Unmarshal(raw, &doc)

	sum := sha256.Sum256([]byte(doc.Canonical))
	if got := hex.EncodeToString(sum[:]); got != doc.InputHash {
		t.Fatalf("hash of rust bytes differs: %s vs %s", got, doc.InputHash)
	}
}
