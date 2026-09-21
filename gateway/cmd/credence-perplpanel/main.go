// credence-perplpanel captures live Perpl market data for the web bundle.
//
// `docs/09` requires every displayed number to come from a retained
// fixture or a transaction-derived record, and `scripts/build-web-data.sh`
// enforces it structurally: the run produced the file, or the screen
// renders nothing. A page that fetched Perpl itself would show nothing in
// the submitted static folder and would make "works when the venue is up"
// the evidence, which it is not.
//
// So this writes what the venue actually served, alongside the snapshot
// the browser re-evaluates it against. The page runs the SAME WebAssembly
// engine the gateway uses, so the three stress scenarios it displays are
// the engine's own output rather than a second implementation.
//
// Read-only and unauthenticated: no key, no funds, no transaction.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/arcbjorn/credence/gateway/canonical"
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/market"
)

// Panel is the file the web bundle reads.
type Panel struct {
	// CapturedAt is when this file was written, in seconds.
	CapturedAt uint64 `json:"captured_at"`
	Venue      Venue  `json:"venue"`
	// Market is the evidence value the engine consumes, exactly as the
	// gateway's own reader produced it.
	Market json.RawMessage `json:"market"`
	// Funding is displayed, never fed to the engine. Perpl funding is a
	// positioning premium; the engine's `funding_rate_bps` is the lender's
	// cost of capital. They share a name and nothing else.
	Funding market.FundingSummary `json:"funding"`
	// Book is the top of the orderbook, for display.
	Book BookView `json:"book"`
	// Snapshot is a real retained snapshot with its market evidence
	// replaced by the observation above. The page evaluates it.
	Snapshot json.RawMessage `json:"snapshot"`
	// Baseline is the SAME snapshot with its original declared market
	// evidence, timed to match. The page evaluates both and shows the
	// difference, which is the only honest way to display the effect of
	// observing a market: a single number proves nothing without the
	// counterfactual it is being compared against.
	Baseline json.RawMessage `json:"baseline"`
	// BaselineMarket is the declared value the baseline uses, so the page
	// can name what it is comparing against.
	BaselineMarket json.RawMessage `json:"baseline_market"`
	// SnapshotSource names the file the snapshot came from, so a reader
	// can check that only the market block was substituted.
	SnapshotSource string `json:"snapshot_source"`
}

type Venue struct {
	BaseURL    string `json:"base_url"`
	MarketID   int    `json:"market_id"`
	Symbol     string `json:"symbol"`
	Resolution uint64 `json:"resolution_seconds"`
	Window     uint64 `json:"window_seconds"`
}

// BookView is the visible top of book, in venue units.
type BookView struct {
	PriceDecimals uint32  `json:"price_decimals"`
	SizeDecimals  uint32  `json:"size_decimals"`
	Bids          []Level `json:"bids"`
	Asks          []Level `json:"asks"`
	// BidLevels and AskLevels count the WHOLE book, not the visible rows,
	// so a reader can see how much was summed versus how much is shown.
	BidLevels int `json:"bid_levels"`
	AskLevels int `json:"ask_levels"`
}

type Level struct {
	Price uint64 `json:"price"`
	Size  uint64 `json:"size"`
}

