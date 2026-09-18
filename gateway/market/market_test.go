package market_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arcbjorn/credence/gateway/market"
)

// A flat series has no volatility; the stress floor exists precisely
// because this number can legitimately be zero.
func TestFlatSeriesHasZeroVolatility(t *testing.T) {
	vol, err := market.RealizedVolatilityBps([]uint64{100, 100, 100, 100})
	if err != nil {
		t.Fatalf("volatility: %v", err)
	}
	if vol != 0 {
		t.Fatalf("flat series volatility = %d, want 0", vol)
	}
}

// A 1% move each step should read as roughly 100 bps.
func TestKnownMoveProducesKnownVolatility(t *testing.T) {
	vol, err := market.RealizedVolatilityBps([]uint64{10_000, 10_100, 10_201, 10_303})
	if err != nil {
		t.Fatalf("volatility: %v", err)
	}
	if vol < 95 || vol > 105 {
		t.Fatalf("volatility = %d bps, want about 100", vol)
	}
}

// Direction must not matter: a fall is as volatile as a rise.
func TestVolatilityIsDirectionless(t *testing.T) {
	up, _ := market.RealizedVolatilityBps([]uint64{10_000, 11_000})
	down, _ := market.RealizedVolatilityBps([]uint64{11_000, 10_000})
	if up == 0 || down == 0 {
		t.Fatal("both directions must register")
	}
	// The denominators differ, so they are close rather than equal.
	if up < down/2 || up > down*2 {
		t.Fatalf("directions diverge implausibly: up=%d down=%d", up, down)
	}
}

func TestTooFewClosesIsAnError(t *testing.T) {
	if _, err := market.RealizedVolatilityBps([]uint64{100}); err == nil {
		t.Fatal("one close cannot yield a return")
	}
	if _, err := market.RealizedVolatilityBps(nil); err == nil {
		t.Fatal("an empty series must be an error")
	}
}

// A zero close makes the return undefined. Refusing beats treating it as a
// 100% move or silently skipping it.
func TestZeroCloseIsRefused(t *testing.T) {
	if _, err := market.RealizedVolatilityBps([]uint64{0, 100}); err == nil {
		t.Fatal("a zero close must be refused")
	}
}

func provider(t *testing.T, body string, status int) *market.HTTPProvider {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return market.NewHTTPProvider(s.URL, "closes", "observed_at", 2*time.Second)
}

func TestHTTPProviderParsesCandles(t *testing.T) {
	p := provider(t, `{"closes":[10000,10100,10201],"observed_at":1700000000}`, http.StatusOK)
	closes, at, err := p.Candles(context.Background())
	if err != nil {
		t.Fatalf("candles: %v", err)
	}
	if len(closes) != 3 || at != 1700000000 {
		t.Fatalf("parsed %v at %d", closes, at)
	}
}

// Without an observation time the collector cannot measure skew, so a
// response that omits it must be refused rather than stamped with now —
// which would make every response look fresh.
func TestMissingObservationTimeIsRefused(t *testing.T) {
	p := provider(t, `{"closes":[10000,10100]}`, http.StatusOK)
	if _, _, err := p.Candles(context.Background()); err == nil {
		t.Fatal("a response with no observation time must be refused")
	}
}

func TestProviderErrorStatusIsSurfaced(t *testing.T) {
	p := provider(t, `{}`, http.StatusInternalServerError)
	if _, _, err := p.Candles(context.Background()); err == nil {
		t.Fatal("a non-200 status must surface as an error")
	}
}

func TestOversizedProviderResponseIsRefused(t *testing.T) {
	p := provider(t, `{"closes":[`+strings.Repeat("1,", market.MaxResponseBytes/2)+`1]}`,
		http.StatusOK)
	if _, _, err := p.Candles(context.Background()); err == nil {
		t.Fatal("an oversized response must be refused")
	}
}

// A failing provider yields an error, never a fabricated observation.
func TestReaderPropagatesProviderFailure(t *testing.T) {
	p := provider(t, `not json`, http.StatusOK)
	r := market.NewReader(p, 3600, "1000000000")
	if _, err := r.Market(context.Background()); err == nil {
		t.Fatal("a provider failure must not yield a market state")
	}
}

