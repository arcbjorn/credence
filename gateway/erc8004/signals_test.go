package erc8004

import (
	"math/big"
	"testing"
)

func defaulted() LoanOutcome {
	return LoanOutcome{
		AgentID:       big.NewInt(184),
		LoanID:        big.NewInt(1),
		Outcome:       Defaulted,
		Principal:     big.NewInt(4_000_000_000),
		InterestPaid:  big.NewInt(0),
		PrincipalLoss: big.NewInt(2_600_000_000),
		HeldSeconds:   432_001,
		EvidenceURI:   "https://example.invalid/loan/1.json",
		ChainID:       testChain,
		Manager:       testManager,
	}
}

// `docs/06`: a repayment, a profit, a forecast and an absence of default
// are separate signals and must not be combined. The registry stores one
// value per entry, so separation means separate ENTRIES -- a single entry
// carrying a blended score would satisfy the ABI and break the rule.
func TestSignalsStaySeparate(t *testing.T) {
	got, err := Signals(defaulted())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d entries, want 4 separate signals", len(got))
	}
	seen := map[string]bool{}
	for _, f := range got {
		if seen[f.Tag1] {
			t.Fatalf("tag %s published twice: signals must not merge", f.Tag1)
		}
		seen[f.Tag1] = true
	}
	for _, want := range []string{
		TagOutcome + "." + OutcomeDefaulted, TagInterest, TagLoss, TagHeld,
	} {
		if !seen[want] {
			t.Errorf("missing signal %s", want)
		}
	}
}

// A realised loss must publish as a negative value. The registry's value
// is signed for exactly this reason; publishing 2_600_000_000 unsigned
// would read as a gain of the same size to anyone summing the tag.
func TestLossPublishesNegative(t *testing.T) {
	got, err := Signals(defaulted())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		if f.Tag1 != TagLoss {
			continue
		}
		if f.Value.Sign() >= 0 {
			t.Fatalf("loss published as %s, want negative", f.Value)
		}
		if f.Value.Cmp(big.NewInt(-2_600_000_000)) != 0 {
			t.Fatalf("loss = %s, want -2600000000", f.Value)
		}
		return
	}
	t.Fatal("no loss signal")
}

// Every entry must point at the same retained record, or the four rows
// are four disconnected claims rather than one documented outcome.
func TestEntriesShareEvidence(t *testing.T) {
	o := defaulted()
	o.EvidenceHash = [32]byte{1, 2, 3}
	got, err := Signals(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		if f.FeedbackURI != o.EvidenceURI {
			t.Errorf("%s has uri %q, want %q", f.Tag1, f.FeedbackURI, o.EvidenceURI)
		}
		if f.FeedbackHash != o.EvidenceHash {
			t.Errorf("%s has a different evidence hash", f.Tag1)
		}
	}
}

// A repaid loan reports zero loss, not a missing loss entry: absence of a
// row and a row reading zero are different facts to a reader, and only
// one of them is a statement Credence made.
func TestRepaidStillPublishesZeroLoss(t *testing.T) {
	o := defaulted()
	o.Outcome = Repaid
	o.PrincipalLoss = big.NewInt(0)
	o.InterestPaid = big.NewInt(6_575_358)
	got, err := Signals(o)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range got {
		if f.Tag1 != TagLoss {
			continue
		}
		found = true
		if f.Value.Sign() != 0 {
			t.Fatalf("repaid loan reports loss %s, want 0", f.Value)
		}
		if f.Tag2 != LoanTag(testChain, testManager, 1) {
			t.Fatalf("loss tagged %q, want %q",
				f.Tag2, LoanTag(testChain, testManager, 1))
		}
	}
	if !found {
		t.Fatal("repaid loan published no loss signal")
	}
}

// An outcome class the mapping does not recognise must be refused.
// Publishing it under a known tag would miscount every reader filtering
// on that tag.
func TestUnknownOutcomeRefused(t *testing.T) {
	o := defaulted()
	o.Outcome = 99
	if _, err := Signals(o); err == nil {
		t.Fatal("expected refusal for unknown outcome class")
	}
}

