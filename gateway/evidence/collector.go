// Package evidence acquires observations and assembles monitoring
// snapshots.
//
// `docs/04` imposes one rule that shapes the whole design:
//
//	Read critical account state at a common block reference where possible;
//	record any source that cannot and apply a declared max age and skew
//	policy. Never combine a current debt amount with an older balance
//	without detecting the mismatch.
//
// So the collector pins a block reference first and reads every chain fact
// at that block. A source that cannot honour it is not silently accepted —
// its skew is measured against a declared bound, and evidence outside that
// bound is marked Stale rather than blended into a snapshot that looks
// coherent but is not.
//
// The collector never substitutes a value for a failed read. A provider
// that fails yields Missing evidence, which the engine turns into
// Unavailable. That is the fail-closed path, and it is the only one.
package evidence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/arcbjorn/credence/gateway/scheduler"
	"github.com/arcbjorn/credence/gateway/snapshot"
)

// BlockRef identifies the point in chain history a read was taken at.
type BlockRef struct {
	Number    uint64
	Hash      string
	Timestamp uint64
}

// AccountState is what the chain reports about a credit account.
type AccountState struct {
	SettlementBalance string     `json:"settlement_balance"`
	Positions         []Position `json:"positions"`
	StateVersion      uint64     `json:"state_version"`
	OutstandingDebt   string     `json:"outstanding_debt"`
	FacilityLimit     string     `json:"facility_limit"`
	Restricted        bool       `json:"restricted"`
}

type Position struct {
	Asset      string `json:"asset"`
	Amount     string `json:"amount"`
	Price      string `json:"price"`
	PriceScale uint32 `json:"price_scale"`
	HaircutBps uint32 `json:"haircut_bps"`
}

// MarketState is the observed market context.
type MarketState struct {
	ObservedAt              uint64 `json:"observed_at"`
	RealizedVolatilityBps   uint32 `json:"realized_volatility_bps"`
	VolatilityWindowSeconds uint64 `json:"volatility_window_seconds"`
	LiquidityDepth          string `json:"liquidity_depth"`
}

// ActiveLoan is the obligation being monitored.
type ActiveLoan struct {
	LoanID               string `json:"loan_id"`
	SettlementAsset      string `json:"settlement_asset"`
	OriginalPrincipal    string `json:"original_principal"`
	OutstandingPrincipal string `json:"outstanding_principal"`
	Apr                  uint32 `json:"apr"`
	OriginatedAt         uint64 `json:"originated_at"`
	MaturityAt           uint64 `json:"maturity_at"`
	InRecovery           bool   `json:"in_recovery"`
	InterestCollected    string `json:"interest_collected"`
}

// ChainReader reads contract state at a pinned block.
//
// Every method takes the block reference rather than reading "latest", so a
// snapshot cannot silently mix heights.
type ChainReader interface {
	// LatestBlock pins the reference all subsequent reads use.
	LatestBlock(ctx context.Context) (BlockRef, error)
	AccountAt(ctx context.Context, account string, at BlockRef) (AccountState, error)
	LoanAt(ctx context.Context, loanID string, at BlockRef) (ActiveLoan, error)
	// AcceptedSequence takes the oracle's derived chain key, not a local
	// binding identifier.
	AcceptedSequence(ctx context.Context, chainKey string, at BlockRef) (uint64, error)
}

// MarketReader supplies market observations. These come from providers that
// cannot honour a block reference, so their skew is measured instead.
type MarketReader interface {
	Market(ctx context.Context) (MarketState, error)
}

// HistoryReader supplies derived repayment history.
type HistoryReader interface {
	History(ctx context.Context, bindingKey string, at BlockRef) (json.RawMessage, error)
	Forecasts(ctx context.Context, bindingKey string) (json.RawMessage, error)
	Portfolio(ctx context.Context, at BlockRef) (json.RawMessage, error)
}

