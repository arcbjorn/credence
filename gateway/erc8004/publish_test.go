package erc8004

import (
	"math/big"
	"testing"
)

const (
	testChain   = uint64(10143)
	testManager = "0x7354894D550A26D1d367bF13016DaE1ffb2dEe47"
)

func TestLoanURIRoundTrips(t *testing.T) {
	for _, base := range []string{"", "https://example.invalid/evidence"} {
		uri := LoanURI(base, testChain, testManager, 3)
		got, ok := ParseLoanURI(uri, testChain, testManager)
		if !ok {
			t.Fatalf("base %q: %q did not parse", base, uri)
		}
		if got != 3 {
			t.Fatalf("base %q: got loan %d, want 3", base, got)
		}
	}
}

// `docs/06`: the old reference was not evidence because the same loan
// number under a different manager produced the same value. A locator that
// still collided across deployments would reintroduce that, and here it
// would also mark a loan published that never was.
func TestLoanURIIsSpecificToTheDeployment(t *testing.T) {
	uri := LoanURI("", testChain, testManager, 3)

	if _, ok := ParseLoanURI(uri, 31337, testManager); ok {
		t.Error("a URI from another chain must not parse as ours")
	}
	other := "0x0000000000000000000000000000000000000001"
	if _, ok := ParseLoanURI(uri, testChain, other); ok {
		t.Error("a URI from another manager must not parse as ours")
	}
	if uri == LoanURI("", 31337, testManager, 3) {
		t.Error("chain id must change the locator")
	}
	if uri == LoanURI("", testChain, other, 3) {
		t.Error("manager must change the locator")
	}
}

func TestParseLoanURIRejectsJunk(t *testing.T) {
	for _, bad := range []string{
		"",
		"credence:10143:" + testManager + ":loan:",
		"credence:10143:" + testManager + ":loan:abc",
		"credence:10143:" + testManager + ":loan:-1",
		"https://example.invalid/evidence/10143/" + testManager + "/loan/3.txt",
		// Would overflow uint64 and wrap to a small id, marking the
		// wrong loan published.
		"credence:10143:" + testManager + ":loan:18446744073709551616",
	} {
		if id, ok := ParseLoanURI(bad, testChain, testManager); ok {
			t.Errorf("%q parsed as loan %d, want refusal", bad, id)
		}
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
