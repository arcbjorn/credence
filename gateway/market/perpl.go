package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PerplProvider reads candles and orderbook depth from Perpl, the perps
// venue on Monad.
//
// `scripts/probe-perpl.sh` establishes on the live venue that everything
// used here is public and unauthenticated. Three details it records are
// load-bearing and would have been got wrong by reading the docs:
//
//   - Resolution is an integer number of SECONDS. The TradingView-style
//     strings (`1h`) return HTTP 500, not 400, so a wrong resolution
//     reads like an outage rather than a bad request.
//   - Timestamps are MILLISECONDS. The evidence contract works in
//     seconds, so they are divided on the way in; publishing a
//     millisecond stamp would make every observation look ~54,000 years
//     in the future and skew detection would pass everything.
//   - Requests are capped near 1,000 candles. `WindowSeconds` and
//     `Resolution` therefore have to be chosen together, which
//     `NewPerplProvider` checks rather than discovering at runtime.
//
// The depth half is the part that changes the system. `Reader` has until
// now taken liquidity depth as a DECLARED policy input, because no public
// feed reported the depth available at liquidation. Perpl's book is that
// feed, so `PerplDepthProvider` promotes depth from declaration to
// observation.
type PerplProvider struct {
	baseURL    string
	marketID   int
	resolution uint64
	window     uint64
	http       *http.Client
}

// MaxPerplCandles is the per-request cap the venue enforces. Exceeding it
// returns HTTP 400 -- which, per the probe, is a COUNT cap and not a
// rejected resolution.
const MaxPerplCandles = 1000

// NewPerplProvider fetches `window` seconds of history at `resolution`.
//
// The candle count is checked here rather than left to the venue: a 400
// mid-cycle becomes Missing evidence and blocks authorization, and a
// misconfiguration that blocks every cycle should be caught at startup
// where it can be read, not at 3am in a scheduler log.
func NewPerplProvider(baseURL string, marketID int, resolution, window uint64, timeout time.Duration) (*PerplProvider, error) {
	if resolution == 0 {
		return nil, fmt.Errorf("perpl: resolution must be positive")
	}
	if window < resolution*2 {
		// Volatility needs at least two closes, so a window that cannot
		// hold two candles can never produce a reading.
		return nil, fmt.Errorf(
			"perpl: window %ds holds fewer than two %ds candles", window, resolution)
	}
	if n := window / resolution; n > MaxPerplCandles {
		return nil, fmt.Errorf(
			"perpl: window %ds at %ds resolution is %d candles, over the %d cap",
			window, resolution, n, MaxPerplCandles)
	}
	return &PerplProvider{
		baseURL:    baseURL,
		marketID:   marketID,
		resolution: resolution,
		window:     window,
		http:       &http.Client{Timeout: timeout},
	}, nil
}

// perplCandle is one OHLCV bar. `v` is a decimal STRING in the venue's
// response, so it is not decoded as a number here; only closes are used.
type perplCandle struct {
	T uint64 `json:"t"` // milliseconds
	C uint64 `json:"c"`
}

type perplCandleResponse struct {
	D []perplCandle `json:"d"`
}

// Candles implements Provider.
func (p *PerplProvider) Candles(ctx context.Context) ([]uint64, uint64, error) {
	now := uint64(time.Now().UnixMilli())
	from := now - p.window*1000
	url := fmt.Sprintf("%s/v1/market-data/%d/candles/%d/%d-%d",
		p.baseURL, p.marketID, p.resolution, from, now)

	var doc perplCandleResponse
	if err := p.getJSON(ctx, url, &doc); err != nil {
		return nil, 0, err
	}
	if len(doc.D) < 2 {
		// Refused rather than padded. A venue that returns one bar has
		// not reported enough to measure a return, and inventing the
		// second one would be synthetic evidence in an executable
		// decision (`docs/04`).
		return nil, 0, fmt.Errorf("perpl: got %d candles, need at least 2", len(doc.D))
	}

	closes := make([]uint64, 0, len(doc.D))
	var newest uint64
	for i, c := range doc.D {
		if c.C == 0 {
			return nil, 0, fmt.Errorf("perpl: candle %d has a zero close", i)
		}
		closes = append(closes, c.C)
		if c.T > newest {
			newest = c.T
		}
	}
	if newest == 0 {
		return nil, 0, fmt.Errorf("perpl: no candle carries a timestamp")
	}

	// The observation is stamped with the newest CANDLE, not the wall
	// clock. Skew is measured against this, so using the clock would
	// report a stale feed as fresh -- the exact failure the collector's
	// skew bound exists to catch.
	return closes, newest / 1000, nil
}