func TestReaderPublishesItsDeclaredWindow(t *testing.T) {
	r := market.NewReader(market.StaticProvider{
		Closes:     []uint64{10_000, 10_100},
		ObservedAt: func() uint64 { return 1700000000 },
	}, 3600, "5000000000")

	state, err := r.Market(context.Background())
	if err != nil {
		t.Fatalf("market: %v", err)
	}
	if state.VolatilityWindowSeconds != 3600 {
		t.Fatalf("window = %d, want 3600", state.VolatilityWindowSeconds)
	}
	if state.LiquidityDepth != "5000000000" {
		t.Fatalf("liquidity depth not published: %q", state.LiquidityDepth)
	}
	if state.ObservedAt != 1700000000 {
		t.Fatalf("observed_at = %d", state.ObservedAt)
	}
}

// A scenario run must be reproducible, so the static provider cannot be
// mutated through the slice it returns.
func TestStaticProviderIsImmutable(t *testing.T) {
	p := market.StaticProvider{Closes: []uint64{10_000, 10_100}}
	first, _, _ := p.Candles(context.Background())
	first[0] = 99_999
	second, _, _ := p.Candles(context.Background())
	if second[0] != 10_000 {
		t.Fatal("mutating a returned series must not affect the provider")
	}
}

// --- the failure modes `docs/11` names ------------------------------------
//
// "Exercise timeouts, partial responses, unexpected asset IDs, stale and
// conflicting prices." Status codes, oversized bodies and a missing
// observation time were already covered above; these are the rest.
//
// Each asserts the same property from a different direction: a provider
// that misbehaves yields an ERROR, never a fabricated observation. That
// is the only behaviour the engine can act on safely -- `docs/04` blocks
// risk on unavailable evidence, and a silently-zeroed volatility would
// read as a calm market instead.

// A provider that never answers must fail, not hang the collector.
func TestSlowProviderTimesOut(t *testing.T) {
	blocked := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked // hold the request open past the client's deadline
	}))
	t.Cleanup(func() {
		close(blocked)
		s.Close()
	})

	p := market.NewHTTPProvider(s.URL, "closes", "observed_at", 50*time.Millisecond)
	if _, _, err := p.Candles(context.Background()); err == nil {
		t.Fatal("a provider that never answers must surface an error")
	}
}

// A cancelled context must abort the read rather than outlive its caller.
func TestCancelledContextAbortsTheRead(t *testing.T) {
	blocked := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	t.Cleanup(func() {
		close(blocked)
		s.Close()
	})

	p := market.NewHTTPProvider(s.URL, "closes", "observed_at", time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.Candles(ctx); err == nil {
		t.Fatal("a cancelled context must abort the provider read")
	}
}

// A body that stops mid-JSON is a PARTIAL response: the bytes that did
// arrive parse as far as they go, and accepting them would hand the
// engine a truncated series as though it were the whole window.
func TestTruncatedResponseIsRefused(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "512")
		_, _ = w.Write([]byte(`{"closes":[10000,10100`))
	}))
	t.Cleanup(s.Close)

	p := market.NewHTTPProvider(s.URL, "closes", "observed_at", 2*time.Second)
	if _, _, err := p.Candles(context.Background()); err == nil {
		t.Fatal("a truncated response must be refused, not parsed as far as it goes")
	}
}

// The closes field named in the config is absent, because the provider
// answered about something else. An empty series must not read as a flat
// one: flat means zero volatility, which is the calmest possible market.
func TestUnexpectedResponseShapeIsRefused(t *testing.T) {
	p := provider(t, `{"prices":[10000,10100,10201],"observed_at":1700000000}`,
		http.StatusOK)
	if _, _, err := p.Candles(context.Background()); err == nil {
		t.Fatal("a response missing the configured closes field must be refused")
	}
}

// The same shape one layer up: whatever the provider did wrong, the
// reader yields no market state rather than a zero-volatility one.
func TestUnexpectedShapeYieldsNoMarketState(t *testing.T) {
	p := provider(t, `{"prices":[1,2,3],"observed_at":1700000000}`, http.StatusOK)
	r := market.NewReader(p, 3600, "1000000000")
	if _, err := r.Market(context.Background()); err == nil {
		t.Fatal("an unexpected response shape must not yield a market state")
	}
}
