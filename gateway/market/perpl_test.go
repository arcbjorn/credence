package market_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arcbjorn/credence/gateway/market"
)

// perplServer serves fixed bodies per path prefix, so one server can back
// both the candle and book providers.
func perplServer(t *testing.T, routes map[string]string) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for prefix, body := range routes {
			if strings.Contains(r.URL.Path, prefix) {
				if strings.HasPrefix(body, "STATUS:") {
					var code int
					_, _ = fmt.Sscanf(body, "STATUS:%d", &code)
					w.WriteHeader(code)
					return
				}
				_, _ = w.Write([]byte(body))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(s.Close)
	return s.URL
}

// A window that cannot hold two candles can never yield a return, so it is
// refused at construction rather than failing every cycle at runtime.
func TestPerplWindowTooShortForTwoCandles(t *testing.T) {
	_, err := market.NewPerplProvider("http://x", 20, 3600, 3600, time.Second)
	if err == nil {
		t.Fatal("expected a one-candle window to be refused")
	}
}

// The venue caps a request near 1,000 candles and answers HTTP 400 beyond
// it. `scripts/probe-perpl.sh` records that this is a COUNT cap, not a
// rejected resolution: 60s over 7 days fails while 60s over 6 hours works.
// Catching it at construction turns a recurring mid-cycle block into a
// startup error someone can read.
func TestPerplCandleCapIsCheckedUpFront(t *testing.T) {
	// 60s resolution over 7 days is 10,080 candles.
	_, err := market.NewPerplProvider("http://x", 20, 60, 604800, time.Second)
	if err == nil {
		t.Fatal("expected an over-cap window to be refused")
	}
	if !strings.Contains(err.Error(), "cap") {
		t.Fatalf("error should name the cap, got %v", err)
	}

	// The same resolution over 6 hours is 360 candles and is fine.
	if _, err := market.NewPerplProvider("http://x", 20, 60, 21600, time.Second); err != nil {
		t.Fatalf("360 candles is under the cap: %v", err)
	}
}

// Perpl stamps candles in MILLISECONDS; the evidence contract works in
// seconds. Without the conversion every observation looks tens of
// thousands of years in the future, and the collector's skew bound -- the
// one check standing between a stale feed and an authorization -- passes
// everything.
func TestPerplTimestampsAreConvertedToSeconds(t *testing.T) {
	body := `{"d":[{"t":1700000000000,"c":100},{"t":1700003600000,"c":101}]}`
	url := perplServer(t, map[string]string{"/candles/": body})
	p, err := market.NewPerplProvider(url, 20, 3600, 7200, 2*time.Second)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	_, at, err := p.Candles(context.Background())
	if err != nil {
		t.Fatalf("candles: %v", err)
	}
	if at != 1700003600 {
		t.Fatalf("expected seconds 1700003600, got %d", at)
	}
}

// The stamp is the newest CANDLE, not the wall clock. A venue that stops
// updating must read as stale; stamping with time.Now() would report a
// frozen feed as perfectly fresh.
func TestPerplStampsTheNewestCandleNotTheClock(t *testing.T) {
	// Both candles are from 2023. A clock stamp would be today.
	body := `{"d":[{"t":1700000000000,"c":100},{"t":1700003600000,"c":101}]}`
	url := perplServer(t, map[string]string{"/candles/": body})
	p, _ := market.NewPerplProvider(url, 20, 3600, 7200, 2*time.Second)

	_, at, err := p.Candles(context.Background())
	if err != nil {
		t.Fatalf("candles: %v", err)
	}
	if at > uint64(time.Now().Unix())-1000 {
		t.Fatalf("stamp %d looks like the clock, not the candle", at)
	}
}

// One candle cannot produce a return. Padding it, or reusing the single
// close as both ends, would be synthetic evidence inside an executable
// decision -- which `docs/04` forbids outright.
func TestPerplSingleCandleIsRefused(t *testing.T) {
	url := perplServer(t, map[string]string{"/candles/": `{"d":[{"t":1700000000000,"c":100}]}`})
	p, _ := market.NewPerplProvider(url, 20, 3600, 7200, 2*time.Second)
	if _, _, err := p.Candles(context.Background()); err == nil {
		t.Fatal("expected a single candle to be refused")
	}
}

// A zero close makes the return undefined. The shared volatility routine
// already refuses it; catching it here names the candle.
func TestPerplZeroCloseIsRefused(t *testing.T) {
	body := `{"d":[{"t":1700000000000,"c":0},{"t":1700003600000,"c":101}]}`
	url := perplServer(t, map[string]string{"/candles/": body})
	p, _ := market.NewPerplProvider(url, 20, 3600, 7200, 2*time.Second)
	if _, _, err := p.Candles(context.Background()); err == nil {
		t.Fatal("expected a zero close to be refused")
	}
}

// The status code is diagnostic on this venue and must survive: 500 means
// a bad resolution FORMAT (the string `1h`), 400 means too many candles.
// Collapsing both to "provider failed" discards the only signal that
// distinguishes a misconfiguration from an outage.
func TestPerplStatusCodeSurvivesInTheError(t *testing.T) {
	for _, code := range []int{400, 500} {
		url := perplServer(t, map[string]string{"/candles/": fmt.Sprintf("STATUS:%d", code)})
		p, _ := market.NewPerplProvider(url, 20, 3600, 7200, 2*time.Second)
		_, _, err := p.Candles(context.Background())
		if err == nil {
			t.Fatalf("status %d should be an error", code)
		}
		if !strings.Contains(err.Error(), fmt.Sprint(code)) {
			t.Fatalf("error should name status %d, got %v", code, err)
		}
	}
}

// Depth sums BIDS. Liquidating collateral sells into bids; counting asks
// would report depth that cannot absorb the modelled trade. Because
// `stress.rs` divides by depth in a square-root impact term, an
// overstatement understates impact and over-lends.
func TestPerplDepthSumsBidsNotAsks(t *testing.T) {
	// Bids: 100*2 + 99*1 = 299. Asks are deliberately much larger, so
	// summing the wrong side is unmissable.
	body := `{"at":{"t":1700000000000},
	          "bid":[{"p":100,"s":2},{"p":99,"s":1}],
	          "ask":[{"p":101,"s":9999}]}`
	url := perplServer(t, map[string]string{"/book": body})
	// Equal decimals on both sides, so no rescaling obscures the sum.
	d := market.NewPerplDepthProvider(url, 20, 0, 0, 0, 2*time.Second)

	got, at, err := d.Depth(context.Background())
	if err != nil {
		t.Fatalf("depth: %v", err)
	}
	if got != 299 {
		t.Fatalf("expected bid notional 299, got %d", got)
	}
	if at != 1700000000 {
		t.Fatalf("expected seconds, got %d", at)
	}
}

// The book quotes scaled integers and the engine works in settlement
// units. With price_decimals=2 and size_decimals=3, a notional is in
// 10^-5; settlement (AUSD) is 10^-6, so the figure scales UP by ten.
func TestPerplDepthRescalesToSettlementUnits(t *testing.T) {
	body := `{"at":{"t":1700000000000},"bid":[{"p":100,"s":1}]}`
	url := perplServer(t, map[string]string{"/book": body})
	d := market.NewPerplDepthProvider(url, 20, 2, 3, 6, 2*time.Second)

	got, _, err := d.Depth(context.Background())
	if err != nil {
		t.Fatalf("depth: %v", err)
	}
	if got != 1000 {
		t.Fatalf("expected 100 rescaled from 10^-5 to 10^-6 = 1000, got %d", got)
	}
}

// Scaling down truncates toward zero, which reports LESS depth. That is
// the conservative direction: less depth means more modelled impact and a
// smaller loan, so the rounding can never flatter a book.
func TestPerplDepthTruncatesDownward(t *testing.T) {
	// 19 in 10^-2 units, read as 10^-1: 1.9 truncates to 1.
	body := `{"at":{"t":1700000000000},"bid":[{"p":19,"s":1}]}`
	url := perplServer(t, map[string]string{"/book": body})
	d := market.NewPerplDepthProvider(url, 20, 2, 0, 1, 2*time.Second)

	got, _, err := d.Depth(context.Background())
	if err != nil {
		t.Fatalf("depth: %v", err)
	}
	if got != 1 {
		t.Fatalf("expected truncation to 1, got %d", got)
	}
}

// An empty book is an ABSENT observation, not an observed zero. Zero would
// reach the engine as "undeclared depth" and take the maximum haircut --
// numerically safe, but it would be published as a measurement. Refusing
// makes it Missing evidence, which is what it is.
func TestPerplEmptyBookIsRefused(t *testing.T) {
	url := perplServer(t, map[string]string{"/book": `{"at":{"t":1700000000000},"bid":[],"ask":[]}`})
	d := market.NewPerplDepthProvider(url, 20, 2, 3, 6, 2*time.Second)
	if _, _, err := d.Depth(context.Background()); err == nil {
		t.Fatal("expected an empty book to be refused")
	}
}

// Without an observation time the reading cannot be placed in time, so
// skew cannot be measured against it.
func TestPerplBookWithoutTimeIsRefused(t *testing.T) {
	url := perplServer(t, map[string]string{"/book": `{"bid":[{"p":100,"s":1}]}`})
	d := market.NewPerplDepthProvider(url, 20, 2, 3, 6, 2*time.Second)
	if _, _, err := d.Depth(context.Background()); err == nil {
		t.Fatal("expected a book with no timestamp to be refused")
	}
}

// --- the reader's use of an observed depth ----------------------------

// stubDepth is a DepthProvider under test control.
type stubDepth struct {
	depth uint64
	err   error
}

func (s stubDepth) Depth(context.Context) (uint64, uint64, error) {
	if s.err != nil {
		return 0, 0, s.err
	}
	return s.depth, 1700000000, nil
}

// When a provider observes depth, the observation replaces the declared
// policy number.
func TestReaderPrefersObservedDepth(t *testing.T) {
	r := market.NewReader(
		market.StaticProvider{Closes: []uint64{100, 101}}, 3600, "999",
	).WithDepthProvider(stubDepth{depth: 12345})

	st, err := r.Market(context.Background())
	if err != nil {
		t.Fatalf("market: %v", err)
	}
	if st.LiquidityDepth != "12345" {
		t.Fatalf("expected the observation, got %q", st.LiquidityDepth)
	}
	if r.DepthFallbackReason() != "" {
		t.Fatalf("no fallback expected, got %q", r.DepthFallbackReason())
	}
}

// A venue outage falls back to the declared number rather than blocking.
//
// This is the deliberate asymmetry: the fallback direction is chosen so a
// provider failure cannot make the system lend MORE. Blocking every cycle
// on an external venue this protocol does not control would be a worse
// failure than using the number an operator wrote down -- and the reason
// is recorded either way, so the evidence never claims a measurement it
// did not make.
func TestReaderFallsBackWhenDepthProviderFails(t *testing.T) {
	r := market.NewReader(
		market.StaticProvider{Closes: []uint64{100, 101}}, 3600, "999",
	).WithDepthProvider(stubDepth{err: fmt.Errorf("venue down")})

	st, err := r.Market(context.Background())
	if err != nil {
		t.Fatalf("a depth outage must not fail the read: %v", err)
	}
	if st.LiquidityDepth != "999" {
		t.Fatalf("expected the declared fallback, got %q", st.LiquidityDepth)
	}
	if !strings.Contains(r.DepthFallbackReason(), "venue down") {
		t.Fatalf("the reason must name the failure, got %q", r.DepthFallbackReason())
	}
}

// Zero from a healthy provider is not an observation of an empty market --
// it is a reading this code cannot distinguish from one, so it takes the
// declared value and says why.
func TestReaderTreatsZeroDepthAsUnobserved(t *testing.T) {
	r := market.NewReader(
		market.StaticProvider{Closes: []uint64{100, 101}}, 3600, "999",
	).WithDepthProvider(stubDepth{depth: 0})

	st, err := r.Market(context.Background())
	if err != nil {
		t.Fatalf("market: %v", err)
	}
	if st.LiquidityDepth != "999" {
		t.Fatalf("expected the declared fallback, got %q", st.LiquidityDepth)
	}
	if r.DepthFallbackReason() == "" {
		t.Fatal("a fallback must always record its reason")
	}
}

// Without a depth provider the reader behaves exactly as before, so a
// scenario facility is unaffected by any of this.
func TestReaderWithoutDepthProviderUsesDeclaredDepth(t *testing.T) {
	r := market.NewReader(market.StaticProvider{Closes: []uint64{100, 101}}, 3600, "999")
	st, err := r.Market(context.Background())
	if err != nil {
		t.Fatalf("market: %v", err)
	}
	if st.LiquidityDepth != "999" {
		t.Fatalf("expected declared depth, got %q", st.LiquidityDepth)
	}
	if r.DepthFallbackReason() != "" {
		t.Fatalf("an unobserved reader has nothing to fall back from, got %q",
			r.DepthFallbackReason())
	}
}

// --- funding ----------------------------------------------------------

// fundingBody builds a response from (rate, seconds) pairs.
func fundingBody(rows ...[2]int64) string {
	var parts []string
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf(`{"rate":%d,"at":{"t":%d}}`, r[0], r[1]*1000))
	}
	return `{"d":[` + strings.Join(parts, ",") + `]}`
}

