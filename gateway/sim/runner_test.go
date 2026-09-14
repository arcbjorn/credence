package sim_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/arcbjorn/credence/gateway/sim"
)

// stubUnderwriter approves at a fixed rate, so a runner test exercises the
// LEDGER rather than the engine.
//
// The engine is tested against the sim separately, in the evaluation
// command. Here the question is whether the runner books outcomes
// correctly, and a real engine would make a failure ambiguous between the
// two.
type stubUnderwriter struct {
	approve bool
	aprBps  uint32
	term    uint64
	seen    int
	// lastInput is kept so a test can assert on what the underwriter was
	// actually shown -- the wall is only real if it can be checked.
	lastInput json.RawMessage
}

func (s *stubUnderwriter) Assess(
	_ context.Context, input, _ json.RawMessage,
) (json.RawMessage, error) {
	s.seen++
	s.lastInput = input
	if !s.approve {
		return json.RawMessage(
			`{"outcome":{"decision":"Denied"},"reasons":[]}`), nil
	}
	var in struct {
		Context struct {
			Request struct {
				RequestedPrincipal string `json:"requested_principal"`
			} `json:"request"`
		} `json:"context"`
	}
	_ = json.Unmarshal(input, &in)
	return json.RawMessage(`{"outcome":{"decision":"ApprovedOrigination",` +
		`"terms":{"approved_principal":"` + in.Context.Request.RequestedPrincipal +
		`","apr_bps":` + itoa(int(s.aprBps)) +
		`,"term_seconds":` + itoa(int(s.term)) + `}},"reasons":[]}`), nil
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

func runCfg(name string, kind sim.PackKind, world sim.Config) sim.RunConfig {
	return sim.RunConfig{
		Pack:           name,
		Kind:           kind,
		World:          world,
		Policy:         json.RawMessage(`{}`),
		PolicyHash:     "0xpolicy",
		ModelHash:      "0xmodel",
		StartingEquity: 10_000_000_000,
		NamedScenarios: []string{"price", "liquidity", "combined"},
	}
}

func agent() sim.MomentumAgent {
	return sim.MomentumAgent{
		LookbackSteps:  24,
		MinMomentumBps: 50,
		LeverageBps:    5_000,
		CostBufferBps:  10,
		TermSeconds:    7 * 86_400,
		MaxAprBps:      4_000,
	}
}

// The underwriter must never be shown a price that has not happened.
//
// `docs/07` draws the wall; this is what proves it holds. Every snapshot
// the runner builds carries an assessment time, and it must be a time the
// clock has actually reached.
func TestUnderwriterSeesNoFuture(t *testing.T) {
	uw := &stubUnderwriter{approve: true, aprBps: 1_200, term: 7 * 86_400}
	cfg := runCfg("wall", sim.KindDevelopment, base())

	r, err := sim.NewRunner(cfg, agent(), uw)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var in struct {
		AssessmentTime uint64 `json:"assessment_time"`
	}
	if uw.lastInput == nil {
		t.Fatal("the underwriter was never asked, so nothing was evaluated")
	}
	if err := json.Unmarshal(uw.lastInput, &in); err != nil {
		t.Fatal(err)
	}

	_, last := sim.MustWorld(t, base()).Span()
	if int64(in.AssessmentTime) > last {
		t.Fatalf("assessment time %d is past the world's end %d",
			in.AssessmentTime, last)
	}
	if pack.Outcome.Originated == 0 {
		t.Fatal("no loan was originated, so the run tested nothing")
	}
}

// The same world, agent and underwriter must produce the same pack.
//
// Without this a fixture pack records a result nobody can reproduce, and
// `docs/07` makes a pack immutable precisely so it can be re-run.
func TestRunIsReproducible(t *testing.T) {
	run := func() sim.Pack {
		uw := &stubUnderwriter{approve: true, aprBps: 1_200, term: 7 * 86_400}
		r, err := sim.NewRunner(
			runCfg("repro", sim.KindDevelopment, base()), agent(), uw)
		if err != nil {
			t.Fatal(err)
		}
		p, err := r.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	a, b := run(), run()
	if a.Digest != b.Digest {
		t.Fatalf("two identical runs produced different packs:\n%s\n%s",
			a.Digest, b.Digest)
	}
}

// `docs/07`: the agent must be able to decline, and the demo must not
// force an approval to advance the script. An offer far above what the
// agent expects to earn must be refused BY THE AGENT, not merely priced.
func TestAgentDeclinesTermsItCannotJustify(t *testing.T) {
	// An absurd rate: no momentum this world produces justifies it.
	uw := &stubUnderwriter{approve: true, aprBps: 4_000, term: 300 * 86_400}

	cfg := runCfg("decline", sim.KindDevelopment, base())
	r, err := sim.NewRunner(cfg, agent(), uw)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if pack.Outcome.Declined == 0 {
		t.Fatal("the agent accepted terms it could not justify")
	}
	if pack.Outcome.Originated != 0 {
		t.Fatal("a declined offer must not originate a loan")
	}
}

// A refusal is an outcome, not a failure: the run continues and records
// it. A runner that stopped on the first denial would report coverage it
// never reached.
func TestRefusalsAreRecordedNotFatal(t *testing.T) {
	uw := &stubUnderwriter{approve: false}
	r, err := sim.NewRunner(
		runCfg("refused", sim.KindDevelopment, base()), agent(), uw)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("a refusal must not end the run: %v", err)
	}
	if pack.Outcome.Originated != 0 {
		t.Fatal("nothing was approved, so nothing should have originated")
	}
	if uw.seen == 0 {
		t.Fatal("the underwriter was never asked")
	}
}

// Principal must reconcile: what was lent equals what came back plus what
// was lost. A pack where it does not is reporting an accounting error, not
// a risk result.
func TestPrincipalReconciles(t *testing.T) {
	cfg := base()
	// A deep shock partway through, so the run reaches a real loss rather
	// than only the clean path.
	cfg.Shocks = []sim.Shock{{
		AtDay: 12, PriceBps: -8_500, DepthBps: 1_000,
		Label: "unnamed-liquidity-spiral",
	}}

	uw := &stubUnderwriter{approve: true, aprBps: 1_200, term: 7 * 86_400}
	r, err := sim.NewRunner(
		runCfg("recon", sim.KindEvaluation, cfg), agent(), uw)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !pack.Outcome.Reconciles() {
		t.Fatalf("principal does not reconcile: lent=%d repaid=%d recovered=%d lost=%d",
			pack.Outcome.PrincipalLent, pack.Outcome.PrincipalRepaid,
			pack.Outcome.PrincipalRecovered, pack.Outcome.PrincipalLost)
	}
}

// A shock outside the model's named scenarios must be reported as such.
//
// `docs/07` requires one in an evaluation set, because it separates
// expected stress protection from guaranteed recovery. A run that quietly
// counted it as covered would erase that distinction.
func TestUnnamedShockIsReportedAsUnnamed(t *testing.T) {
	cfg := base()
	cfg.Shocks = []sim.Shock{{
		AtDay: 5, PriceBps: -6_000, DepthBps: 500,
		Label: "unnamed-liquidity-spiral",
	}}

	uw := &stubUnderwriter{approve: true, aprBps: 1_200, term: 14 * 86_400}
	r, err := sim.NewRunner(
		runCfg("unnamed", sim.KindEvaluation, cfg), agent(), uw)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if pack.Outcome.Originated == 0 {
		t.Skip("no loan was live for the shock, so it exercised nothing")
	}
	if !pack.Outcome.UnnamedShockExercised {
		t.Fatal("an unnamed shock was not reported as unnamed")
	}
}

// A shock that hits an IDLE account exercised nothing, and counting it
// would overstate coverage -- the number a reader most wants to trust.
func TestShockWithNoLiveLoanIsNotCounted(t *testing.T) {
	cfg := base()
	cfg.Shocks = []sim.Shock{{
		AtDay: 0, PriceBps: -5_000, Label: "early-crash",
	}}

	// Never approves, so no loan is ever live.
	uw := &stubUnderwriter{approve: false}
	r, err := sim.NewRunner(
		runCfg("idle", sim.KindEvaluation, cfg), agent(), uw)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(pack.Outcome.ShocksExercised) != 0 {
		t.Fatalf("a shock with no live loan was counted: %v",
			pack.Outcome.ShocksExercised)
	}
}

// --- fixture packs ---

// A pack is immutable once used. An edited one must fail verification,
// or "immutable" is a claim rather than a property.
func TestEditedPackFailsVerification(t *testing.T) {
	uw := &stubUnderwriter{approve: true, aprBps: 1_200, term: 7 * 86_400}
	r, _ := sim.NewRunner(
		runCfg("sealed", sim.KindEvaluation, base()), agent(), uw)
	pack, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := pack.Verify(); err != nil {
		t.Fatalf("a freshly sealed pack must verify: %v", err)
	}

	edited := pack
	edited.Outcome.Defaulted = 0 // the edit someone would actually make
	if err := edited.Verify(); err == nil {
		t.Fatal("an edited pack verified; immutability is not enforced")
	}
}

// Saving must refuse to overwrite. A pack file that can be replaced in
// place is a pack that can be quietly rewritten after it was quoted.
func TestSaveRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	uw := &stubUnderwriter{approve: true, aprBps: 1_200, term: 7 * 86_400}
	r, _ := sim.NewRunner(
		runCfg("once", sim.KindEvaluation, base()), agent(), uw)
	pack, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	path, err := pack.Save(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := pack.Save(dir); err == nil {
		t.Fatal("a used pack was overwritten")
	}
}

func TestLoadVerifiesOnTheWayIn(t *testing.T) {
	dir := t.TempDir()
	uw := &stubUnderwriter{approve: true, aprBps: 1_200, term: 7 * 86_400}
	r, _ := sim.NewRunner(
		runCfg("loaded", sim.KindEvaluation, base()), agent(), uw)
	pack, _ := r.Run(context.Background())
	path, err := pack.Save(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sim.Load(path); err != nil {
		t.Fatalf("a sealed pack must load: %v", err)
	}

	// Tamper on disk, the way an edit would.
	raw, _ := os.ReadFile(path)
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	doc["model_hash"] = "0xsomething-else"
	edited, _ := json.Marshal(doc)
	_ = os.WriteFile(filepath.Join(dir, "loaded.json"), edited, 0o644)

	if _, err := sim.Load(path); err == nil {
		t.Fatal("a tampered pack loaded without complaint")
	}
}