func main() {
	base := flag.String("base", "https://app.perpl.xyz/api", "Perpl API base URL")
	marketID := flag.Int("market", 20, "market id (NOT stable across networks)")
	symbol := flag.String("symbol", "ETH", "symbol, for display")
	resolution := flag.Uint64("resolution", 60, "candle resolution, in SECONDS")
	window := flag.Uint64("window", 21600, "volatility window, in seconds")
	fundWindow := flag.Uint64("funding-window", 604800, "funding window, in seconds")
	priceDec := flag.Uint("price-decimals", 2, "from context markets[id].config")
	sizeDec := flag.Uint("size-decimals", 3, "from context markets[id].config")
	settleDec := flag.Uint("settlement-decimals", 6, "settlement token decimals")
	snapPath := flag.String("snapshot", "", "a retained snapshot JSON to re-evaluate")
	snapLabel := flag.String("snapshot-label", "", "how to name that snapshot on the page")
	depthRows := flag.Int("levels", 8, "book levels to display per side")
	out := flag.String("out", "", "output file")
	timeout := flag.Duration("timeout", 20*time.Second, "per-request timeout")
	flag.Parse()

	if *out == "" {
		fail("-out is required")
	}
	if *snapPath == "" {
		fail("-snapshot is required: the panel re-evaluates a REAL snapshot")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*(*timeout))
	defer cancel()

	// The same providers the scheduler builds.
	candles, err := market.NewPerplProvider(*base, *marketID, *resolution, *window, *timeout)
	if err != nil {
		fail("provider: %v", err)
	}
	depth := market.NewPerplDepthProvider(*base, *marketID,
		uint32(*priceDec), uint32(*sizeDec), uint32(*settleDec), *timeout)
	reader := market.NewReader(candles, *window, "0").WithDepthProvider(depth)

	state, err := reader.Market(ctx)
	if err != nil {
		fail("market: %v", err)
	}
	// A declared fallback here would make the panel display a policy
	// number under an "observed" label, which is the dishonesty `docs/04`
	// exists to prevent.
	if reason := reader.DepthFallbackReason(); reason != "" {
		fail("depth was not observed (%s); the panel must not label a declared number as measured", reason)
	}

	funding, err := market.NewPerplFundingProvider(*base, *marketID, *fundWindow, *timeout).
		Funding(ctx)
	if err != nil {
		fail("funding: %v", err)
	}

	book, err := fetchBook(ctx, *base, *marketID, *depthRows, uint32(*priceDec), uint32(*sizeDec), *timeout)
	if err != nil {
		fail("book: %v", err)
	}

	snapshot, err := substituteMarket(*snapPath, state)
	if err != nil {
		fail("snapshot: %v", err)
	}

	// The counterfactual: the same account and the same clock, with the
	// market evidence the fixture declared. Only the market differs, so
	// any difference in the result is attributable to it and nothing else.
	baseline, declared, err := rebaseDeclared(*snapPath, state.ObservedAt)
	if err != nil {
		fail("baseline: %v", err)
	}

	marketJSON, err := json.Marshal(state)
	if err != nil {
		fail("encode market: %v", err)
	}

	panel := Panel{
		CapturedAt: uint64(time.Now().Unix()),
		Venue: Venue{
			BaseURL: *base, MarketID: *marketID, Symbol: *symbol,
			Resolution: *resolution, Window: *window,
		},
		Market:         marketJSON,
		Funding:        funding,
		Book:           book,
		Snapshot:       snapshot,
		Baseline:       baseline,
		BaselineMarket: declared,
		SnapshotSource: label(*snapLabel, *snapPath),
	}

	body, err := json.MarshalIndent(panel, "", "  ")
	if err != nil {
		fail("encode panel: %v", err)
	}
	if err := os.WriteFile(*out, append(body, '\n'), 0o644); err != nil {
		fail("write: %v", err)
	}

	fmt.Printf("  captured %s market %d (%s)\n", *base, *marketID, *symbol)
	fmt.Printf("  volatility  %d bps over %ds\n", state.RealizedVolatilityBps, *window)
	fmt.Printf("  depth       %s settlement units (observed)\n", state.LiquidityDepth)
	fmt.Printf("  funding     latest %d bps, mean %d, %d%% one-sided over %d samples\n",
		funding.LatestBps, funding.MeanBps, funding.OneSidedPct, funding.Samples)
	fmt.Printf("  wrote       %s\n", *out)
}

