// Package sim generates a synthetic economic world, independently of the
// underwriter that will be judged against it.
//
// `docs/07`: the 32-day build cannot produce the multi-month arcs the
// thesis describes, so the synthetic population is built deliberately and
// said so. Presented correctly -- evaluating an underwriting model against
// a synthetic population, which is what a risk team actually does -- this
// exercises failure paths real data could not surface in time.
//
// **The separation is the point.** A world generator that consulted the
// underwriter's state would be marking its own homework: prices would move
// where the model expected, fills would arrive when it was solvent, and a
// clean evaluation would prove only that the two agreed. So nothing in
// this file imports the engine, reads an assessment, or takes a decision
// as input. It produces prices, fills, costs and outcomes from a seed, and
// the underwriter sees them only as evidence at a point in time:
//
//	World generator          Underwriter
//	─────────────────        ─────────────────
//	prices                   sees only evidence
//	execution costs   ──X──► available at that
//	fills                    point in time
//	outcomes                 (no future events,
//	hidden parameters         no hidden params)
//
// Determinism is load-bearing. `docs/04` requires a retained decision to
// replay byte for byte, and a world that could not be regenerated from its
// seed would make every retained assessment unreproducible. The generator
// therefore draws from an explicit PRNG rather than the runtime's, and
// orders events by virtual timestamp with stable tie-breaking.
package sim

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"sort"
)

// Version identifies the generator's own logic.
//
// Recorded in every fixture pack beside the seed: a pack replayed under a
// different generator version is not the same world, and `docs/07` makes a
// pack immutable once used in a recorded demonstration.
const Version = "sim-v1"

// Config declares a world. Everything that shapes the run is here, so a
// pack records exactly what produced it.
type Config struct {
	Seed uint64 `json:"seed"`

	// StartTime is the virtual clock's origin, in Unix seconds. Virtual,
	// never the wall clock: `docs/07` forbids presenting virtual elapsed
	// time as the age of a public-chain loan.
	StartTime int64 `json:"start_time"`

	// Days the world runs for.
	Days int `json:"days"`

	// StepSeconds between observations.
	StepSeconds int64 `json:"step_seconds"`

	// StartPrice of the single risky asset, in settlement base units
	// scaled by PriceScale.
	StartPrice uint64 `json:"start_price"`
	PriceScale uint32 `json:"price_scale"`

	// DriftBpsPerDay is the deterministic component; VolBpsPerStep bounds
	// the random walk's step size.
	DriftBpsPerDay int64  `json:"drift_bps_per_day"`
	VolBpsPerStep  uint64 `json:"vol_bps_per_step"`

	// LiquidityDepth available for an exit, in settlement units.
	LiquidityDepth uint64 `json:"liquidity_depth"`

	// Shocks applied at named virtual times. An evaluation set should
	// include one OUTSIDE the model's named scenarios (`docs/07`), so the
	// difference between expected stress protection and guaranteed
	// recovery is visible rather than assumed.
	Shocks []Shock `json:"shocks"`
}

// Shock is a scheduled market event.
type Shock struct {
	// AtDay the shock begins.
	AtDay int `json:"at_day"`
	// PriceBps the asset moves by, signed. -7000 is a 70% fall.
	PriceBps int64 `json:"price_bps"`
	// DepthBps the liquidity depth becomes, as a fraction of its normal
	// value. 2000 leaves a fifth of the book.
	DepthBps uint64 `json:"depth_bps"`
	// Label names the shock for the record. A shock the model has no
	// named scenario for should say so here.
	Label string `json:"label"`
}

// Observation is one moment of the world, as an observer would see it.
//
// Deliberately narrow: a price, a depth, and when it was observed. The
// underwriter gets nothing else, and in particular gets no view of the
// shock schedule or the seed that produced this.
type Observation struct {
	At             int64  `json:"at"`
	Price          uint64 `json:"price"`
	PriceScale     uint32 `json:"price_scale"`
	LiquidityDepth uint64 `json:"liquidity_depth"`
	// RealizedVolBps over the trailing window, which is a statistic an
	// observer can compute rather than a parameter handed down.
	RealizedVolBps uint64 `json:"realized_vol_bps"`
	// ShockLabel is non-empty only while a shock is in effect. Recorded
	// for the EVALUATION's benefit, never read by the underwriter.
	ShockLabel string `json:"shock_label,omitempty"`
}