// perplLevel is one book level: price, size, and resting order count.
type perplLevel struct {
	P uint64 `json:"p"`
	S uint64 `json:"s"`
}

type perplBook struct {
	Bid []perplLevel `json:"bid"`
	Ask []perplLevel `json:"ask"`
	At  struct {
		T uint64 `json:"t"` // milliseconds
	} `json:"at"`
}

// PerplDepthProvider reports the depth a liquidation could actually trade
// into, in settlement units.
//
// Separate from PerplProvider because depth and volatility answer
// different questions and fail independently: a book can be empty while
// candles are healthy, and the reader must be able to tell which one is
// missing.
type PerplDepthProvider struct {
	baseURL  string
	marketID int
	// priceDecimals and sizeDecimals come from the venue's own market
	// config (`config.price_decimals`, `config.size_decimals`), and
	// settlementDecimals from its settlement token. Depth is meaningless
	// without all three: the book quotes scaled integers, and the engine
	// works in settlement units.
	priceDecimals      uint32
	sizeDecimals       uint32
	settlementDecimals uint32
	http               *http.Client
}

func NewPerplDepthProvider(baseURL string, marketID int, priceDecimals, sizeDecimals, settlementDecimals uint32, timeout time.Duration) *PerplDepthProvider {
	return &PerplDepthProvider{
		baseURL:            baseURL,
		marketID:           marketID,
		priceDecimals:      priceDecimals,
		sizeDecimals:       sizeDecimals,
		settlementDecimals: settlementDecimals,
		http:               &http.Client{Timeout: timeout},
	}
}

// Depth returns total resting BID notional, in settlement units.
//
// The bid side specifically. Liquidating collateral SELLS, so it trades
// into bids; summing asks, or both sides, would report depth that cannot
// absorb the trade the liquidity scenario models. Since `stress.rs` uses
// depth as the denominator of a square-root market-impact term,
// overstating it understates impact and over-lends -- so the conservative
// choice is the only correct one here.
func (p *PerplDepthProvider) Depth(ctx context.Context) (uint64, uint64, error) {
	url := fmt.Sprintf("%s/v1/market-data/%d/book", p.baseURL, p.marketID)

	var doc perplBook
	if err := p.getJSON(ctx, url, &doc); err != nil {
		return 0, 0, err
	}
	if len(doc.Bid) == 0 {
		// An empty book is NOT zero depth reported as fine. Zero depth
		// reaches `liquidity_impact_bps` as "undeclared", which takes the
		// maximum haircut -- correct, but it would be reported as an
		// observation when it is really an absent one. Refusing makes it
		// Missing evidence instead.
		return 0, 0, fmt.Errorf("perpl: book has no bids")
	}
	if doc.At.T == 0 {
		return 0, 0, fmt.Errorf("perpl: book carries no observation time")
	}

	// notional = sum(price * size), in units of 10^-(priceDecimals+sizeDecimals).
	var notional uint64
	for i, l := range doc.Bid {
		if l.P == 0 || l.S == 0 {
			continue // an empty level contributes nothing; not an error
		}
		v, err := mulChecked(l.P, l.S)
		if err != nil {
			return 0, 0, fmt.Errorf("perpl: bid level %d: %w", i, err)
		}
		notional, err = addChecked(notional, v)
		if err != nil {
			return 0, 0, fmt.Errorf("perpl: summing bids: %w", err)
		}
	}

	scaled, err := rescale(notional, p.priceDecimals+p.sizeDecimals, p.settlementDecimals)
	if err != nil {
		return 0, 0, fmt.Errorf("perpl: depth: %w", err)
	}
	return scaled, doc.At.T / 1000, nil
}