// Persistently one-sided funding is the crowding signal: leveraged
// positioning in one direction, which unwinds faster than it accumulates.
func TestFundingOneSidedWindowReadsAsCrowded(t *testing.T) {
	body := fundingBody([2]int64{30, 100}, [2]int64{20, 200}, [2]int64{40, 300})
	url := perplServer(t, map[string]string{"/funding/": body})
	f, err := market.NewPerplFundingProvider(url, 20, 3600, 2*time.Second).
		Funding(context.Background())
	if err != nil {
		t.Fatalf("funding: %v", err)
	}
	if f.OneSidedPct != 100 {
		t.Fatalf("all three agree, expected 100, got %d", f.OneSidedPct)
	}
	if f.MeanBps != 30 {
		t.Fatalf("expected mean 30, got %d", f.MeanBps)
	}
	if f.MaxAbsBps != 40 {
		t.Fatalf("expected max 40, got %d", f.MaxAbsBps)
	}
}

// Funding that flips sign is a market finding its level, not crowding.
func TestFundingAlternatingSignIsNotCrowded(t *testing.T) {
	body := fundingBody([2]int64{30, 100}, [2]int64{-30, 200},
		[2]int64{30, 300}, [2]int64{-20, 400})
	url := perplServer(t, map[string]string{"/funding/": body})
	f, err := market.NewPerplFundingProvider(url, 20, 3600, 2*time.Second).
		Funding(context.Background())
	if err != nil {
		t.Fatalf("funding: %v", err)
	}
	if f.OneSidedPct > 60 {
		t.Fatalf("an alternating window is not crowded, got %d%%", f.OneSidedPct)
	}
}

