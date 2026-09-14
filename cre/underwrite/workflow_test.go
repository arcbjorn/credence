package main

import (
	"testing"

	"github.com/smartcontractkit/cre-sdk-go/capabilities/scheduler/cron"
	"github.com/smartcontractkit/cre-sdk-go/cre/testutils"
	"github.com/stretchr/testify/require"
)

func validConfig() *Config {
	return &Config{
		Schedule:        "0 */5 * * * *",
		RiskServiceURL:  "https://risk.credence.invalid",
		ChainSelector:   2183018362218727504,
		ReceiverAddress: "0x0000000000000000000000000000000000000001",
	}
}

func TestInitWorkflowRegistersTheCronTrigger(t *testing.T) {
	runtime := testutils.NewRuntime(t, testutils.Secrets{})

	workflow, err := InitWorkflow(validConfig(), runtime.Logger(), nil)
	require.NoError(t, err)
	require.Len(t, workflow, 1)
	require.Equal(t,
		cron.Trigger(&cron.Config{}).CapabilityID(),
		workflow[0].CapabilityID())
}

// A workflow missing the address it writes to, or the engine it asks,
// must refuse to start. Starting anyway produces a workflow that runs on
// schedule, reaches consensus on nothing, and writes nowhere -- which
// looks healthy in every log while no monitoring update reaches chain.
func TestInitWorkflowRefusesIncompleteConfig(t *testing.T) {
	runtime := testutils.NewRuntime(t, testutils.Secrets{})

	for _, c := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"no schedule", func(c *Config) { c.Schedule = "" }},
		{"no risk service", func(c *Config) { c.RiskServiceURL = "" }},
		{"no receiver", func(c *Config) { c.ReceiverAddress = "" }},
		// The zero address is the config template's placeholder, not an
		// address. An empty-string check passes it, and the workflow would
		// then write reports into nothing -- succeeding, at a cost, with
		// no permission reaching the oracle. `config.production.json`
		// shipped with exactly this value.
		{"zero receiver", func(c *Config) {
			c.ReceiverAddress = "0x0000000000000000000000000000000000000000"
		}},
		// Not an address at all. `common.HexToAddress` does not error --
		// it returns the zero address for unparseable input, so garbage
		// silently becomes the same failure as above.
		{"unparseable receiver", func(c *Config) {
			c.ReceiverAddress = "not-an-address"
		}},
		{"short receiver", func(c *Config) { c.ReceiverAddress = "0xdeadbeef" }},
		// A write needs a chain to land on.
		{"no chain selector", func(c *Config) { c.ChainSelector = 0 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			config := validConfig()
			c.mutate(config)
			_, err := InitWorkflow(config, runtime.Logger(), nil)
			require.Error(t, err, "an incomplete workflow must not start")
		})
	}
}

// `docs/04`: a decision and a permission are separate fields but not
// independent ones. A Denied result claiming Allowed is a contradiction,
// and the oracle rejects it -- catching it here keeps the failure
// attributable to the engine rather than surfacing far away.
func TestContradictoryResultIsRefused(t *testing.T) {
	require.Error(t, checkAssessment(&assessmentResponse{
		Decision:   "Denied",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
	}))
}

// An assessment with no evidence class cannot be acted on: `docs/05`
// invariant 13 pins the executable class per deployment and compares it by
// exact match, so an empty class is not a permissive default.
func TestMissingClassIsRefused(t *testing.T) {
	require.Error(t, checkAssessment(&assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "",
	}))
}

func TestWellFormedAssessmentPasses(t *testing.T) {
	require.NoError(t, checkAssessment(&assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
	}))

	// A denial that says Blocked is coherent, and is a real result: it
	// withdraws permission rather than failing.
	require.NoError(t, checkAssessment(&assessmentResponse{
		Decision:   "Denied",
		Permission: "Blocked",
		Class:      "PrototypeEligible",
	}))
}

// --- review findings, 14 September ---

// An assessment that has already expired must not become a fresh
// permission.
//
// The engine bounds an executable assessment by its oldest required
// evidence deadline (`docs/04`): permission may not outlive the evidence
// behind it. The workflow built its own window from runtime time plus
// `validitySeconds` and never consulted that bound, so an answer that
// expired long ago became an Allowed report valid for another hour. The
// on-chain expiry check cannot recover a bound that was discarded before
// signing.
func TestExpiredAssessmentIsRefused(t *testing.T) {
	a := &assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
		ValidAfter: 900,
		ValidUntil: 1000,
	}
	// Now is well past the engine's bound.
	if err := checkFreshness(a, 2000); err == nil {
		t.Fatal("an assessment expired at 1000 was accepted at 2000")
	}
}