// BindingInfo is the static identity a snapshot is scoped to.
type BindingInfo struct {
	Raw         json.RawMessage
	ModelPolicy json.RawMessage
	Account     string
	// ChainKey is the oracle's own derived key, distinct from the
	// gateway's local binding identifier. Supplied by the lookup so the
	// collector never has to guess which of the two a caller meant.
	ChainKey string
}

// Config declares the policy the collector applies.
type Config struct {
	SchemaVersion string
	// FacilityPolicy is "Observed" or "Scenario". A scenario facility
	// accepts synthetic evidence; an observed one never does.
	FacilityPolicy string
	RequestedClass string
	ScenarioID     *string
	// MaxSkewSeconds bounds how far an off-chain observation may sit from
	// the pinned block before it is Stale. This is the declared policy
	// `docs/04` requires, not an implicit tolerance.
	MaxSkewSeconds uint64
	// ValiditySeconds is how long the resulting assessment may authorize.
	ValiditySeconds uint64
}

// Collector assembles monitoring snapshots.
type Collector struct {
	chain   ChainReader
	market  MarketReader
	history HistoryReader
	binding func(bindingKey string) (BindingInfo, bool)
	cfg     Config
}

func New(
	chain ChainReader,
	market MarketReader,
	history HistoryReader,
	binding func(string) (BindingInfo, bool),
	cfg Config,
) *Collector {
	return &Collector{chain: chain, market: market, history: history, binding: binding, cfg: cfg}
}

// Collect builds a monitoring snapshot for one scheduled task.
//
// Returns an error only when no snapshot can be built at all — an unknown
// binding, or a chain that cannot be reached to pin a block. Every other
// failure becomes Missing or Stale evidence inside the snapshot, so the
// engine still records a decision and the reason it was blocked.
func (c *Collector) Collect(
	ctx context.Context,
	task scheduler.Task,
	now int64,
) (snapshot.Input, error) {
	info, ok := c.binding(task.BindingKey)
	if !ok {
		return snapshot.Input{}, fmt.Errorf("unknown binding %q", task.BindingKey)
	}

	// Pin the block first. Every chain read below uses it, so debt and
	// balance can never come from different heights.
	block, err := c.chain.LatestBlock(ctx)
	if err != nil {
		return snapshot.Input{}, fmt.Errorf("pin block reference: %w", err)
	}

	ts := uint64(now)
	in := snapshot.Input{
		SchemaVersion:  c.cfg.SchemaVersion,
		FacilityPolicy: c.cfg.FacilityPolicy,
		RequestedClass: c.cfg.RequestedClass,
		AssessmentTime: ts,
		ScenarioID:     c.cfg.ScenarioID,
		Binding:        info.Raw,
		ModelPolicy:    info.ModelPolicy,
	}

	sequence, err := c.chain.AcceptedSequence(ctx, info.ChainKey, block)
	if err != nil {
		return snapshot.Input{}, fmt.Errorf("read accepted sequence: %w", err)
	}
	in.Proposal = snapshot.Proposal{
		Sequence:   sequence + 1,
		ValidAfter: ts,
		ValidUntil: ts + c.cfg.ValiditySeconds,
	}

	in.Account = c.collectAccount(ctx, info.Account, block, ts)
	in.Market = c.collectMarket(ctx, block, ts)
	in.History = c.collectRaw(ctx, "history", task.BindingKey, block, ts, c.history.History)
	in.Forecasts = c.collectForecasts(ctx, task.BindingKey, ts)
	in.Portfolio = c.collectPortfolio(ctx, block, ts)
	in.Context = c.collectLoanContext(ctx, task.LoanID, block)

	return in, nil
}

func (c *Collector) collectAccount(
	ctx context.Context,
	account string,
	block BlockRef,
	ts uint64,
) snapshot.Evidence {
	state, err := c.chain.AccountAt(ctx, account, block)
	if err != nil {
		// No substitution. An unreadable account is unknown, not empty.
		return snapshot.Missing("account", "chain", snapshot.OriginObserved, snapshot.Required, ts)
	}
	ev, err := snapshot.Present(
		"account", "chain", snapshot.OriginObserved, snapshot.Required,
		block.Timestamp, ts, state,
	)
	if err != nil {
		return snapshot.Missing("account", "chain", snapshot.OriginObserved, snapshot.Required, ts)
	}
	return ev
}