// Zero samples are EXCLUDED from the crowding measure. A market with no
// premium is neither crowded nor balanced, and counting flat readings as
// agreement would make a dead market look maximally one-sided -- the
// exact inversion this measure exists to avoid.
//
// Live data makes this concrete: roughly half of ETH's samples over seven
// days were zero (`scripts/probe-perpl.sh`).
func TestFundingZeroSamplesDoNotCountAsAgreement(t *testing.T) {
	// Two real one-sided readings among many flat ones.
	body := fundingBody([2]int64{0, 100}, [2]int64{0, 200}, [2]int64{0, 300},
		[2]int64{0, 400}, [2]int64{30, 500}, [2]int64{30, 600})
	url := perplServer(t, map[string]string{"/funding/": body})
	f, err := market.NewPerplFundingProvider(url, 20, 3600, 2*time.Second).
		Funding(context.Background())
	if err != nil {
		t.Fatalf("funding: %v", err)
	}
	// Both NON-ZERO samples agree, so 100% -- computed over 2, not 6.
	if f.OneSidedPct != 100 {
		t.Fatalf("expected the measure over non-zero samples, got %d", f.OneSidedPct)
	}
	if f.Samples != 6 {
		t.Fatalf("the sample count still reports the whole window, got %d", f.Samples)
	}
}

