// Package sources composes the evidence readers the collector needs.
//
// `evidence.HistoryReader` bundles three unrelated reads — repayment
// history, forecast quality, and vault capacity — because the collector
// consumes them together. They come from different places: the first two
// are the gateway's own records, the third is chain state. Composing them
// here keeps that split visible instead of hiding it behind one type that
// pretends they share an origin.
package sources

import (
	"context"
	"encoding/json"

	"github.com/arcbjorn/credence/gateway/evidence"
)

// Derived supplies the gateway's own records.
type Derived interface {
	History(ctx context.Context, bindingKey string) (json.RawMessage, error)
	Forecasts(ctx context.Context, bindingKey string) (json.RawMessage, error)
}

// Portfolio supplies vault-wide capacity from chain state.
type Portfolio interface {
	Portfolio(ctx context.Context, at evidence.BlockRef) (json.RawMessage, error)
}

// History implements evidence.HistoryReader over both.
type History struct {
	derived   Derived
	portfolio Portfolio
}

func NewHistory(d Derived, p Portfolio) *History {
	return &History{derived: d, portfolio: p}
}

// History ignores the block reference: repayment history is the gateway's
// own append-only record, not chain state, so pinning it to a height would
// imply a guarantee this source cannot make.
func (h *History) History(
	ctx context.Context,
	bindingKey string,
	_ evidence.BlockRef,
) (json.RawMessage, error) {
	return h.derived.History(ctx, bindingKey)
}

func (h *History) Forecasts(ctx context.Context, bindingKey string) (json.RawMessage, error) {
	return h.derived.Forecasts(ctx, bindingKey)
}

func (h *History) Portfolio(
	ctx context.Context,
	at evidence.BlockRef,
) (json.RawMessage, error) {
	return h.portfolio.Portfolio(ctx, at)
}
