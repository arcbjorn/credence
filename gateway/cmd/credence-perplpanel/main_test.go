package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/arcbjorn/credence/gateway/evidence"
)

// A minimal snapshot with the fields the substitution touches.
const snapshotFixture = `{
  "assessment_time": 1000,
  "schema_version": "v1",
  "account": {"state": "Present"},
  "proposal": {"valid_after": 900, "valid_until": 4600},
  "market": {
    "state": "Present",
    "origin": "Synthetic",
    "source": "declared",
    "observed_at": 1000,
    "retrieved_at": 1000,
    "content_hash": "stale-hash-describing-the-old-value",
    "value": {
      "observed_at": 1000,
      "realized_volatility_bps": 2000,
      "volatility_window_seconds": 86400,
      "liquidity_depth": "2000000000000"
    }
  }
}`

func fixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "snap.json")
	if err := os.WriteFile(p, []byte(snapshotFixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

func observed() evidence.MarketState {
	return evidence.MarketState{
		ObservedAt:              9000,
		RealizedVolatilityBps:   7,
		VolatilityWindowSeconds: 21600,
		LiquidityDepth:          "280566141730",
	}
}

func decode(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return m
}

// sameJSON compares two encodings by value rather than by bytes.
func sameJSON(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("decode a: %v", err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("decode b: %v", err)
	}
	ax, err := json.Marshal(x)
	if err != nil {
		t.Fatalf("encode a: %v", err)
	}
	by, err := json.Marshal(y)
	if err != nil {
		t.Fatalf("encode b: %v", err)
	}
	return string(ax) == string(by)
}

func marketOf(t *testing.T, snap json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	return decode(t, decode(t, snap)["market"])
}

// Observed numbers must carry observed PROVENANCE. A snapshot holding
// real Perpl values under `origin: "Synthetic"` would misreport where they
// came from, and origin is what the engine uses to decide whether a
// result may be executable at all.
func TestSubstitutionMarksTheEvidenceObserved(t *testing.T) {
	out, err := substituteMarket(fixture(t), observed())
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	m := marketOf(t, out)
	if string(m["origin"]) != `"Observed"` {
		t.Fatalf("origin must become Observed, got %s", m["origin"])
	}
	if string(m["source"]) != `"perpl"` {
		t.Fatalf("source must name the venue, got %s", m["source"])
	}
}

// The stored hash described the OLD value. Keeping it would hand the
// engine evidence whose hash describes different numbers; the field is
// required, so omitting it is not an option either.
func TestSubstitutionRecomputesTheContentHash(t *testing.T) {
	out, err := substituteMarket(fixture(t), observed())
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	m := marketOf(t, out)
	var got string
	if err := json.Unmarshal(m["content_hash"], &got); err != nil {
		t.Fatalf("content_hash: %v", err)
	}
	if got == "stale-hash-describing-the-old-value" {
		t.Fatal("the stale hash survived the substitution")
	}
	if got == "" {
		t.Fatal("content_hash is required and must not be blanked")
	}
}

// The engine enforces freshness against the snapshot's assessment time, so
// a retained fixture dated after the observation makes today's live
// reading look days stale and the whole panel renders Unavailable.
//
// Found by running it: the fixture was 8 days ahead of the live venue and
// the engine returned StaleRequired with age_seconds 710300.
func TestSubstitutionMovesTheClockToTheObservation(t *testing.T) {
	out, err := substituteMarket(fixture(t), observed())
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	var at uint64
	if err := json.Unmarshal(decode(t, out)["assessment_time"], &at); err != nil {
		t.Fatalf("assessment_time: %v", err)
	}
	if at != 9000 {
		t.Fatalf("expected the observation time 9000, got %d", at)
	}
}

// The proposal window keeps its LENGTH and moves its start, or the
// assessment authorizes a period that ended before it was made.
func TestSubstitutionMovesTheProposalWindow(t *testing.T) {
	out, err := substituteMarket(fixture(t), observed())
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	prop := decode(t, decode(t, out)["proposal"])
	var after, until uint64
	_ = json.Unmarshal(prop["valid_after"], &after)
	_ = json.Unmarshal(prop["valid_until"], &until)

	if after != 9000 {
		t.Fatalf("window must start at the observation, got %d", after)
	}
	// The fixture's span is 4600-900 = 3700.
	if until-after != 3700 {
		t.Fatalf("window length must be preserved, got %d", until-after)
	}
}

// Only the market block moves. If the account or portfolio changed too,
// the comparison would no longer isolate the market's effect.
func TestSubstitutionLeavesEveryOtherBlockAlone(t *testing.T) {
	out, err := substituteMarket(fixture(t), observed())
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	before := decode(t, json.RawMessage(snapshotFixture))
	after := decode(t, out)

	// Compared semantically, not byte for byte: re-encoding drops the
	// fixture's whitespace, which is not a modification.
	for _, block := range []string{"account", "schema_version", "proposal"} {
		if block == "proposal" {
			continue // the proposal window moves with the clock, by design
		}
		if !sameJSON(t, before[block], after[block]) {
			t.Fatalf("the %s block was modified: %s -> %s",
				block, before[block], after[block])
		}
	}
}

// The baseline exists to isolate the market's effect, so it must keep the
// DECLARED value while sharing the observed snapshot's clock. Evaluating
// the two at different times would compare two different questions, and
// the stale-evidence blocker would answer the second.
func TestBaselineKeepsDeclaredValuesAtTheSharedClock(t *testing.T) {
	base, declared, err := rebaseDeclared(fixture(t), 9000)
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}

	var got evidence.MarketState
	if err := json.Unmarshal(declared, &got); err != nil {
		t.Fatalf("declared: %v", err)
	}
	if got.LiquidityDepth != "2000000000000" || got.RealizedVolatilityBps != 2000 {
		t.Fatalf("the baseline must keep the DECLARED market, got %+v", got)
	}

	m := marketOf(t, base)
	if string(m["origin"]) != `"Synthetic"` {
		t.Fatalf("the baseline stays synthetic, got %s", m["origin"])
	}

	// The clock is the one thing the two share.
	var at uint64
	_ = json.Unmarshal(decode(t, base)["assessment_time"], &at)
	if at != 9000 {
		t.Fatalf("baseline must share the observed clock, got %d", at)
	}
}

// The value carries its own observed_at, which the freshness check reads.
// If only the envelope moved, the baseline would block as stale while the
// observed snapshot did not -- a difference produced by the clock rather
// than by the market, which is exactly what the comparison must not show.
func TestBaselineMovesTheInnerObservedAtToo(t *testing.T) {
	base, _, err := rebaseDeclared(fixture(t), 9000)
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	var value struct {
		ObservedAt uint64 `json:"observed_at"`
	}
	if err := json.Unmarshal(marketOf(t, base)["value"], &value); err != nil {
		t.Fatalf("value: %v", err)
	}
	if value.ObservedAt != 9000 {
		t.Fatalf("inner observed_at must move with the envelope, got %d", value.ObservedAt)
	}
}
