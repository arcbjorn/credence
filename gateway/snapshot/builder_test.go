package snapshot_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arcbjorn/credence/gateway/snapshot"
)

func fixturePath(name string) string {
	return filepath.Join("..", "..", "fixtures", "performer", name)
}

// Rebuilds the committed fixture through the Go builder. If Go's assembly
// drifts from Rust's representation, the hash changes and replay breaks —
// so this is the test that keeps the two sides speaking the same language.
func loadFixtureAsInput(t *testing.T) snapshot.Input {
	t.Helper()
	raw, err := os.ReadFile(fixturePath("input.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var in snapshot.Input
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return in
}

// The Go builder must reproduce the exact input_hash Rust computed for the
// same snapshot.
func TestInputHashMatchesRustFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v1", "snapshot-vector.json"))
	if err != nil {
		t.Skipf("snapshot vector unavailable: %v", err)
	}
	var vector struct {
		InputHash string          `json:"input_hash"`
		Input     json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatalf("parse vector: %v", err)
	}

	var in snapshot.Input
	if err := json.Unmarshal(vector.Input, &in); err != nil {
		t.Fatalf("unmarshal into builder type: %v", err)
	}

	got, err := in.InputHash()
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if got != vector.InputHash {
		t.Fatalf("input_hash drift\n  rust: %s\n  go:   %s", vector.InputHash, got)
	}
}

// A round trip through the Go type must not alter the canonical bytes —
// otherwise the gateway would persist a snapshot that no longer reproduces
// its own decision.
func TestRoundTripPreservesCanonicalBytes(t *testing.T) {
	in := loadFixtureAsInput(t)
	first, err := in.Canonical()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}

	var again snapshot.Input
	if err := json.Unmarshal(first, &again); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	second, err := again.Canonical()
	if err != nil {
		t.Fatalf("canonical again: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("canonical encoding is not stable across a round trip")
	}
}

// docs/04: an acquisition failure changes availability, not origin. The
// builder offers no way to substitute a value for a failed fetch.
func TestMissingPreservesOriginAndSuppliesNoValue(t *testing.T) {
	ev := snapshot.Missing("mkt", "perpl", snapshot.OriginObserved, snapshot.Required, 1000)

	if ev.Value != nil {
		t.Fatal("a failed fetch must not carry a value")
	}
	if ev.Origin != snapshot.OriginObserved {
		t.Fatalf("origin changed on failure: %s", ev.Origin)
	}
	if ev.State != snapshot.StateMissing {
		t.Fatalf("state = %s, want Missing", ev.State)
	}
	if ev.CoverageValue != snapshot.CoverageUnknown {
		t.Fatalf("coverage = %s, want Unknown", ev.CoverageValue)
	}
}

func TestPresentHashesItsValue(t *testing.T) {
	ev, err := snapshot.Present(
		"acct", "chain", snapshot.OriginObserved, snapshot.Required, 1000, 1000,
		map[string]any{"balance": "5000000000"},
	)
	if err != nil {
		t.Fatalf("present: %v", err)
	}
	if len(ev.ContentHash) != 64 {
		t.Fatalf("content hash = %q, want 64 hex chars", ev.ContentHash)
	}
	if ev.State != snapshot.StatePresent {
		t.Fatal("expected Present")
	}
}

// Floats never carry an authorization amount; hashing must refuse them.
func TestPresentRejectsFloatValues(t *testing.T) {
	_, err := snapshot.Present(
		"acct", "chain", snapshot.OriginObserved, snapshot.Required, 1000, 1000,
		map[string]any{"balance": 5000.5},
	)
	if err == nil {
		t.Fatal("a float value must not be hashable")
	}
}

// Catching this at assembly makes a misconfigured provider fail loudly
// rather than producing a snapshot the engine must reject.
func TestSyntheticInObservedFacilityIsRejected(t *testing.T) {
	in := loadFixtureAsInput(t)
	in.Market.Origin = snapshot.OriginSynthetic

	err := in.Validate()
	if err == nil {
		t.Fatal("synthetic evidence must be rejected in an observed facility")
	}
	if !strings.Contains(err.Error(), "synthetic") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScenarioFacilityRequiresScenarioID(t *testing.T) {
	in := loadFixtureAsInput(t)
	in.FacilityPolicy = "Scenario"
	in.ScenarioID = nil
	if err := in.Validate(); err == nil {
		t.Fatal("a scenario facility must name its scenario")
	}

	id := "run-1"
	in.ScenarioID = &id
	in.Market.Origin = snapshot.OriginSynthetic // permitted in a scenario
	if err := in.Validate(); err != nil {
		t.Fatalf("scenario facility with an id must validate: %v", err)
	}
}

func TestDuplicateEvidenceIDsRejected(t *testing.T) {
	in := loadFixtureAsInput(t)
	in.Market.EvidenceID = in.Account.EvidenceID
	if err := in.Validate(); err == nil {
		t.Fatal("duplicate evidence ids must be rejected")
	}
}

func TestValidityWindowIsChecked(t *testing.T) {
	in := loadFixtureAsInput(t)
	in.Proposal.ValidUntil = in.Proposal.ValidAfter
	if err := in.Validate(); err == nil {
		t.Fatal("an empty validity window must be rejected")
	}

	in = loadFixtureAsInput(t)
	in.AssessmentTime = in.Proposal.ValidUntil + 1
	if err := in.Validate(); err == nil {
		t.Fatal("assessment time outside the window must be rejected")
	}
}

// The builder reports what is blocking without deciding the outcome.
func TestBlockingEvidenceIsReportedNotDecided(t *testing.T) {
	in := loadFixtureAsInput(t)
	if got := in.BlockingEvidence(); len(got) != 0 {
		t.Fatalf("complete fixture should block nothing, got %v", got)
	}

	in.Market = snapshot.Missing("mkt", "perpl", snapshot.OriginObserved, snapshot.Required, 1000)
	blocking := in.BlockingEvidence()
	if len(blocking) != 1 || blocking[0] != "mkt" {
		t.Fatalf("expected [mkt], got %v", blocking)
	}
}

// Partial coverage blocks a required field: a response can be current and
// still omit the position that matters.
func TestPartialCoverageBlocks(t *testing.T) {
	in := loadFixtureAsInput(t)
	ev, err := snapshot.Partial(
		"mkt2", "perpl", snapshot.OriginObserved, snapshot.Required, 1000, 1000,
		map[string]any{"depth": "1"},
	)
	if err != nil {
		t.Fatalf("partial: %v", err)
	}
	in.Market = ev
	if got := in.BlockingEvidence(); len(got) != 1 {
		t.Fatalf("partial coverage must block, got %v", got)
	}
}

func TestSyntheticUsedIsSurfaced(t *testing.T) {
	in := loadFixtureAsInput(t)
	if in.SyntheticUsed() {
		t.Fatal("observed fixture must not report synthetic use")
	}
	in.History.Origin = snapshot.OriginSynthetic
	if !in.SyntheticUsed() {
		t.Fatal("synthetic input must be surfaced")
	}
}
