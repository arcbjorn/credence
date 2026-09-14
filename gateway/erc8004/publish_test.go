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
	plans, err := Plan(
		[]LoanOutcome{outcome(1), outcome(2), outcome(3)},
		Published{1: true, 3: true},
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