// An entirely flat window divides by zero if the guard is missing.
func TestFundingAllZeroWindowIsNotCrowded(t *testing.T) {
	body := fundingBody([2]int64{0, 100}, [2]int64{0, 200})
	url := perplServer(t, map[string]string{"/funding/": body})
	f, err := market.NewPerplFundingProvider(url, 20, 3600, 2*time.Second).
		Funding(context.Background())
	if err != nil {
		t.Fatalf("a flat window is legitimate: %v", err)
	}
	if f.OneSidedPct != 0 {
		t.Fatalf("no premium is not crowding, got %d", f.OneSidedPct)
	}
}

// The latest reading is the NEWEST sample, not the last in array order.
func TestFundingLatestIsTheNewestSample(t *testing.T) {
	// Deliberately out of order: the newest (t=900) sits first.
	body := fundingBody([2]int64{99, 900}, [2]int64{10, 100}, [2]int64{20, 200})
	url := perplServer(t, map[string]string{"/funding/": body})
	f, err := market.NewPerplFundingProvider(url, 20, 3600, 2*time.Second).
		Funding(context.Background())
	if err != nil {
		t.Fatalf("funding: %v", err)
	}
	if f.LatestBps != 99 {
		t.Fatalf("expected the newest sample 99, got %d", f.LatestBps)
	}
	if f.ObservedAt != 900 {
		t.Fatalf("expected seconds 900, got %d", f.ObservedAt)
	}
}

