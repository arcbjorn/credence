package evidence_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/scheduler"
	"github.com/arcbjorn/credence/gateway/snapshot"
)

type fakeChain struct {
	// priceObservedAt is the source timestamp of the price positions were
	// marked at. Zero means the deployment prices from policy, which has
	// no observation time to age.
	priceObservedAt uint64
	block      evidence.BlockRef
	blockErr   error
	accountErr error
	loanErr    error
	seqErr     error
	sequence   uint64
	// Records the block each read was given, to prove they all match.
	readBlocks []uint64
}

func (c *fakeChain) LatestBlock(context.Context) (evidence.BlockRef, error) {
	if c.blockErr != nil {
		return evidence.BlockRef{}, c.blockErr
	}
	return c.block, nil
}

func (c *fakeChain) AccountAt(_ context.Context, _ string, at evidence.BlockRef) (evidence.AccountState, error) {
	c.readBlocks = append(c.readBlocks, at.Number)
	if c.accountErr != nil {
		return evidence.AccountState{}, c.accountErr
	}
	return evidence.AccountState{
		SettlementBalance: "5000000000",
		StateVersion:      3,
		OutstandingDebt:   "1000000000",
		FacilityLimit:     "50000000000",
		PriceObservedAt:   c.priceObservedAt,
	}, nil
}

func (c *fakeChain) LoanAt(_ context.Context, id string, at evidence.BlockRef) (evidence.ActiveLoan, error) {
	c.readBlocks = append(c.readBlocks, at.Number)
	if c.loanErr != nil {
		return evidence.ActiveLoan{}, c.loanErr
	}
	return evidence.ActiveLoan{LoanID: id, OutstandingPrincipal: "1000000000", Apr: 1200}, nil
}

func (c *fakeChain) AcceptedSequence(_ context.Context, _ string, at evidence.BlockRef) (uint64, error) {
	c.readBlocks = append(c.readBlocks, at.Number)
	if c.seqErr != nil {
		return 0, c.seqErr
	}
	return c.sequence, nil
}

type fakeMarket struct {
	state evidence.MarketState
	err   error
}

func (m fakeMarket) Market(context.Context) (evidence.MarketState, error) {
	return m.state, m.err
}

type fakeHistory struct{ err error }

func (h fakeHistory) History(context.Context, string, evidence.BlockRef) (json.RawMessage, error) {
	if h.err != nil {
		return nil, h.err
	}
	return json.RawMessage(`{"weighted_repayments_ppm":500000}`), nil
}

func (h fakeHistory) Forecasts(context.Context, string) (json.RawMessage, error) {
	if h.err != nil {
		return nil, h.err
	}
	return json.RawMessage(`{"comparable_resolved":10}`), nil
}

func (h fakeHistory) Portfolio(context.Context, evidence.BlockRef) (json.RawMessage, error) {
	if h.err != nil {
		return nil, h.err
	}
	return json.RawMessage(`{"strategy_id":"spot-v1"}`), nil
}

func config() evidence.Config {
	return evidence.Config{
		SchemaVersion:   "v1",
		FacilityPolicy:  "Observed",
		RequestedClass:  "PrototypeEligible",
		MaxSkewSeconds:  60,
		ValiditySeconds: 3600,
	}
}

func binding(string) (evidence.BindingInfo, bool) {
	return evidence.BindingInfo{
		Raw:         json.RawMessage(`{"facility_id":"1"}`),
		ModelPolicy: json.RawMessage(`{"model_hash":"m","policy_hash":"p"}`),
		Account:     "0xacct",
	}, true
}

func task() scheduler.Task {
	return scheduler.Task{BindingKey: "b1", LoanID: "loan-1", Trigger: scheduler.TriggerStaleness}
}

func collect(t *testing.T, c *evidence.Collector) snapshot.Input {
	t.Helper()
	in, err := c.Collect(context.Background(), task(), 10_000)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return in
}

// docs/04: read critical account state at a common block reference. Every
// chain read must use the same height, or debt and balance could come from
// different moments.
func TestAllChainReadsUseOneBlockReference(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Hash: "0xb", Timestamp: 9_990}}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 9_990}},
		fakeHistory{}, binding, config())

	collect(t, c)

	if len(chain.readBlocks) < 3 {
		t.Fatalf("expected several pinned reads, got %d", len(chain.readBlocks))
	}
	for i, n := range chain.readBlocks {
		if n != 500 {
			t.Fatalf("read %d used block %d, want 500 — heights must not mix", i, n)
		}
	}
}