// rescale converts a fixed-point integer from `from` decimals to `to`.
//
// Scaling DOWN truncates, which rounds depth toward zero -- the
// conservative direction, since less depth means more modelled impact.
func rescale(v uint64, from, to uint32) (uint64, error) {
	if from == to {
		return v, nil
	}
	if to > from {
		factor, err := pow10(to - from)
		if err != nil {
			return 0, err
		}
		return mulChecked(v, factor)
	}
	factor, err := pow10(from - to)
	if err != nil {
		return 0, err
	}
	return v / factor, nil
}

func pow10(n uint32) (uint64, error) {
	if n > 19 {
		return 0, fmt.Errorf("10^%d overflows uint64", n)
	}
	out := uint64(1)
	for i := uint32(0); i < n; i++ {
		out *= 10
	}
	return out, nil
}

func mulChecked(a, b uint64) (uint64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if a > ^uint64(0)/b {
		return 0, fmt.Errorf("%d * %d overflows", a, b)
	}
	return a * b, nil
}

func addChecked(a, b uint64) (uint64, error) {
	if a > ^uint64(0)-b {
		return 0, fmt.Errorf("%d + %d overflows", a, b)
	}
	return a + b, nil
}

// getJSON is shared by both providers: same limits, same refusals.
func (p *PerplProvider) getJSON(ctx context.Context, url string, out any) error {
	return getJSONBounded(ctx, p.http, url, out)
}

func (p *PerplDepthProvider) getJSON(ctx context.Context, url string, out any) error {
	return getJSONBounded(ctx, p.http, url, out)
}

