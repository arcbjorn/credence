package main

import (
	"testing"

	"github.com/arcbjorn/credence/gateway/store"
)

// resolveDrawdown is the decision the sampling loop makes, extracted so it
// can be tested without a chain.
//
// Three outcomes, not two. `docs/06` scores a forecast by what RESOLVED,
// and an unobservable moment must carry no score at all.
func resolveDrawdown(peak, trough uint64, seen bool, thresholdBps uint64) store.Resolution {
	if !seen {
		return store.NotObservable
	}
	if trough*10_000 < peak*(10_000-thresholdBps) {
		return store.Occurred
	}
	return store.DidNotOccur
}

// An unobservable sample must not score as "did not occur".
//
// `navSample` returned zero for both a failed read and a real zero, and
// the caller tested `navPeak > 0`. A transient RPC error therefore
// credited the agent with a correct forecast on a question nobody
// answered — and these resolutions feed the forecast evidence that later
// underwriting prices against.
//
// Reported by review, 15 September.
func TestUnobservableSampleIsNotScored(t *testing.T) {
	got := resolveDrawdown(0, 0, false, 500)
	if got != store.NotObservable {
		t.Fatalf("resolution = %s, want NotObservable", got)
	}
}

// An account genuinely wiped out is a REAL observation of zero, and the
// drawdown it forecast plainly occurred. Dropping it would hide the one
// reading a drawdown forecast most needs.
func TestInsolventAccountIsObservedNotDropped(t *testing.T) {
	got := resolveDrawdown(10_000, 0, true, 500)
	if got != store.Occurred {
		t.Fatalf("resolution = %s, want Occurred", got)
	}
}

func TestHealthyHoldDidNotOccur(t *testing.T) {
	// A 1% fall against a 5% threshold.
	got := resolveDrawdown(10_000, 9_900, true, 500)
	if got != store.DidNotOccur {
		t.Fatalf("resolution = %s, want DidNotOccur", got)
	}
}

func TestDrawdownBeyondThresholdOccurred(t *testing.T) {
	// A 6% fall against a 5% threshold.
	got := resolveDrawdown(10_000, 9_400, true, 500)
	if got != store.Occurred {
		t.Fatalf("resolution = %s, want Occurred", got)
	}
}