// collectMarket applies the skew policy. Market data comes from providers
// that cannot honour a block reference, so the gap between the observation
// and the pinned block is measured rather than assumed acceptable.
func (c *Collector) collectMarket(
	ctx context.Context,
	block BlockRef,
	ts uint64,
) snapshot.Evidence {
	state, err := c.market.Market(ctx)
	if err != nil {
		return snapshot.Missing("market", "provider", snapshot.OriginObserved, snapshot.Required, ts)
	}

	if skew(state.ObservedAt, block.Timestamp) > c.cfg.MaxSkewSeconds {
		// Detected, not blended. This is the mismatch `docs/04` requires be
		// caught rather than silently combined.
		ev := snapshot.Missing("market", "provider", snapshot.OriginObserved, snapshot.Required, ts)
		ev.State = snapshot.StateStale
		observed := state.ObservedAt
		ev.ObservedAt = &observed
		return ev
	}

	ev, err := snapshot.Present(
		"market", "provider", snapshot.OriginObserved, snapshot.Required,
		state.ObservedAt, ts, state,
	)
	if err != nil {
		return snapshot.Missing("market", "provider", snapshot.OriginObserved, snapshot.Required, ts)
	}
	return ev
}

func (c *Collector) collectRaw(
	ctx context.Context,
	id, bindingKey string,
	block BlockRef,
	ts uint64,
	read func(context.Context, string, BlockRef) (json.RawMessage, error),
) snapshot.Evidence {
	raw, err := read(ctx, bindingKey, block)
	if err != nil {
		return snapshot.Missing(id, "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return snapshot.Missing(id, "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	ev, err := snapshot.Present(id, "derived", snapshot.OriginObserved, snapshot.Required,
		block.Timestamp, ts, value)
	if err != nil {
		return snapshot.Missing(id, "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	return ev
}

func (c *Collector) collectForecasts(
	ctx context.Context,
	bindingKey string,
	ts uint64,
) snapshot.Evidence {
	raw, err := c.history.Forecasts(ctx, bindingKey)
	if err != nil {
		return snapshot.Missing("forecasts", "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return snapshot.Missing("forecasts", "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	ev, err := snapshot.Present("forecasts", "derived", snapshot.OriginObserved,
		snapshot.Required, ts, ts, value)
	if err != nil {
		return snapshot.Missing("forecasts", "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	return ev
}

func (c *Collector) collectPortfolio(
	ctx context.Context,
	block BlockRef,
	ts uint64,
) snapshot.Evidence {
	raw, err := c.history.Portfolio(ctx, block)
	if err != nil {
		return snapshot.Missing("portfolio", "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return snapshot.Missing("portfolio", "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	ev, err := snapshot.Present("portfolio", "derived", snapshot.OriginObserved,
		snapshot.Required, block.Timestamp, ts, value)
	if err != nil {
		return snapshot.Missing("portfolio", "derived", snapshot.OriginObserved, snapshot.Required, ts)
	}
	return ev
}

// collectLoanContext builds the monitoring context. It carries the active
// loan and never a borrower request — monitoring cannot fund a loan.
func (c *Collector) collectLoanContext(
	ctx context.Context,
	loanID string,
	block BlockRef,
) json.RawMessage {
	loan, err := c.chain.LoanAt(ctx, loanID, block)
	if err != nil {
		// The context is still monitoring-shaped, with the loan identified
		// but unread. Account evidence will block the assessment; what must
		// not happen is the context silently becoming something else.
		raw, _ := json.Marshal(map[string]any{
			"kind": "Monitoring",
			"loan": map[string]any{"loan_id": loanID},
		})
		return raw
	}
	raw, _ := json.Marshal(map[string]any{"kind": "Monitoring", "loan": loan})
	return raw
}

func skew(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
