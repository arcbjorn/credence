// credence-perplcheck drives the real Perpl providers against the real
// venue and prints the MarketState they produce.
//
// The unit tests in `gateway/market` run against httptest fixtures this
// repository wrote, so they prove the parsing matches a BELIEF about the
// venue. They cannot prove the belief is right. This binary is the check
// that can: it builds the same providers the scheduler builds and reads
// app.perpl.xyz.
//
// `docs/08` requires an integration be demonstrated in the environment it
// claims to work in. Read-only: no key, no funds, no transaction.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/arcbjorn/credence/gateway/market"
)

func main() {
	base := flag.String("base", "https://app.perpl.xyz/api", "Perpl API base URL")
	marketID := flag.Int("market", 20, "market id (NOT stable across networks)")
	// 60s over 6 hours: 360 candles, under the venue's ~1000 cap, and
	// fresh enough for a realistic skew budget. An hourly resolution reads
	// up to an hour stale because a candle is stamped at its OPEN, which
	// is why config refuses a resolution coarser than max_skew_seconds.
	resolution := flag.Uint64("resolution", 60, "candle resolution, in SECONDS")
	window := flag.Uint64("window", 21600, "observation window, in seconds")
	priceDec := flag.Uint("price-decimals", 2, "from context markets[id].config")
	sizeDec := flag.Uint("size-decimals", 3, "from context markets[id].config")
	settleDec := flag.Uint("settlement-decimals", 6, "settlement token decimals")
	timeout := flag.Duration("timeout", 20*time.Second, "per-request timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*(*timeout))
	defer cancel()

	provider, err := market.NewPerplProvider(*base, *marketID, *resolution, *window, *timeout)
	if err != nil {
		fail("provider: %v", err)
	}
	depth := market.NewPerplDepthProvider(
		*base, *marketID,
		uint32(*priceDec), uint32(*sizeDec), uint32(*settleDec), *timeout,
	)

	reader := market.NewReader(provider, *window, "0").WithDepthProvider(depth)

	state, err := reader.Market(ctx)
	if err != nil {
		fail("market read: %v", err)
	}

	// A fallback here means the venue did not serve depth. That is not a
	// crash, but it IS a failed verification: the point of this binary is
	// to show an observation, and reporting a declared number as though it
	// were measured is the dishonesty `docs/04` forbids.
	if reason := reader.DepthFallbackReason(); reason != "" {
		fail("depth was not observed (%s); this check cannot pass on a fallback", reason)
	}

	out, _ := json.MarshalIndent(state, "  ", "  ")
	fmt.Printf("  evidence.MarketState =\n  %s\n\n", out)

	// Skew against the wall clock. The collector measures this against a
	// pinned block; here there is no chain, so the local clock is the
	// closest available reference and the number is indicative only.
	// A candle is stamped at its OPEN, so age up to one resolution is
	// normal on a live feed. Beyond that the feed is genuinely behind.
	skew := time.Since(time.Unix(int64(state.ObservedAt), 0))
	fmt.Printf("  observation is %s old by the local clock\n", skew.Round(time.Second))
	if skew > 2*time.Duration(*resolution)*time.Second {
		fail("observation is older than two candles; the feed is behind")
	}

	if state.RealizedVolatilityBps == 0 {
		// Legitimate in a calm window, and the stress floor exists for
		// exactly this case. Worth saying out loud so a reader does not
		// mistake a quiet market for a broken feed.
		fmt.Println("  volatility is zero: a calm window, which the stress floor covers")
	}

	fmt.Println()
	fmt.Println("PASS  the gateway's own providers read Perpl and produced a MarketState")
	fmt.Println()
	fmt.Println("  Depth is OBSERVED, not declared: it is the resting bid notional")
	fmt.Println("  a liquidation would actually sell into, rescaled to settlement")
	fmt.Println("  units. Before this, docs/04 recorded depth as a declared policy")
	fmt.Println("  input because no public feed reported it.")
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL  "+format+"\n", args...)
	os.Exit(1)
}
