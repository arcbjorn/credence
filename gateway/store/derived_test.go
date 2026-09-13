package store_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/arcbjorn/credence/gateway/store"
)

func derived(t *testing.T) (*store.Derived, *store.Registry, *store.Store) {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return store.NewDerived(s), store.NewRegistry(s), s
}

func seedBindingFor(t *testing.T, r *store.Registry, key string) {
	t.Helper()
	ctx := context.Background()
	if err := r.RegisterAgent(ctx, store.Agent{
		AgentKey: "ag1", IdentityRegistry: "0xreg", AgentID: "184",
	}); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := r.RegisterBinding(ctx, store.Binding{
		BindingKey: key, AgentKey: "ag1", Operator: "0xop",
		CreditAccount: "0xacct", FacilityID: "1",
		ConfigHash: "0xc", PolicyHash: "0xp",
		// Indexed from the first block, so coverage is genuinely proven.
		IndexedFrom: 1,
	}); err != nil {
		t.Fatalf("binding: %v", err)
	}
}

// Registers a binding WITHOUT declaring where indexing started, which is
// the state of an account enrolled after it was already active.
func seedUnindexedBinding(t *testing.T, r *store.Registry, key string) {
	t.Helper()
	ctx := context.Background()
	if err := r.RegisterAgent(ctx, store.Agent{
		AgentKey: "ag1", IdentityRegistry: "0xreg", AgentID: "184",
	}); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := r.RegisterBinding(ctx, store.Binding{
		BindingKey: key, AgentKey: "ag1", Operator: "0xop",
		CreditAccount: "0xacct", FacilityID: "1",
		ConfigHash: "0xc", PolicyHash: "0xp",
	}); err != nil {
		t.Fatalf("binding: %v", err)
	}
}

func historyOf(t *testing.T, d *store.Derived, key string) store.HistorySummary {
	t.Helper()
	raw, err := d.History(context.Background(), key)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var h store.HistorySummary
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	return h
}

// The distinction docs/04 draws: an unknown binding is not an empty one.
// Returning a clean history here would grant every unregistered agent a
// spotless record.
func TestUnknownBindingHasNoHistory(t *testing.T) {
	d, _, _ := derived(t)
	if _, err := d.History(context.Background(), "never-seen"); err == nil {
		t.Fatal("an unknown binding must not yield a history")
	}
	if _, err := d.Forecasts(context.Background(), "never-seen"); err == nil {
		t.Fatal("an unknown binding must not yield forecasts")
	}
}

// An INDEXED binding with no loans has a verified empty history, which the
// engine treats as a new account rather than as missing evidence.
func TestIndexedBindingWithNoLoansIsVerifiedEmpty(t *testing.T) {
	d, r, _ := derived(t)
	seedBindingFor(t, r, "b1")

	h := historyOf(t, d, "b1")
	if !h.CoverageComplete {
		t.Fatal("an indexed binding with no loans has complete coverage")
	}
	if h.WeightedRepaymentsPPM != 0 || h.ResolvedDefaults != 0 {
		t.Fatalf("expected an empty history, got %+v", h)
	}
}

// Registration is not coverage.
//
// A binding enrolled after its account was already active has outcomes the
// gateway never saw. Reporting that as verified-clean would earn credit
// the agent never demonstrated (`docs/04`), so an undeclared indexing
// start must read as incomplete.
func TestUnindexedBindingHasIncompleteCoverage(t *testing.T) {
	d, r, _ := derived(t)
	seedUnindexedBinding(t, r, "b1")

	h := historyOf(t, d, "b1")
	if h.CoverageComplete {
		t.Fatal("registration alone must not assert complete coverage")
	}
}

func recordOutcome(t *testing.T, s *store.Store, event string, payload map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	_, err := s.DB().ExecContext(context.Background(),
		`INSERT INTO chain_events
		   (chain_id, tx_hash, log_index, block_number, block_hash,
		    contract, event_name, payload)
		 VALUES (0, ?, 0, 1, '', 'LoanManager', ?, ?)`,
		event+payload["binding_key"].(string)+payload["tag"].(string), event, raw)
	if err != nil {
		t.Fatalf("record outcome: %v", err)
	}
}

// docs/06: weight by exposure and duration, so cheap history earns little.
func TestRepaymentWeightScalesWithExposureAndDuration(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")

	// Full reference size held for the full window.
	recordOutcome(t, s, "Repaid", map[string]any{
		"binding_key": "b1", "tag": "1",
		"principal": 1_000_000, "reference_size": 1_000_000,
		"eligible_seconds": 30 * 86_400,
	})
	full := historyOf(t, d, "b1").WeightedRepaymentsPPM
	if full != 1_000_000 {
		t.Fatalf("full exposure for the full window should weigh 1_000_000 ppm, got %d", full)
	}
}

// The farming case the weighting exists to prevent.
func TestSameBlockRepaymentEarnsNothing(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")

	recordOutcome(t, s, "Repaid", map[string]any{
		"binding_key": "b1", "tag": "1",
		"principal": 1_000_000, "reference_size": 1_000_000,
		"eligible_seconds": 0,
	})
	if w := historyOf(t, d, "b1").WeightedRepaymentsPPM; w != 0 {
		t.Fatalf("an instant repayment must earn no weight, got %d", w)
	}
}