func getJSONBounded(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("perpl unreachable: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		// The status is included because Perpl's codes are diagnostic in a
		// way the probe documents: 500 means a bad resolution FORMAT, 400
		// means too many candles requested. Swallowing it would discard
		// the only signal distinguishing the two.
		return fmt.Errorf("perpl returned status %d", res.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read perpl response: %w", err)
	}
	if len(raw) > MaxResponseBytes {
		return fmt.Errorf("perpl response exceeds %d bytes", MaxResponseBytes)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode perpl response: %w", err)
	}
	return nil
}

// --- funding ----------------------------------------------------------

// perplFunding is one funding observation. `rate` is the per-interval
// funding in bps, signed: positive means longs pay shorts.
type perplFunding struct {
	Rate int64 `json:"rate"`
	At   struct {
		T uint64 `json:"t"` // milliseconds
	} `json:"at"`
}

type perplFundingResponse struct {
	D []perplFunding `json:"d"`
}

// FundingSummary describes recent perpetual funding on one market.
//
// **This is a positioning signal, not a cost.** It must not be confused
// with the engine's `funding_rate_bps`, which is the LENDER'S cost of
// capital and is deducted from gross interest in `pricing.rs`. Perpl's
// funding is the premium longs pay shorts to hold a perp near its index.
// Wiring one into the other would silently corrupt loan pricing, so
// nothing here reaches the engine: `docs/08` keeps market feedback
// informational, and this is market feedback.
//
// What it legitimately indicates is CROWDING. Persistently one-sided
// funding means leveraged positioning in one direction, and leveraged
// positioning unwinds faster than it accumulates -- which is a reason to
// read a calm volatility number with suspicion, not a reason to change a
// number the engine already computed.
type FundingSummary struct {
	// Samples counted in the window.
	Samples int `json:"samples"`
	// LatestBps is the most recent observation.
	LatestBps int64 `json:"latest_bps"`
	// MeanBps is the arithmetic mean over the window, signed.
	MeanBps int64 `json:"mean_bps"`
	// MaxAbsBps is the largest absolute reading in the window.
	MaxAbsBps int64 `json:"max_abs_bps"`
	// OneSidedPct is the share of NON-ZERO samples sharing the sign of the
	// window's mean, as a percentage.
	//
	// The crowding measure. Funding that flips sign constantly is a market
	// finding its level; funding that stays one-sided is accumulated
	// positioning. Zero samples are excluded because a market with no
	// premium is neither crowded nor balanced, and counting them as
	// agreement would make a dead market look one-sided.
	OneSidedPct int `json:"one_sided_pct"`
	// WindowSeconds is what the figures describe.
	WindowSeconds uint64 `json:"window_seconds"`
	// ObservedAt is the newest sample, in seconds.
	ObservedAt uint64 `json:"observed_at"`
}

// PerplFundingProvider reads recent funding for one market.
type PerplFundingProvider struct {
	baseURL  string
	marketID int
	window   uint64
	http     *http.Client
}

func NewPerplFundingProvider(baseURL string, marketID int, window uint64, timeout time.Duration) *PerplFundingProvider {
	return &PerplFundingProvider{
		baseURL:  baseURL,
		marketID: marketID,
		window:   window,
		http:     &http.Client{Timeout: timeout},
	}
}

// Funding summarises the window. Returns an error rather than a zeroed
// summary when nothing is available: a flat zero reads as a perfectly
// balanced market, which is the opposite of "not observed".
func (p *PerplFundingProvider) Funding(ctx context.Context) (FundingSummary, error) {
	now := uint64(time.Now().UnixMilli())
	from := now - p.window*1000
	url := fmt.Sprintf("%s/v1/market-data/%d/funding/%d-%d",
		p.baseURL, p.marketID, from, now)

	var doc perplFundingResponse
	if err := getJSONBounded(ctx, p.http, url, &doc); err != nil {
		return FundingSummary{}, err
	}
	if len(doc.D) == 0 {
		return FundingSummary{}, fmt.Errorf("perpl: no funding samples in the window")
	}

	out := FundingSummary{
		Samples:       len(doc.D),
		WindowSeconds: p.window,
	}

	var sum int64
	for _, f := range doc.D {
		sum += f.Rate
		if abs64(f.Rate) > out.MaxAbsBps {
			out.MaxAbsBps = abs64(f.Rate)
		}
		if f.At.T/1000 > out.ObservedAt {
			out.ObservedAt = f.At.T / 1000
			out.LatestBps = f.Rate
		}
	}
	if out.ObservedAt == 0 {
		return FundingSummary{}, fmt.Errorf("perpl: no funding sample carries a timestamp")
	}
	out.MeanBps = sum / int64(len(doc.D))

	// Crowding: of the samples that expressed a premium at all, how many
	// agreed with the window's direction.
	var nonZero, agreeing int
	for _, f := range doc.D {
		if f.Rate == 0 {
			continue
		}
		nonZero++
		if (f.Rate > 0) == (out.MeanBps > 0) && out.MeanBps != 0 {
			agreeing++
		}
	}
	if nonZero > 0 {
		out.OneSidedPct = agreeing * 100 / nonZero
	}

	return out, nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// --- book, for display ------------------------------------------------

// BookLevel is one price level.
type BookLevel struct {
	Price uint64
	Size  uint64
}

// BookLevels is a parsed orderbook.
type BookLevels struct {
	Bids       []BookLevel
	Asks       []BookLevel
	ObservedAt uint64 // seconds
}

// FetchBookLevels reads the raw book.
//
// Exported so a display can show the levels that `Depth` summed, using
// the SAME parsing. A second decoder would let the panel disagree with
// the number the engine consumed, which is exactly the drift a panel is
// supposed to make visible.
func FetchBookLevels(ctx context.Context, baseURL string, marketID int, timeout time.Duration) (BookLevels, error) {
	client := &http.Client{Timeout: timeout}
	url := fmt.Sprintf("%s/v1/market-data/%d/book", baseURL, marketID)

	var doc perplBook
	if err := getJSONBounded(ctx, client, url, &doc); err != nil {
		return BookLevels{}, err
	}
	if len(doc.Bid) == 0 {
		return BookLevels{}, fmt.Errorf("perpl: book has no bids")
	}

	out := BookLevels{ObservedAt: doc.At.T / 1000}
	for _, l := range doc.Bid {
		out.Bids = append(out.Bids, BookLevel{Price: l.P, Size: l.S})
	}
	for _, l := range doc.Ask {
		out.Asks = append(out.Asks, BookLevel{Price: l.P, Size: l.S})
	}
	return out, nil
}