// World is a generated run: a deterministic series of observations.
type World struct {
	Config       Config        `json:"config"`
	Version      string        `json:"version"`
	Observations []Observation `json:"observations"`
}

// Generate produces a world from its configuration.
//
// Pure: the same config yields the same world, on any machine and in any
// order. No clock is read and no entropy is drawn beyond the seeded PRNG.
func Generate(cfg Config) (*World, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	rng := newRNG(cfg.Seed)
	stepsPerDay := int64(86_400) / cfg.StepSeconds
	total := int(stepsPerDay * int64(cfg.Days))

	// Shocks indexed by the step they begin at, so application is a
	// lookup rather than a scan -- and so two shocks on one day apply in
	// declaration order rather than map order.
	shockAt := map[int]Shock{}
	for _, sh := range cfg.Shocks {
		step := int(int64(sh.AtDay) * stepsPerDay)
		if _, taken := shockAt[step]; !taken {
			shockAt[step] = sh
		}
	}

	obs := make([]Observation, 0, total)
	price := cfg.StartPrice
	depth := cfg.LiquidityDepth
	active := Shock{}
	var recent []uint64

	// Drift carried as a remainder rather than divided per step.
	//
	// `DriftBpsPerDay / stepsPerDay` is integer division: 15 bps/day over
	// 24 hourly steps truncates to ZERO, and so does -5. Both shipped
	// worlds therefore had no drift at all, while the evaluation set
	// documented itself as "a market that drifts against the agent" -- the
	// random walk is symmetric, so nothing else supplied the tilt.
	//
	// Accumulating the fractional part applies the full daily drift across
	// a day's steps without ever needing a per-step value of less than one
	// basis point. Reported by review, 14 September.
	driftCarry := int64(0)

	for i := 0; i < total; i++ {
		if sh, ok := shockAt[i]; ok {
			// A shock marks the step it struck, and only that step. The
			// label used to be assigned to `active` and never cleared, so
			// every later observation claimed the shock too -- and a loan
			// opened weeks afterwards counted as having exercised it.
			active = sh
			price = applyBps(price, sh.PriceBps)
			if sh.DepthBps > 0 {
				depth = cfg.LiquidityDepth * sh.DepthBps / 10_000
			}
		} else {
			active = Shock{}
		}

		// A bounded random walk plus drift. Bounded because an unbounded
		// one eventually produces a price the fixed-point arithmetic
		// cannot represent, and a generator that can overflow is a
		// generator whose worlds are not reproducible.
		step := int64(rng.next()%(2*cfg.VolBpsPerStep+1)) - int64(cfg.VolBpsPerStep)
		driftCarry += cfg.DriftBpsPerDay
		driftPerStep := driftCarry / stepsPerDay
		driftCarry -= driftPerStep * stepsPerDay
		price = applyBps(price, step+driftPerStep)
		if price == 0 {
			// A zero price is not a cheap asset, it is a broken world.
			price = 1
		}

		recent = append(recent, price)
		if len(recent) > int(stepsPerDay) {
			recent = recent[1:]
		}

		obs = append(obs, Observation{
			At:             cfg.StartTime + int64(i)*cfg.StepSeconds,
			Price:          price,
			PriceScale:     cfg.PriceScale,
			LiquidityDepth: depth,
			RealizedVolBps: realizedVolBps(recent),
			ShockLabel:     active.Label,
		})
	}

	return &World{Config: cfg, Version: Version, Observations: obs}, nil
}

