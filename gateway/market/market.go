// Package market supplies market observations.
//
// This is the one evidence source the gateway cannot derive or read from
// its own chain. `docs/04` therefore treats it as the case where skew must
// be *measured*: a provider cannot honour a block reference, so the
// collector compares the observation time against a declared bound rather
// than assuming freshness.
//
// Nothing here fabricates a value. A provider that fails returns an error,
// which becomes Missing evidence, which blocks authorization. There is
// deliberately no fallback that invents a volatility number to keep a cycle
// moving — that would be the gateway deciding risk, which is not its job.
package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/arcbjorn/credence/gateway/evidence"
)

// MaxResponseBytes bounds a provider reply.
const MaxResponseBytes = 1 << 20

// Provider fetches raw candles. Split from the reader so volatility
// computation is testable without a network.
type Provider interface {
	// Candles returns recent closes, oldest first, with their timestamps.
	Candles(ctx context.Context) (closes []uint64, observedAt uint64, err error)
}

// Reader computes market state from provider candles.
type Reader struct {
	provider Provider
	// WindowSeconds is the declared volatility window. Published in the
	// evidence so a reader can see what the number describes.
	windowSeconds uint64
	// LiquidityDepth is a policy input rather than an observation: no
	// public feed reports the depth actually available to this protocol at
	// liquidation, so it is declared and labelled rather than estimated.
	liquidityDepth string
}

func NewReader(p Provider, windowSeconds uint64, liquidityDepth string) *Reader {
	return &Reader{provider: p, windowSeconds: windowSeconds, liquidityDepth: liquidityDepth}
}

// Market returns the current observation.
func (r *Reader) Market(ctx context.Context) (evidence.MarketState, error) {
	closes, observedAt, err := r.provider.Candles(ctx)
	if err != nil {
		return evidence.MarketState{}, fmt.Errorf("market: fetch candles: %w", err)
	}
	vol, err := RealizedVolatilityBps(closes)
	if err != nil {
		return evidence.MarketState{}, fmt.Errorf("market: %w", err)
	}
	return evidence.MarketState{
		ObservedAt:              observedAt,
		RealizedVolatilityBps:   vol,
		VolatilityWindowSeconds: r.windowSeconds,
		LiquidityDepth:          r.liquidityDepth,
	}, nil
}

// RealizedVolatilityBps computes volatility from a close series, in basis
// points, using integer arithmetic.
//
// A mean absolute return is used rather than a standard deviation: it needs
// no square root, so the whole computation stays in integers. `docs/04`
// forbids a floating-point path producing a value used in authorization,
// and this feeds the stress shock that sizes capacity.
//
// The measure is deliberately conservative in a different way too — mean
// absolute deviation understates tail risk relative to a standard
// deviation, which is why the stress policy applies a multiple and a floor
// on top rather than trusting this number alone.
func RealizedVolatilityBps(closes []uint64) (uint32, error) {
	if len(closes) < 2 {
		return 0, fmt.Errorf("volatility needs at least two closes, got %d", len(closes))
	}

	var totalBps uint64
	var samples uint64
	for i := 1; i < len(closes); i++ {
		previous := closes[i-1]
		if previous == 0 {
			// A zero close makes the return undefined. Refusing is better
			// than treating it as a 100% move or skipping it silently.
			return 0, fmt.Errorf("close %d is zero", i-1)
		}
		current := closes[i]
		var delta uint64
		if current > previous {
			delta = current - previous
		} else {
			delta = previous - current
		}
		// bps = |delta| * 10_000 / previous
		totalBps += delta * 10_000 / previous
		samples++
	}

	mean := totalBps / samples
	if mean > uint64(^uint32(0)) {
		return 0, fmt.Errorf("volatility %d overflows", mean)
	}
	return uint32(mean), nil
}

// HTTPProvider reads candles from a JSON endpoint.
type HTTPProvider struct {
	url  string
	http *http.Client
	// path names the JSON array of closes, so one implementation serves
	// providers with different response shapes.
	closesField string
	timeField   string
}

func NewHTTPProvider(url, closesField, timeField string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{
		url:         url,
		http:        &http.Client{Timeout: timeout},
		closesField: closesField,
		timeField:   timeField,
	}
}

func (p *HTTPProvider) Candles(ctx context.Context) ([]uint64, uint64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return nil, 0, err
	}
	res, err := p.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("provider unreachable: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("provider returned status %d", res.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(res.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read provider response: %w", err)
	}
	if len(raw) > MaxResponseBytes {
		return nil, 0, fmt.Errorf("provider response exceeds %d bytes", MaxResponseBytes)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, 0, fmt.Errorf("decode provider response: %w", err)
	}

	closesRaw, ok := doc[p.closesField]
	if !ok {
		return nil, 0, fmt.Errorf("provider response has no %q field", p.closesField)
	}
	var closes []uint64
	if err := json.Unmarshal(closesRaw, &closes); err != nil {
		return nil, 0, fmt.Errorf("decode closes: %w", err)
	}

	var observedAt uint64
	if timeRaw, ok := doc[p.timeField]; ok {
		if err := json.Unmarshal(timeRaw, &observedAt); err != nil {
			return nil, 0, fmt.Errorf("decode observation time: %w", err)
		}
	}
	if observedAt == 0 {
		// Without an observation time, skew cannot be measured, so the
		// collector cannot tell fresh data from stale. Refuse rather than
		// substitute the current clock, which would make every response
		// look fresh regardless of its age.
		return nil, 0, fmt.Errorf("provider response has no usable %q field", p.timeField)
	}

	return closes, observedAt, nil
}

// StaticProvider serves a fixed series. Used for scenario facilities, where
// synthetic evidence is permitted and labelled as such.
type StaticProvider struct {
	Closes     []uint64
	ObservedAt func() uint64
}

func (p StaticProvider) Candles(context.Context) ([]uint64, uint64, error) {
	if len(p.Closes) < 2 {
		return nil, 0, fmt.Errorf("static provider needs at least two closes")
	}
	at := uint64(time.Now().Unix())
	if p.ObservedAt != nil {
		at = p.ObservedAt()
	}
	// Copy so a caller cannot mutate the configured series between cycles,
	// which would make a scenario run irreproducible.
	out := make([]uint64, len(p.Closes))
	copy(out, p.Closes)
	return out, at, nil
}
