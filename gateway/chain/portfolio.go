package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/arcbjorn/credence/gateway/evidence"
)

// PortfolioReader reads vault-wide capacity at a pinned block.
//
// `docs/03`: per-agent limits alone do not address many agents following
// one strategy. These are the lender-side constraints — they bound
// `drawable_capacity` and must never touch `agent_capacity`, which is why
// they are supplied as portfolio context rather than folded into the
// account.
//
// Every value is read at the caller's block, so vault cash and outstanding
// principal cannot come from different heights and imply a budget that
// never existed.
type PortfolioReader struct {
	rpc        *RPC
	vault      string
	strategyID string
}

func NewPortfolioReader(rpc *RPC, vault, strategyID string) *PortfolioReader {
	return &PortfolioReader{rpc: rpc, vault: vault, strategyID: strategyID}
}

type portfolioContext struct {
	StrategyID              string `json:"strategy_id"`
	RemainingStrategyBudget string `json:"remaining_strategy_budget"`
	RemainingVaultBudget    string `json:"remaining_vault_budget"`
	AvailableCash           string `json:"available_cash"`
}

// Portfolio reads the vault's remaining capacity.
func (p *PortfolioReader) Portfolio(
	ctx context.Context,
	at evidence.BlockRef,
) (json.RawMessage, error) {
	idleCash, err := p.readUint(ctx, "idleCash()", at.Number)
	if err != nil {
		return nil, fmt.Errorf("portfolio: idle cash: %w", err)
	}
	outstanding, err := p.readUint(ctx, "outstandingPrincipal()", at.Number)
	if err != nil {
		return nil, fmt.Errorf("portfolio: outstanding principal: %w", err)
	}
	globalCap, err := p.readUint(ctx, "globalCap()", at.Number)
	if err != nil {
		return nil, fmt.Errorf("portfolio: global cap: %w", err)
	}

	// Remaining budget saturates at zero. Outstanding principal can exceed
	// the cap after a cap reduction, and a negative budget would underflow
	// into an enormous positive one — the exact shape of bug that silently
	// authorizes everything.
	remaining := new(big.Int).Sub(globalCap, outstanding)
	if remaining.Sign() < 0 {
		remaining = big.NewInt(0)
	}

	return json.Marshal(portfolioContext{
		StrategyID: p.strategyID,
		// One strategy in v0, so its budget is the vault's. Kept as a
		// distinct field because `docs/03` reserves a place for
		// per-strategy limits once more than one exists.
		RemainingStrategyBudget: remaining.String(),
		RemainingVaultBudget:    remaining.String(),
		AvailableCash:           idleCash.String(),
	})
}

func (p *PortfolioReader) readUint(
	ctx context.Context,
	signature string,
	blockNumber uint64,
) (*big.Int, error) {
	out, err := p.rpc.CallContract(ctx, p.vault, selector(signature), blockNumber)
	if err != nil {
		return nil, err
	}
	return decodeUint(out, 0)
}