func TestMissingAmountsRefused(t *testing.T) {
	o := defaulted()
	o.PrincipalLoss = nil
	if _, err := Signals(o); err == nil {
		t.Fatal("expected refusal when an amount is missing")
	}
}

// The loan id must be recoverable from a field the registry RETURNS.
// feedbackURI and feedbackHash are not: no view exposes them, so keying
// idempotency on them would need a log scan a public RPC will not serve
// cheaply. Every entry therefore carries `loan:N` in tag2.
func TestEveryEntryCarriesTheLoanIDInTag2(t *testing.T) {
	o := defaulted()
	o.LoanID = big.NewInt(7)
	got, err := Signals(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		id, ok := ParseLoanTag(f.Tag2, testChain, testManager)
		if !ok {
			t.Fatalf("%s: tag2 %q does not identify a loan", f.Tag1, f.Tag2)
		}
		if id != 7 {
			t.Fatalf("%s: tag2 names loan %d, want 7", f.Tag1, id)
		}
	}
}

// The outcome class rides in tag1 so `credence.outcome.defaulted` selects
// every default in one filtered call.
func TestOutcomeClassIsFilterable(t *testing.T) {
	for _, c := range []struct {
		outcome uint8
		want    string
	}{
		{Repaid, TagOutcome + ".repaid"},
		{Recovered, TagOutcome + ".recovered"},
		{Defaulted, TagOutcome + ".defaulted"},
	} {
		o := defaulted()
		o.Outcome = c.outcome
		got, err := Signals(o)
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Tag1 != c.want {
			t.Errorf("outcome %d tagged %q, want %q", c.outcome, got[0].Tag1, c.want)
		}
	}
}

func TestParseLoanTagRejectsForeignTags(t *testing.T) {
	tag := DeploymentTag(testChain, testManager)
	for _, bad := range []string{"", "loan:", "loan:x", "crosscheck", "pass",
		"loan:" + tag + ":", "loan:" + tag + ":x",
		"loan:" + tag + ":18446744073709551616",
		// Another deployment's loan 1 is a different obligation.
		"loan:" + DeploymentTag(31337, testManager) + ":1",
		"loan:1"} {
		if id, ok := ParseLoanTag(bad, testChain, testManager); ok {
			t.Errorf("%q parsed as loan %d, want refusal", bad, id)
		}
	}
}

// The tag must be specific to the deployment. Loan ids restart at 1 for
// every manager, and because idempotency keys on this tag, a collision
// does not merely confuse a reader -- it makes the second deployment's
// loan 1 permanently unpublishable. `docs/06` records the same defect
// against the old keccak("loan-N") evidence reference.
func TestLoanTagIsScopedToTheDeployment(t *testing.T) {
	mine := LoanTag(testChain, testManager, 1)
	if mine == LoanTag(31337, testManager, 1) {
		t.Error("chain id must change the tag")
	}
	other := "0x0000000000000000000000000000000000000001"
	if mine == LoanTag(testChain, other, 1) {
		t.Error("manager must change the tag")
	}
	if _, ok := ParseLoanTag(mine, 31337, testManager); ok {
		t.Error("another chain's tag must not parse as ours")
	}
	if _, ok := ParseLoanTag(mine, testChain, other); ok {
		t.Error("another manager's tag must not parse as ours")
	}
}

// The independent reader recomputes this digest in Python with the
// standard library alone. If the two ever disagree the reader silently
// reports no obligations, so the expected value is pinned here.
//
//	python3 -c "import hashlib;print(hashlib.sha256(
//	  b'10143:0xdbc452dd0c1f2ff6e40465f04ea4d5dd33527ba2').hexdigest()[:8])"
func TestDeploymentTagMatchesTheReader(t *testing.T) {
	got := DeploymentTag(10143, "0xdbC452DD0c1f2fF6e40465f04EA4D5Dd33527bA2")
	if got != "a70ec896" {
		t.Fatalf("DeploymentTag = %s, want a70ec896 (the reader's value)", got)
	}
}
