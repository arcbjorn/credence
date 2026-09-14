package sim_test

import (
	"encoding/json"
	"testing"

	"github.com/arcbjorn/credence/gateway/sim"
)

func base() sim.Config {
	return sim.Config{
		Seed:           42,
		StartTime:      1_700_000_000,
		Days:           30,
		StepSeconds:    3_600,
		StartPrice:     2_000_000_000,
		PriceScale:     6,
		DriftBpsPerDay: 20,
		VolBpsPerStep:  40,
		LiquidityDepth: 10_000_000_000,
	}
}

// The same seed must produce the same world, byte for byte.
//
// `docs/04` requires a retained decision to replay exactly. A world that
// could not be regenerated from its seed would make every assessment made
// against it unreproducible, so this is the property the whole package
// rests on.
func TestGenerationIsDeterministic(t *testing.T) {
	a, err := sim.Generate(base())
	if err != nil {
		t.Fatal(err)
	}
	b, err := sim.Generate(base())
	if err != nil {
		t.Fatal(err)
	}

	if a.Fingerprint() != b.Fingerprint() {
		t.Fatalf("fingerprints differ: %s vs %s", a.Fingerprint(), b.Fingerprint())
	}

	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("two runs of the same config produced different worlds")
	}
}

// A different seed must produce a different world, or the seed is not
// doing anything and every "independent" evaluation set is the same set.
func TestSeedChangesTheWorld(t *testing.T) {
	a, _ := sim.Generate(base())

	cfg := base()
	cfg.Seed = 43
	b, _ := sim.Generate(cfg)

	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("two seeds produced the same world")
	}
}

// The underwriter sees the present and the past, never a future event.
//
// `docs/07` draws the wall explicitly: the generator knows the whole
// series, and an observer at time t must not be able to read across it.
// An off-by-one here would hand the model tomorrow's price.
func TestAtNeverReturnsAFutureObservation(t *testing.T) {
	w, err := sim.Generate(base())
	if err != nil {
		t.Fatal(err)
	}

	for _, o := range w.Observations {
		// Ask one second before this observation exists.
		got, ok := w.At(o.At - 1)
		if !ok {
			continue // before the world began, which is honest
		}
		if got.At >= o.At {
			t.Fatalf("at %d the observer saw %d, which had not happened",
				o.At-1, got.At)
		}
	}
}

func TestAtBeforeTheWorldBeganIsAbsent(t *testing.T) {
	w, _ := sim.Generate(base())
	first, _ := w.Span()
	if _, ok := w.At(first - 1); ok {
		t.Fatal("the observer saw something before the world began")
	}
}

// A shock must actually move the market, or a scenario set proves nothing.
func TestShockMovesPriceAndDepth(t *testing.T) {
	cfg := base()
	cfg.Shocks = []sim.Shock{{
		AtDay:    10,
		PriceBps: -7_000, // a 70% fall
		DepthBps: 2_000,  // a fifth of the book left
		Label:    "crash",
	}}
	w, err := sim.Generate(cfg)
	if err != nil {
		t.Fatal(err)
	}

	dayTen := cfg.StartTime + int64(10)*86_400
	before, _ := w.At(dayTen - 3_600)
	after, _ := w.At(dayTen + 3_600)

	if after.Price >= before.Price/2 {
		t.Fatalf("a 70%% shock left price %d against %d",
			after.Price, before.Price)
	}
	if after.LiquidityDepth >= before.LiquidityDepth {
		t.Fatalf("depth did not fall: %d -> %d",
			before.LiquidityDepth, after.LiquidityDepth)
	}
	if after.ShockLabel != "crash" {
		t.Fatalf("shock label = %q, want crash", after.ShockLabel)
	}
}

// `docs/07`: an evaluation set should include a shock OUTSIDE the model's
// named scenarios, so the difference between expected stress protection
// and guaranteed recovery is visible. The generator must be able to
// express one, which means an unlabelled or out-of-range shock is refused
// rather than silently dropped.
func TestMalformedConfigIsRefused(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*sim.Config)
	}{
		{"zero days", func(c *sim.Config) { c.Days = 0 }},
		{"step does not divide a day", func(c *sim.Config) { c.StepSeconds = 7_000 }},
		{"zero price", func(c *sim.Config) { c.StartPrice = 0 }},
		{"zero depth", func(c *sim.Config) { c.LiquidityDepth = 0 }},
		{"shock past the run", func(c *sim.Config) {
			c.Shocks = []sim.Shock{{AtDay: 99, Label: "late"}}
		}},
		{"unlabelled shock", func(c *sim.Config) {
			c.Shocks = []sim.Shock{{AtDay: 1}}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := base()
			c.mutate(&cfg)
			if _, err := sim.Generate(cfg); err == nil {
				t.Fatal("expected a refusal")
			}
		})
	}
}

// A price may fall a long way without reaching zero, because a zero price
// is not a cheap asset -- it is a world the arithmetic cannot represent.
func TestPriceNeverReachesZero(t *testing.T) {
	cfg := base()
	cfg.Shocks = []sim.Shock{
		{AtDay: 1, PriceBps: -9_900, Label: "collapse"},
		{AtDay: 2, PriceBps: -9_900, Label: "collapse-again"},
		{AtDay: 3, PriceBps: -9_900, Label: "and-again"},
	}
	w, err := sim.Generate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range w.Observations {
		if o.Price == 0 {
			t.Fatalf("price reached zero at %d", o.At)
		}
	}
}

// Realized volatility is a statistic an OBSERVER computes from prices it
// has seen, not a parameter handed down -- which is what keeps it on the
// underwriter's side of the wall.
func TestRealizedVolatilityRespondsToAShock(t *testing.T) {
	cfg := base()
	cfg.VolBpsPerStep = 1 // an almost flat world
	cfg.Shocks = []sim.Shock{{AtDay: 10, PriceBps: -5_000, Label: "drop"}}
	w, _ := sim.Generate(cfg)

	quiet, _ := w.At(cfg.StartTime + int64(9)*86_400)
	noisy, _ := w.At(cfg.StartTime + int64(10)*86_400 + 3_600)

	if noisy.RealizedVolBps <= quiet.RealizedVolBps {
		t.Fatalf("volatility did not respond: %d -> %d",
			quiet.RealizedVolBps, noisy.RealizedVolBps)
	}
}
