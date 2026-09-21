// Package loans encodes and reads the loan lifecycle.
//
// Extracted from `cmd/credence-scenario`, which had implemented all of
// it and could share none of it: code under `cmd/` cannot be imported,
// so a second caller would have copied the ABI encodings -- and a
// second copy of a selector is a second thing that can drift from the
// contract.
//
// **This package holds no authority.** It builds calldata and reads
// state. Every refusal still comes from the chain: `LoanManager` checks
// the oracle's permission, the binding, the state version and the
// facility caps, and nothing here can bypass any of that. A caller that
// asks to originate a loan the oracle did not authorize gets a reverted
// transaction, exactly as a caller using `cast` would.
//
// That property is what makes an HTTP surface over this package safe to
// add: the API can request, and only the contracts decide.
package loans

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/chain"
)

// EncodeRepay builds calldata for `repay(uint256)`.
func EncodeRepay(loanID uint64) []byte {
	out := append([]byte{}, attest.Selector("repay(uint256)")...)
	return append(out, attest.Word(new(big.Int).SetUint64(loanID))...)
}

// EncodeOriginate builds calldata for `originate(Binding, uint256)`.
//
// `Binding` is entirely static, so it inlines with no offset. The tuple
// is spelled out rather than built from a struct tag: the selector is
// part of the contract's interface, and writing it explicitly means a
// change to either side fails to compile rather than encoding garbage.
func EncodeOriginate(b attest.Binding, principal uint64) []byte {
	selector := attest.KeccakBytes([]byte(
		"originate((uint256,address,uint256,address,address,uint256,uint64," +
			"uint64,bytes32,uint64,bytes32),uint256)"))

	out := append([]byte{}, selector[:4]...)
	out = append(out, attest.Word(b.IdentityRegistryChainID)...)
	out = append(out, attest.AddressWord(b.IdentityRegistry)...)
	out = append(out, attest.Word(b.AgentID)...)
	out = append(out, attest.AddressWord(b.Operator)...)
	out = append(out, attest.AddressWord(b.CreditAccount)...)
	out = append(out, attest.Word(b.FacilityID)...)
	out = append(out, attest.Uint64Word(b.AccountStateVersion)...)
	out = append(out, attest.Uint64Word(b.ConfigVersion)...)
	out = append(out, b.ConfigHash[:]...)
	out = append(out, attest.Uint64Word(b.PolicyEpoch)...)
	out = append(out, b.PolicyHash[:]...)
	out = append(out, attest.Word(new(big.Int).SetUint64(principal))...)
	return out
}

// Status is what an account's credit looks like right now.
//
// Read from chain at one pinned block, never assembled from separate
// reads at different heights: a health figure that mixed two blocks
// could show a repaid loan with its old debt.
type Status struct {
	// LoanID is 0 when the account has no active loan.
	LoanID uint64 `json:"loan_id"`
	// DebtDue is principal plus accrued interest, in settlement base
	// units. Zero when there is no active loan.
	DebtDue string `json:"debt_due"`
	// Block is the height every figure above was read at.
	Block uint64 `json:"block"`
}

// Read reports an account's credit status at the latest block.
func Read(ctx context.Context, rpc *chain.RPC, manager, account string) (*Status, error) {
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("read latest block: %w", err)
	}

	loanID, err := ActiveLoan(ctx, rpc, manager, account, block.Number)
	if err != nil {
		return nil, err
	}

	status := &Status{LoanID: loanID, DebtDue: "0", Block: block.Number}
	if loanID == 0 {
		return status, nil
	}

	due, err := DebtDue(ctx, rpc, manager, loanID, block.Number)
	if err != nil {
		return nil, err
	}
	status.DebtDue = due.String()
	return status, nil
}

// ActiveLoan reads `activeLoan(address)`, returning 0 when there is none.
func ActiveLoan(
	ctx context.Context,
	rpc *chain.RPC,
	manager, account string,
	blockNumber uint64,
) (uint64, error) {
	accountAddr, err := ParseAddress(account)
	if err != nil {
		return 0, fmt.Errorf("parse account: %w", err)
	}
	data := append(attest.Selector("activeLoan(address)"),
		attest.AddressWord(accountAddr)...)
	out, err := rpc.CallContract(ctx, manager, data, blockNumber)
	if err != nil {
		return 0, fmt.Errorf("read activeLoan: %w", err)
	}
	return new(big.Int).SetBytes(out).Uint64(), nil
}

// DebtDue reads `debtDue(uint256)` at a pinned block.
func DebtDue(
	ctx context.Context,
	rpc *chain.RPC,
	manager string,
	loanID, blockNumber uint64,
) (*big.Int, error) {
	data := append(attest.Selector("debtDue(uint256)"),
		attest.Word(new(big.Int).SetUint64(loanID))...)
	out, err := rpc.CallContract(ctx, manager, data, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("read debtDue: %w", err)
	}
	// Short-read guard: an RPC that answered with fewer than 32 bytes
	// would otherwise produce a small number rather than an error, and a
	// debt figure that is quietly wrong is worse than one that is absent.
	if len(out) < 32 {
		return nil, fmt.Errorf("short debtDue result: %d bytes", len(out))
	}
	return new(big.Int).SetBytes(out[:32]), nil
}

// ParseAddress decodes a 0x-prefixed 20-byte address.
//
// Strict about the length: a short string would otherwise left-pad into
// a different, valid-looking address, and calldata built from it would
// read the wrong contract rather than fail.
func ParseAddress(s string) (attest.Address, error) {
	var a attest.Address
	trimmed := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	b, err := hex.DecodeString(trimmed)
	if err != nil {
		return a, fmt.Errorf("address %q is not hex: %w", s, err)
	}
	if len(b) != 20 {
		return a, fmt.Errorf("address %q is %d bytes, want 20", s, len(b))
	}
	copy(a[:], b)
	return a, nil
}
