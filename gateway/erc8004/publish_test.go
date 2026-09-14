package erc8004

import (
	"math/big"
	"testing"
)

const (
	testChain   = uint64(10143)
	testManager = "0x7354894D550A26D1d367bF13016DaE1ffb2dEe47"
)

// The evidence locator must still be specific to the deployment.
// `docs/06` records that the old `keccak("loan-N")` reference was not
// evidence because the same loan number under a different manager
// produced the same value.
func TestLoanURIIsSpecificToTheDeployment(t *testing.T) {
	uri := LoanURI("", testChain, testManager, 3)
	other := "0x0000000000000000000000000000000000000001"
	if uri == LoanURI("", 31337, testManager, 3) {
		t.Error("chain id must change the locator")
	}
	if uri == LoanURI("", testChain, other, 3) {
		t.Error("manager must change the locator")
	}
	if uri == LoanURI("", testChain, testManager, 4) {
		t.Error("loan id must change the locator")
	}
}

func outcome(id uint64) LoanOutcome {
	return LoanOutcome{
		AgentID:       big.NewInt(1830),
		LoanID:        new(big.Int).SetUint64(id),
		Outcome:       Repaid,
		Principal:     big.NewInt(4_000_000_000),
		InterestPaid:  big.NewInt(6_575_358),
		PrincipalLoss: big.NewInt(0),
		HeldSeconds:   45,
		ChainID:       testChain,
		Manager:       testManager,
	}
}

// The registry does NOT dedupe: feedbackIndex auto-increments, so a
// retried queue would publish the same outcome twice and double-count it.
// `docs/06` requires queued updates to be idempotent, and with no on-chain
// guard that has to hold here.
func TestPlanSkipsAlreadyPublished(t *testing.T) {
	// Fully published: every signal of loans 1 and 3 is already present.
	full := Published{}
	for _, id := range []uint64{1, 3} {
		entries, err := Signals(outcome(id))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			full.Mark(id, e.Tag1)
		}
	}

	plans, err := Plan(
		[]LoanOutcome{outcome(1), outcome(2), outcome(3)},
		full,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1", len(plans))
	}
	if plans[0].LoanID != 2 {
		t.Fatalf("planned loan %d, want the unpublished 2", plans[0].LoanID)
	}
}

// Every signal for one loan is published together or not at all: a partial
// set would leave a reader summing three of four facts.
func TestPlanCarriesEverySignal(t *testing.T) {
	plans, err := Plan([]LoanOutcome{outcome(1)}, Published{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1", len(plans))
	}
	if n := len(plans[0].Entries); n != 4 {
		t.Fatalf("loan planned with %d entries, want 4 signals", n)
	}
}

func TestPlanRefusesUnusableOutcome(t *testing.T) {
	bad := outcome(1)
	bad.Outcome = 42
	if _, err := Plan([]LoanOutcome{bad}, Published{}); err == nil {
		t.Fatal("expected refusal for an unknown outcome class")
	}
}

// A partly published loan resumes at the missing signals.
//
// Each loan writes four independent transactions. Readback used to mark
// the whole loan published on finding ANY of them, so a run that sent the
// outcome and then stopped -- a failure, a kill, a restart -- left the
// interest, loss and held-duration rows unwritten permanently. A reader
// would then sum three of four facts and never know.
//
// Reported by review, 14 September.
func TestPlanResumesAPartlyPublishedLoan(t *testing.T) {
	entries, err := Signals(outcome(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected several signals, got %d", len(entries))
	}

	// Only the FIRST signal landed.
	partial := Published{}
	partial.Mark(1, entries[0].Tag1)

	plans, err := Plan([]LoanOutcome{outcome(1)}, partial)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want the loan resumed", len(plans))
	}
	if got, want := len(plans[0].Entries), len(entries)-1; got != want {
		t.Fatalf("planned %d entries, want the %d still missing", got, want)
	}
	for _, e := range plans[0].Entries {
		if e.Tag1 == entries[0].Tag1 {
			t.Fatalf("re-sent an already published signal %q", e.Tag1)
		}
	}
}

// A loan with no signals published yet still plans all of them.
func TestPlanCarriesEverySignalWhenNothingPublished(t *testing.T) {
	entries, err := Signals(outcome(1))
	if err != nil {
		t.Fatal(err)
	}
	plans, err := Plan([]LoanOutcome{outcome(1)}, Published{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Entries) != len(entries) {
		t.Fatalf("expected all %d signals planned", len(entries))
	}
}