// The skew policy is declared and enforced: market data too far from the
// pinned block is Stale, not blended in.
func TestMarketBeyondSkewBoundIsStale(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Timestamp: 10_000}}
	// Observed 300s before the block, against a 60s bound.
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 9_700}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	if in.Market.State != snapshot.StateStale {
		t.Fatalf("market state = %s, want Stale", in.Market.State)
	}
	if in.Market.Value != nil {
		t.Fatal("stale evidence must not carry a usable value")
	}
}

func TestMarketWithinSkewBoundIsPresent(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Timestamp: 10_000}}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 9_970}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	if in.Market.State != snapshot.StatePresent {
		t.Fatalf("market state = %s, want Present", in.Market.State)
	}
}

// A failed read yields Missing — never a substituted value. Missing debt is
// unknown debt, not zero debt.
func TestFailedAccountReadIsMissingNotEmpty(t *testing.T) {
	chain := &fakeChain{
		block:      evidence.BlockRef{Number: 500, Timestamp: 10_000},
		accountErr: errors.New("rpc down"),
	}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 10_000}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	if in.Account.State != snapshot.StateMissing {
		t.Fatalf("account state = %s, want Missing", in.Account.State)
	}
	if in.Account.Value != nil {
		t.Fatal("a failed read must not produce a value")
	}
	// The origin of what would have been read is preserved.
	if in.Account.Origin != snapshot.OriginObserved {
		t.Fatalf("origin changed on failure: %s", in.Account.Origin)
	}
}

func TestFailedMarketReadIsMissing(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Timestamp: 10_000}}
	c := evidence.New(chain, fakeMarket{err: errors.New("provider down")},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	if in.Market.State != snapshot.StateMissing {
		t.Fatalf("market state = %s, want Missing", in.Market.State)
	}
}

// Several failures compound into blockers rather than one masking another.
func TestMultipleFailuresAllBlock(t *testing.T) {
	chain := &fakeChain{
		block:      evidence.BlockRef{Number: 500, Timestamp: 10_000},
		accountErr: errors.New("down"),
	}
	c := evidence.New(chain, fakeMarket{err: errors.New("down")},
		fakeHistory{err: errors.New("down")}, binding, config())

	in := collect(t, c)
	blocking := in.BlockingEvidence()
	if len(blocking) < 4 {
		t.Fatalf("expected every required source to block, got %v", blocking)
	}
}

// The context is always monitoring-shaped and never carries a request.
func TestContextIsAlwaysMonitoring(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Timestamp: 10_000}}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 10_000}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	var probe struct {
		Kind    string          `json:"kind"`
		Loan    json.RawMessage `json:"loan"`
		Request json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(in.Context, &probe); err != nil {
		t.Fatalf("context: %v", err)
	}
	if probe.Kind != "Monitoring" {
		t.Fatalf("kind = %q, want Monitoring", probe.Kind)
	}
	if len(probe.Request) != 0 {
		t.Fatal("a monitoring context must never carry a borrower request")
	}
	if len(probe.Loan) == 0 {
		t.Fatal("a monitoring context must identify its loan")
	}
}

// Even an unreadable loan keeps the context monitoring-shaped, so a read
// failure can never turn a health check into something that could fund.
func TestUnreadableLoanKeepsMonitoringShape(t *testing.T) {
	chain := &fakeChain{
		block:   evidence.BlockRef{Number: 500, Timestamp: 10_000},
		loanErr: errors.New("rpc down"),
	}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 10_000}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	var probe struct {
		Kind    string          `json:"kind"`
		Request json.RawMessage `json:"request"`
	}
	json.Unmarshal(in.Context, &probe)
	if probe.Kind != "Monitoring" || len(probe.Request) != 0 {
		t.Fatal("a failed loan read must not change the context kind")
	}
}

// The proposed sequence must exceed what the chain has accepted.
func TestProposedSequenceExceedsAccepted(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Timestamp: 10_000}, sequence: 7}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 10_000}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	if in.Proposal.Sequence != 8 {
		t.Fatalf("sequence = %d, want 8", in.Proposal.Sequence)
	}
}

// A snapshot that cannot be pinned to a block cannot be built at all.
func TestUnpinnableChainFailsCollection(t *testing.T) {
	chain := &fakeChain{blockErr: errors.New("rpc down")}
	c := evidence.New(chain, fakeMarket{}, fakeHistory{}, binding, config())

	if _, err := c.Collect(context.Background(), task(), 10_000); err == nil {
		t.Fatal("a snapshot with no block reference must not be built")
	}
}