// substituteMarket replaces a snapshot's market evidence with an observed
// one, leaving every other field untouched.
//
// Only the market block moves. The account, history, forecasts and
// portfolio stay exactly as retained, so what the panel shows is the
// effect of THIS market on a decision that really happened -- not a
// hypothetical account invented to make a point.
func substituteMarket(path string, state any) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}

	mkt, ok := snap["market"]
	if !ok {
		return nil, fmt.Errorf("snapshot has no market evidence")
	}
	var ev map[string]json.RawMessage
	if err := json.Unmarshal(mkt, &ev); err != nil {
		return nil, fmt.Errorf("parse market evidence: %w", err)
	}

	value, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	ev["value"] = value

	// The provenance fields must move WITH the value. A snapshot carrying
	// observed numbers under `origin: "Synthetic"` would misreport where
	// they came from, and origin is what the engine uses to decide whether
	// a result may be executable at all.
	ev["origin"] = json.RawMessage(`"Observed"`)
	ev["source"] = json.RawMessage(`"perpl"`)

	// Timestamps follow the observation, or skew is measured against the
	// retained snapshot's clock rather than this reading's.
	var st struct {
		ObservedAt uint64 `json:"observed_at"`
	}
	if err := json.Unmarshal(value, &st); err != nil {
		return nil, err
	}
	at, err := json.Marshal(st.ObservedAt)
	if err != nil {
		return nil, err
	}
	ev["observed_at"] = at
	ev["retrieved_at"] = at

	// The content hash described the OLD value and is now wrong.
	// Recomputed with the SAME canonical encoder the gateway uses, so the
	// patched evidence is internally consistent and the engine accepts it
	// on the same terms as any other snapshot. Omitting it is not an
	// option -- the field is required -- and keeping the stale one would
	// hand the engine evidence whose hash describes different numbers.
	contentHash, err := canonical.HashHex(state)
	if err != nil {
		return nil, fmt.Errorf("hash market value: %w", err)
	}
	hashJSON, err := json.Marshal(contentHash)
	if err != nil {
		return nil, err
	}
	ev["content_hash"] = hashJSON

	patched, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	snap["market"] = patched

	// The assessment time moves TO the observation.
	//
	// A retained snapshot carries the time its decision was made, which is
	// not now. The engine enforces freshness against that time
	// (`evidence.rs`: "a current fetch of an old observation is still an
	// old observation"), so leaving it alone makes today's live reading
	// look 8 days stale against a fixture dated in the future, and the
	// whole panel renders `Unavailable`.
	//
	// The substitution is asking a well-posed question -- what would this
	// account's decision be, on today's market -- and that decision is
	// made NOW. Moving the clock is what makes it that question rather
	// than a malformed version of the original.
	//
	// The alternative, loosening `max_evidence_age_seconds`, would weaken
	// the one check that stops stale market data authorizing a loan. It is
	// not on the table for a display.
	at2, err := json.Marshal(st.ObservedAt)
	if err != nil {
		return nil, err
	}
	snap["assessment_time"] = at2

	// The proposal window travels with it, or the assessment authorizes a
	// period that ended days before it was made.
	if b, ok := snap["proposal"]; ok {
		var prop map[string]json.RawMessage
		if err := json.Unmarshal(b, &prop); err == nil {
			var validAfter, validUntil uint64
			_ = json.Unmarshal(prop["valid_after"], &validAfter)
			_ = json.Unmarshal(prop["valid_until"], &validUntil)
			// Keep the window's LENGTH, move its start.
			span := validUntil - validAfter
			na, _ := json.Marshal(st.ObservedAt)
			nu, _ := json.Marshal(st.ObservedAt + span)
			prop["valid_after"] = na
			prop["valid_until"] = nu
			if nb, err := json.Marshal(prop); err == nil {
				snap["proposal"] = nb
			}
		}
	}

	return json.Marshal(snap)
}