// Ten micro-loans must not approach one real one.
func TestManyTinyLoansDoNotMatchOneRealLoan(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")
	seedBindingFor2 := func(key string) {
		_ = r.RegisterBinding(context.Background(), store.Binding{
			BindingKey: key, AgentKey: "ag1", Operator: "0xop",
			CreditAccount: "0xacct", FacilityID: "1",
			ConfigHash: "0xc", PolicyHash: "0xp",
		})
	}
	seedBindingFor2("b2")

	for i := 0; i < 10; i++ {
		recordOutcome(t, s, "Repaid", map[string]any{
			"binding_key": "b1", "tag": string(rune('a' + i)),
			"principal": 1_000, "reference_size": 1_000_000,
			"eligible_seconds": 60,
		})
	}
	recordOutcome(t, s, "Repaid", map[string]any{
		"binding_key": "b2", "tag": "1",
		"principal": 1_000_000, "reference_size": 1_000_000,
		"eligible_seconds": 30 * 86_400,
	})

	tiny := historyOf(t, d, "b1").WeightedRepaymentsPPM
	real := historyOf(t, d, "b2").WeightedRepaymentsPPM
	if tiny >= real/100 {
		t.Fatalf("ten micro-loans (%d ppm) came too close to one real loan (%d ppm)", tiny, real)
	}
}

func TestDefaultsAndBreachesAreCounted(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")

	recordOutcome(t, s, "Defaulted", map[string]any{"binding_key": "b1", "tag": "1"})
	recordOutcome(t, s, "PolicyBreach", map[string]any{"binding_key": "b1", "tag": "2"})

	h := historyOf(t, d, "b1")
	if h.ResolvedDefaults != 1 || h.PolicyBreaches != 1 {
		t.Fatalf("outcomes not counted: %+v", h)
	}
}

// A malformed record makes coverage incomplete rather than being silently
// dropped, which would report a cleaner history than the data supports.
func TestMalformedRecordMarksCoverageIncomplete(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")

	recordOutcome(t, s, "Repaid", map[string]any{
		"binding_key": "b1", "tag": "1",
		"principal": 1_000, "reference_size": 0, // unusable
		"eligible_seconds": 60,
	})
	if historyOf(t, d, "b1").CoverageComplete {
		t.Fatal("a record that cannot be weighted must make coverage incomplete")
	}
}

func forecastsOf(t *testing.T, d *store.Derived, key string) store.ForecastSummary {
	t.Helper()
	raw, err := d.Forecasts(context.Background(), key)
	if err != nil {
		t.Fatalf("forecasts: %v", err)
	}
	var f store.ForecastSummary
	_ = json.Unmarshal(raw, &f)
	return f
}

func addForecast(t *testing.T, s *store.Store, id, key string) {
	t.Helper()
	_, err := s.DB().ExecContext(context.Background(),
		`INSERT INTO forecasts
		   (forecast_id, binding_key, event_definition, probability_ppm,
		    horizon_seconds, committed_at, payload)
		 VALUES (?, ?, 'drawdown', 120000, 1200, 1, x'')`, id, key)
	if err != nil {
		t.Fatalf("add forecast: %v", err)
	}
}

func resolveForecast(t *testing.T, s *store.Store, outcomeID, forecastID, resolution string, brier any) {
	t.Helper()
	_, err := s.DB().ExecContext(context.Background(),
		`INSERT INTO outcomes (outcome_id, forecast_id, resolution, resolved_at, brier_ppm, payload)
		 VALUES (?, ?, ?, 1, ?, x'')`, outcomeID, forecastID, resolution, brier)
	if err != nil {
		t.Fatalf("resolve forecast: %v", err)
	}
}

func TestResolvedForecastsAverageBrier(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")

	addForecast(t, s, "f1", "b1")
	addForecast(t, s, "f2", "b1")
	resolveForecast(t, s, "o1", "f1", "Occurred", 40_000)
	resolveForecast(t, s, "o2", "f2", "DidNotOccur", 60_000)

	f := forecastsOf(t, d, "b1")
	if f.ComparableResolved != 2 {
		t.Fatalf("resolved = %d, want 2", f.ComparableResolved)
	}
	if f.MeanBrierPPM != 50_000 {
		t.Fatalf("mean brier = %d, want 50000", f.MeanBrierPPM)
	}
}

// docs/03: insufficient eligible outcomes take the maximum penalty, so an
// unresolved commitment must never inflate the resolved count.
func TestUnresolvedForecastsAreCountedSeparately(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")

	addForecast(t, s, "f1", "b1")
	addForecast(t, s, "f2", "b1")
	resolveForecast(t, s, "o1", "f1", "Occurred", 40_000)

	f := forecastsOf(t, d, "b1")
	if f.ComparableResolved != 1 {
		t.Fatalf("resolved = %d, want 1", f.ComparableResolved)
	}
	if f.Unresolved != 1 {
		t.Fatalf("unresolved = %d, want 1", f.Unresolved)
	}
}

// An unobservable outcome earns no credit either.
func TestUnobservableOutcomesEarnNoCredit(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")

	addForecast(t, s, "f1", "b1")
	resolveForecast(t, s, "o1", "f1", "NotObservable", nil)

	f := forecastsOf(t, d, "b1")
	if f.ComparableResolved != 0 {
		t.Fatalf("an unobservable outcome must not count as resolved: %+v", f)
	}
	if f.Unobservable != 1 {
		t.Fatalf("unobservable = %d, want 1", f.Unobservable)
	}
}
