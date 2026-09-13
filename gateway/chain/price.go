package chain

import (
	"context"
	"fmt"
	"math/big"
)

// PriceObservation is a price read from an on-chain source, with the time
// the source says it was observed.
//
// ObservedAt comes from the source rather than from the gateway's clock:
// a price is only as fresh as whoever published it, and substituting local
// time here would make a dead feed look current.
type PriceObservation struct {
	Price      *big.Int
	Scale      uint32
	ObservedAt uint64
}

// RiskyPriceAt reads an IPriceSource at a pinned block.
//
// This is an observation, not a policy input: unlike the declared
// RiskyAssetPolicy price, it moves when the market moves, which is what
// lets a price move trigger reassessment rather than merely a clock.
func (r *Reader) RiskyPriceAt(
	ctx context.Context,
	source string,
	at uint64,
) (PriceObservation, error) {
	var obs PriceObservation

	out, err := r.rpc.CallContract(ctx, source, selector("riskyPrice()"), at)
	if err != nil {
		return obs, fmt.Errorf("risky price: %w", err)
	}

	price, err := decodeUint(out, 0)
	if err != nil {
		return obs, fmt.Errorf("risky price: %w", err)
	}
	scale, err := decodeUint(out, 1)
	if err != nil {
		return obs, fmt.Errorf("risky price scale: %w", err)
	}
	observedAt, err := decodeUint(out, 2)
	if err != nil {
		return obs, fmt.Errorf("risky price observed at: %w", err)
	}

	// A zero price is not a cheap asset, it is an unconfigured or broken
	// source. Returning it would let stress sizing run against nothing.
	if price.Sign() == 0 {
		return obs, fmt.Errorf("risky price: source reported zero")
	}

	return PriceObservation{
		Price:      price,
		Scale:      uint32(scale.Uint64()),
		ObservedAt: observedAt.Uint64(),
	}, nil
}