// rebaseDeclared moves the retained snapshot's clock without touching its
// market evidence.
//
// The comparison is only meaningful if the clock is the ONE thing the two
// snapshots share. Evaluating the observed one at today's time and the
// declared one at its original time would compare two different
// questions, and the stale-evidence blocker would answer the second.
func rebaseDeclared(path string, at uint64) (json.RawMessage, json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var snap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, nil, err
	}

	mkt, ok := snap["market"]
	if !ok {
		return nil, nil, fmt.Errorf("snapshot has no market evidence")
	}
	var ev map[string]json.RawMessage
	if err := json.Unmarshal(mkt, &ev); err != nil {
		return nil, nil, err
	}

	// Only the timestamps move. The VALUE stays declared, which is the
	// whole point of the comparison.
	atJSON, err := json.Marshal(at)
	if err != nil {
		return nil, nil, err
	}
	ev["observed_at"] = atJSON
	ev["retrieved_at"] = atJSON

	// The value carries its own observed_at, which the freshness check
	// reads. It has to move with the envelope or the baseline blocks as
	// stale while the observed snapshot does not -- a difference produced
	// by the clock rather than by the market.
	var value map[string]json.RawMessage
	if err := json.Unmarshal(ev["value"], &value); err != nil {
		return nil, nil, err
	}
	value["observed_at"] = atJSON
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	ev["value"] = valueJSON

	// Re-hash for the same reason as the observed side: the stored hash
	// described the value at its old timestamp.
	//
	// Decoded into the TYPED MarketState rather than `any`. The canonical
	// encoder refuses floats outright, and `json.Unmarshal` into `any`
	// turns every number into a float64 -- so the untyped path fails with
	// "float in hashed payload" on a payload that contains no floats.
	var declaredState evidence.MarketState
	if err := json.Unmarshal(valueJSON, &declaredState); err != nil {
		return nil, nil, err
	}
	h, err := canonical.HashHex(declaredState)
	if err != nil {
		return nil, nil, err
	}
	hj, err := json.Marshal(h)
	if err != nil {
		return nil, nil, err
	}
	ev["content_hash"] = hj

	patched, err := json.Marshal(ev)
	if err != nil {
		return nil, nil, err
	}
	snap["market"] = patched
	snap["assessment_time"] = atJSON

	if b, ok := snap["proposal"]; ok {
		var prop map[string]json.RawMessage
		if err := json.Unmarshal(b, &prop); err == nil {
			var va, vu uint64
			_ = json.Unmarshal(prop["valid_after"], &va)
			_ = json.Unmarshal(prop["valid_until"], &vu)
			span := vu - va
			na, _ := json.Marshal(at)
			nu, _ := json.Marshal(at + span)
			prop["valid_after"] = na
			prop["valid_until"] = nu
			if nb, err := json.Marshal(prop); err == nil {
				snap["proposal"] = nb
			}
		}
	}

	out, err := json.Marshal(snap)
	if err != nil {
		return nil, nil, err
	}
	return out, valueJSON, nil
}

func fetchBook(ctx context.Context, base string, id, rows int, priceDec, sizeDec uint32, timeout time.Duration) (BookView, error) {
	levels, err := market.FetchBookLevels(ctx, base, id, timeout)
	if err != nil {
		return BookView{}, err
	}
	view := BookView{
		PriceDecimals: priceDec,
		SizeDecimals:  sizeDec,
		BidLevels:     len(levels.Bids),
		AskLevels:     len(levels.Asks),
	}
	for i, l := range levels.Bids {
		if i >= rows {
			break
		}
		view.Bids = append(view.Bids, Level{Price: l.Price, Size: l.Size})
	}
	for i, l := range levels.Asks {
		if i >= rows {
			break
		}
		view.Asks = append(view.Asks, Level{Price: l.Price, Size: l.Size})
	}
	return view, nil
}

// label prefers an explicit name over the file path.
//
// The path is usually a mktemp scratch file, and "tmp.nbcHaDQ3zO" told a
// reader nothing about which run the snapshot came from. Caught by
// looking at the rendered page, not by a test.
func label(explicit, path string) string {
	if explicit != "" {
		return explicit
	}
	return path
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL  "+format+"\n", args...)
	os.Exit(1)
}