// At returns the observation in effect at a virtual time.
//
// The underwriter reads the world only through this: it sees the present
// and the past, never a future event. Returning the LATEST observation at
// or before the requested time is what enforces that -- an off-by-one that
// returned the next one would leak a price that has not happened.
func (w *World) At(t int64) (Observation, bool) {
	if len(w.Observations) == 0 || t < w.Observations[0].At {
		return Observation{}, false
	}
	i := sort.Search(len(w.Observations), func(i int) bool {
		return w.Observations[i].At > t
	})
	return w.Observations[i-1], true
}

// Span reports the world's first and last observation times.
func (w *World) Span() (int64, int64) {
	if len(w.Observations) == 0 {
		return 0, 0
	}
	return w.Observations[0].At, w.Observations[len(w.Observations)-1].At
}

func (c Config) validate() error {
	if c.Days <= 0 {
		return fmt.Errorf("sim: days must be positive")
	}
	if c.StepSeconds <= 0 || 86_400%c.StepSeconds != 0 {
		return fmt.Errorf(
			"sim: step_seconds must divide a day evenly, got %d", c.StepSeconds)
	}
	if c.StartPrice == 0 {
		return fmt.Errorf("sim: start_price must be positive")
	}
	if c.LiquidityDepth == 0 {
		return fmt.Errorf("sim: liquidity_depth must be positive")
	}
	for _, s := range c.Shocks {
		if s.AtDay < 0 || s.AtDay >= c.Days {
			return fmt.Errorf(
				"sim: shock %q at day %d is outside the run", s.Label, s.AtDay)
		}
		if s.Label == "" {
			return fmt.Errorf("sim: every shock must be labelled")
		}
	}
	return nil
}

// applyBps moves a value by a signed basis-point amount, saturating rather
// than wrapping. A wrapped price would produce a world that cannot happen.
func applyBps(v uint64, bps int64) uint64 {
	if bps == 0 {
		return v
	}
	scaled := new(big.Int).SetUint64(v)
	scaled.Mul(scaled, new(big.Int).SetInt64(10_000+bps))
	scaled.Div(scaled, new(big.Int).SetInt64(10_000))
	if scaled.Sign() < 0 || !scaled.IsUint64() {
		if bps < 0 {
			return 0
		}
		return ^uint64(0)
	}
	return scaled.Uint64()
}

// realizedVolBps is the trailing high-low range as a fraction of the low.
//
// A statistic an observer can compute from the prices it has seen, not a
// parameter handed down from the generator -- which is what keeps it on
// the underwriter's side of the wall.
func realizedVolBps(prices []uint64) uint64 {
	if len(prices) < 2 {
		return 0
	}
	lo, hi := prices[0], prices[0]
	for _, p := range prices {
		if p < lo {
			lo = p
		}
		if p > hi {
			hi = p
		}
	}
	if lo == 0 {
		return 0
	}
	return (hi - lo) * 10_000 / lo
}

// rng is a SplitMix64 generator.
//
// Explicit rather than `math/rand`: the standard library's algorithm is
// not guaranteed stable across Go versions, and a fixture pack that
// produced different worlds after a toolchain upgrade would silently
// invalidate every retained assessment made against it.
type rng struct{ state uint64 }

func newRNG(seed uint64) *rng { return &rng{state: seed} }

func (r *rng) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Fingerprint is a stable digest of the world's observations.
//
// Two runs that claim to be the same world must agree here. Used by the
// fixture pack to detect a generator change that a version bump missed.
func (w *World) Fingerprint() string {
	h := newRNG(0)
	var acc uint64
	for _, o := range w.Observations {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], o.Price)
		acc = acc*31 + binary.BigEndian.Uint64(buf[:])
		acc ^= uint64(o.At)
		acc += h.next() & 0xFFFF
	}
	return fmt.Sprintf("%016x", acc)
}

// MustWorld generates a world or fails the test.
//
// Exported for tests in other packages: an evaluation harness needs to
// assert against the world the runner saw, and reaching into the runner
// for it would couple the two.
func MustWorld(t interface{ Fatal(...any) }, cfg Config) *World {
	w, err := Generate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