// An empty window is refused rather than summarised as zero: a flat zero
// reads as a perfectly balanced market, which is the opposite of "not
// observed".
func TestFundingEmptyWindowIsRefused(t *testing.T) {
	url := perplServer(t, map[string]string{"/funding/": `{"d":[]}`})
	_, err := market.NewPerplFundingProvider(url, 20, 3600, 2*time.Second).
		Funding(context.Background())
	if err == nil {
		t.Fatal("expected an empty funding window to be refused")
	}
}

// Negative funding (shorts paying longs) is crowding too, in the other
// direction. The measure is about one-sidedness, not about sign.
func TestFundingNegativeWindowIsAlsoCrowded(t *testing.T) {
	body := fundingBody([2]int64{-30, 100}, [2]int64{-20, 200}, [2]int64{-40, 300})
	url := perplServer(t, map[string]string{"/funding/": body})
	f, err := market.NewPerplFundingProvider(url, 20, 3600, 2*time.Second).
		Funding(context.Background())
	if err != nil {
		t.Fatalf("funding: %v", err)
	}
	if f.OneSidedPct != 100 {
		t.Fatalf("expected 100%% one-sided, got %d", f.OneSidedPct)
	}
	if f.MeanBps >= 0 {
		t.Fatalf("the mean must keep its sign, got %d", f.MeanBps)
	}
	if f.MaxAbsBps != 40 {
		t.Fatalf("max is an ABSOLUTE reading, got %d", f.MaxAbsBps)
	}
}
