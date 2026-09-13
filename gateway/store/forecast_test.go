package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/arcbjorn/credence/gateway/store"
)

func forecastFixture(id string, probPPM uint64) store.Forecast {
	payload, _ := json.Marshal(map[string]any{
		"event":           "NAV drawdown > 5% within 20 minutes",
		"probability_ppm": probPPM,
	})
	return store.Forecast{
		ForecastID:      id,
		BindingKey:      "b1",
		EventDefinition: "nav_drawdown_5pct_20m",
		ProbabilityPPM:  probPPM,
		HorizonSeconds:  1_200,
		CommittedAt:     1_000,
		Payload:         payload,
	}
}

// Brier loss is (p - y)^2, so a confident correct call scores near zero
// and a confident wrong one scores near the maximum. These are the values
// the forecast penalty caps capacity from, so they must be exact rather
// than merely ordered.
func TestBrierScoresAreExact(t *testing.T) {
	d, r, _ := derived(t)
	seedBindingFor(t, r, "b1")
	ctx := context.Background()

	cases := []struct {
		name       string
		probPPM    uint64
		resolution store.Resolution
		wantBrier  uint64
	}{
		// Certain and right: (1.0 - 1.0)^2 = 0
		{"certain correct", 1_000_000, store.Occurred, 0},
		// Certain and wrong: (1.0 - 0.0)^2 = 1.0
		{"certain wrong", 1_000_000, store.DidNotOccur, 1_000_000},
		// Maximally uncertain: (0.5 - 1.0)^2 = 0.25
		{"coin flip", 500_000, store.Occurred, 250_000},
		// Confident and right: (0.9 - 1.0)^2 = 0.01
		{"confident correct", 900_000, store.Occurred, 10_000},
		// Confident and wrong: (0.1 - 1.0)^2 = 0.81
		{"confident wrong", 100_000, store.Occurred, 810_000},
	}

	for i, c := range cases {
		id := c.name
		if err := r.CommitForecast(ctx, forecastFixture(id, c.probPPM)); err != nil {
			t.Fatalf("%s: commit: %v", c.name, err)
		}
		if err := r.ResolveForecast(
			ctx, "o"+id, id, c.resolution, 2_200, nil,
		); err != nil {
			t.Fatalf("%s: resolve: %v", c.name, err)
		}
		_ = i
	}

	// The aggregate must be the mean of the five scores.
	fc := forecastsOf(t, d, "b1")
	if fc.ComparableResolved != uint32(len(cases)) {
		t.Fatalf("expected %d resolved, got %d", len(cases), fc.ComparableResolved)
	}
	var sum uint64
	for _, c := range cases {
		sum += c.wantBrier
	}
	want := sum / uint64(len(cases))
	if fc.MeanBrierPPM != want {
		t.Fatalf("mean brier: got %d, want %d", fc.MeanBrierPPM, want)
	}
}

// An unobservable outcome carries no score. `docs/06`: unobservable
// outcomes receive no favourable treatment, and scoring one as correct
// would credit an agent for an event nobody could check.
func TestUnobservableOutcomeIsNotScored(t *testing.T) {
	d, r, s := derived(t)
	seedBindingFor(t, r, "b1")
	ctx := context.Background()

	if err := r.CommitForecast(ctx, forecastFixture("f1", 900_000)); err != nil {
		t.Fatal(err)
	}
	if err := r.ResolveForecast(
		ctx, "o1", "f1", store.NotObservable, 2_200, nil,
	); err != nil {
		t.Fatal(err)
	}

	fc := forecastsOf(t, d, "b1")
	if fc.ComparableResolved != 0 {
		t.Fatalf("an unobservable outcome must not count as resolved, got %d",
			fc.ComparableResolved)
	}
	if fc.Unobservable != 1 {
		t.Fatalf("it must be reported as unobservable, got %d", fc.Unobservable)
	}

	// No score at all, not a score of zero. A zero would read as a
	// perfect forecast if the resolution filter ever widened.
	var brier any
	if err := s.DB().QueryRowContext(ctx,
		`SELECT brier_ppm FROM outcomes WHERE outcome_id = ?`, "o1",
	).Scan(&brier); err != nil {
		t.Fatalf("read brier: %v", err)
	}
	if brier != nil {
		t.Fatalf("an unobservable outcome must carry no score, got %v", brier)
	}
}

// A committed forecast with no outcome yet is unresolved, not absent. The
// engine takes the maximum penalty on insufficient resolved outcomes, so
// an uncounted commitment would silently look like a clean record.
func TestUnresolvedForecastIsCounted(t *testing.T) {
	d, r, _ := derived(t)
	seedBindingFor(t, r, "b1")
	ctx := context.Background()

	if err := r.CommitForecast(ctx, forecastFixture("f1", 300_000)); err != nil {
		t.Fatal(err)
	}

	fc := forecastsOf(t, d, "b1")
	if fc.Unresolved != 1 {
		t.Fatalf("expected 1 unresolved, got %d", fc.Unresolved)
	}
	if fc.ComparableResolved != 0 {
		t.Fatalf("nothing is resolved yet, got %d", fc.ComparableResolved)
	}
}

// The first commitment is the evidence. Re-committing must not rewrite it,
// or a forecast could be revised once its outcome became visible.
func TestRecommitDoesNotOverwrite(t *testing.T) {
	d, r, _ := derived(t)
	seedBindingFor(t, r, "b1")
	ctx := context.Background()

	if err := r.CommitForecast(ctx, forecastFixture("f1", 100_000)); err != nil {
		t.Fatal(err)
	}
	// A second commitment claiming near-certainty, after the fact.
	if err := r.CommitForecast(ctx, forecastFixture("f1", 990_000)); err != nil {
		t.Fatal(err)
	}
	if err := r.ResolveForecast(ctx, "o1", "f1", store.Occurred, 2_200, nil); err != nil {
		t.Fatal(err)
	}

	// Scored against the ORIGINAL 0.1, so (0.1-1.0)^2 = 0.81.
	fc := forecastsOf(t, d, "b1")
	if fc.MeanBrierPPM != 810_000 {
		t.Fatalf("must score the original commitment: got %d, want 810000",
			fc.MeanBrierPPM)
	}
}