// A live assessment still passes, or the check has broken the service.
func TestLiveAssessmentPassesFreshness(t *testing.T) {
	a := &assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
		ValidAfter: 900,
		ValidUntil: 5000,
	}
	if err := checkFreshness(a, 1000); err != nil {
		t.Fatalf("a live assessment was refused: %v", err)
	}
}

// An assessment must not be used before it is valid.
func TestNotYetValidAssessmentIsRefused(t *testing.T) {
	a := &assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
		ValidAfter: 5000,
		ValidUntil: 9000,
	}
	if err := checkFreshness(a, 1000); err == nil {
		t.Fatal("an assessment valid from 5000 was accepted at 1000")
	}
}

// An answer about one loan must not become another loan's permission.
//
// The endpoint returns `loan_id`, but the workflow's response type did not
// decode it and report construction used the REQUESTED id instead. A
// cached, misrouted or simply wrong response naming loan 2 therefore
// became an Allowed report for enrolled loan 1, and the receiver then
// substituted loan 1's enrolled binding. The opaque assessment and input
// hashes do not establish which binding the answer was about.
func TestAnswerForAnotherLoanIsRefused(t *testing.T) {
	a := &assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
		LoanID:     2,
		ValidAfter: 900,
		ValidUntil: 5000,
	}
	if err := checkLoan(a, LoanRef{LoanID: 1}); err == nil {
		t.Fatal("an answer about loan 2 was accepted as loan 1's permission")
	}
}

// The matching case still passes.
func TestAnswerForTheRequestedLoanPasses(t *testing.T) {
	a := &assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
		LoanID:     1,
		ValidAfter: 900,
		ValidUntil: 5000,
	}
	if err := checkLoan(a, LoanRef{LoanID: 1}); err != nil {
		t.Fatalf("an answer about the requested loan was refused: %v", err)
	}
}

// An answer that names no loan at all must be refused rather than assumed
// to be about the requested one -- that assumption is the defect.
func TestAnswerWithNoLoanIdIsRefused(t *testing.T) {
	a := &assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
		ValidAfter: 900,
		ValidUntil: 5000,
	}
	if err := checkLoan(a, LoanRef{LoanID: 1}); err == nil {
		t.Fatal("an answer naming no loan was accepted")
	}
}

// The report must never authorize past the engine's own bound.
//
// The configured window is a ceiling, not a replacement: whichever comes
// first ends the authorization. A workflow that always used
// `now + validitySeconds` would extend an assessment beyond the evidence
// that supports it, which `docs/04` forbids and which no on-chain check
// can undo.
func TestReportNeverOutlivesTheEngineBound(t *testing.T) {
	a := &assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
		LoanID:     1,
		ValidAfter: 900,
		// The engine says this dies at 1500.
		ValidUntil: 1500,
		ModelHash:  "0x" + hex64('a'),
		InputHash:  "0x" + hex64('b'),
		Evidence:   "0x" + hex64('c'),
		Assessment: "0x" + hex64('d'),
	}

	// The configuration would happily grant an hour.
	r, err := buildReport(LoanRef{LoanID: 1}, a, 1, 1000, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if r.validUntil != 1500 {
		t.Fatalf("validUntil = %d, want the engine's 1500", r.validUntil)
	}
}

// When the engine's bound is the looser of the two, the configured
// window still applies -- the rule is "whichever comes first", not
// "always the engine".
func TestConfiguredWindowStillCapsALongerBound(t *testing.T) {
	a := &assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
		LoanID:     1,
		ValidAfter: 900,
		ValidUntil: 999_999,
		ModelHash:  "0x" + hex64('a'),
		InputHash:  "0x" + hex64('b'),
		Evidence:   "0x" + hex64('c'),
		Assessment: "0x" + hex64('d'),
	}
	r, err := buildReport(LoanRef{LoanID: 1}, a, 1, 1000, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if r.validUntil != 1000+3600 {
		t.Fatalf("validUntil = %d, want the configured 4600", r.validUntil)
	}
}

func hex64(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