func TestUnknownBindingFailsCollection(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500}}
	c := evidence.New(chain, fakeMarket{}, fakeHistory{},
		func(string) (evidence.BindingInfo, bool) { return evidence.BindingInfo{}, false },
		config())

	if _, err := c.Collect(context.Background(), task(), 10_000); err == nil {
		t.Fatal("an unknown binding must fail collection")
	}
}

// The assembled snapshot must satisfy the builder's own validation.
func TestCollectedSnapshotValidates(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Timestamp: 10_000}}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 10_000}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	if err := in.Validate(); err != nil {
		t.Fatalf("collected snapshot must validate: %v", err)
	}
	if in.SyntheticUsed() {
		t.Fatal("an observed collector must not produce synthetic evidence")
	}
}

// Evidence ids must be unique, or the engine rejects the snapshot.
func TestEvidenceIDsAreUnique(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Timestamp: 10_000}}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 10_000}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	seen := map[string]bool{}
	for _, id := range []string{
		in.Account.EvidenceID, in.Market.EvidenceID, in.History.EvidenceID,
		in.Forecasts.EvidenceID, in.Portfolio.EvidenceID,
	} {
		if seen[id] {
			t.Fatalf("duplicate evidence id %q", id)
		}
		seen[id] = true
	}
}

// Derived evidence arrives as JSON with integer amounts. Decoding it into
// `any` with json.Unmarshal turns every number into a float64, which the
// canonical encoder rejects — so the value would fail to hash and degrade
// to Missing with no indication of why. This pins the strict decode.
func TestIntegerDerivedEvidenceIsPresent(t *testing.T) {
	chain := &fakeChain{block: evidence.BlockRef{Number: 500, Timestamp: 10_000}}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 10_000}},
		fakeHistory{}, binding, config())

	in := collect(t, c)

	for name, ev := range map[string]snapshot.Evidence{
		"history":   in.History,
		"forecasts": in.Forecasts,
		"portfolio": in.Portfolio,
	} {
		if ev.State != snapshot.StatePresent {
			t.Errorf("%s state = %s, want Present — integer JSON must survive decoding",
				name, ev.State)
		}
		if ev.ContentHash == "" {
			t.Errorf("%s must hash successfully", name)
		}
	}

	// And the whole snapshot must still hash, which is what the engine
	// compares against.
	if _, err := in.InputHash(); err != nil {
		t.Fatalf("snapshot must hash: %v", err)
	}
}


// A position marked at a price the feed published long ago must not enter
// apparently fresh account evidence.
//
// The account is stamped with the BLOCK timestamp, so without this the
// price's own age is invisible -- and fresh independent market evidence
// does not vouch for the age of the price a position was marked at.
// `docs/11`: stale data blocks new risk.
//
// Reported by review, 14 September.
func TestStalePriceMakesAccountEvidenceStale(t *testing.T) {
	chain := &fakeChain{
		block: evidence.BlockRef{Number: 500, Timestamp: 10_000},
		// The price was published 300s before the block, against a 60s bound.
		priceObservedAt: 9_700,
	}
	c := evidence.New(chain, fakeMarket{state: evidence.MarketState{ObservedAt: 9_990}},
		fakeHistory{}, binding, config())

	in := collect(t, c)
	if in.Account.State != snapshot.StateStale {
		t.Fatalf("account state = %s, want Stale: a position marked at a "+
			"300s-old price is not fresh because the block is", in.Account.State)
	}
	if in.Account.Value != nil {
		t.Fatal("stale evidence must not carry a usable value")
	}
}

// A price inside the bound is fine, and a deployment pricing from policy
// has no observation time to age.
func TestFreshAndPolicyPricedAccountsAreNotStale(t *testing.T) {
	for _, c := range []struct {
		name       string
		observedAt uint64
	}{
		{"fresh feed price", 9_970},
		{"policy price, no feed", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			chain := &fakeChain{
				block:           evidence.BlockRef{Number: 500, Timestamp: 10_000},
				priceObservedAt: c.observedAt,
			}
			col := evidence.New(chain,
				fakeMarket{state: evidence.MarketState{ObservedAt: 9_990}},
				fakeHistory{}, binding, config())

			in := collect(t, col)
			if in.Account.State == snapshot.StateStale {
				t.Fatal("account evidence must not be stale here")
			}
		})
	}
}
