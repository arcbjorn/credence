package chain

import (
	"context"
	"fmt"
	"math/big"

	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/publisher"
)

// Addresses of the contracts this reader queries.
type Addresses struct {
	Oracle        string
	LoanManager   string
	CreditAccount string
	Settlement    string
	Vault         string
}

// Reader implements evidence.ChainReader and publisher.Chain over JSON-RPC.
//
// Every read takes an explicit block, so the two interfaces cannot
// accidentally observe different heights. `publisher.Chain` is the one
// exception: reconciliation deliberately reads the newest state, because
// its question is "has this landed by now?" rather than "what was true at
// snapshot time?".
type Reader struct {
	rpc  *RPC
	addr Addresses
}

func NewReader(rpc *RPC, addr Addresses) *Reader {
	return &Reader{rpc: rpc, addr: addr}
}

// --- evidence.ChainReader ---

func (r *Reader) LatestBlock(ctx context.Context) (evidence.BlockRef, error) {
	b, err := r.rpc.LatestBlock(ctx)
	if err != nil {
		return evidence.BlockRef{}, err
	}
	return evidence.BlockRef{Number: b.Number, Hash: b.Hash, Timestamp: b.Timestamp}, nil
}

// AccountAt reads credit-account state at a pinned block.
//
// Balance, debt, and state version are read at the same height, which is
// what `docs/04` requires: never combine a current debt amount with an
// older balance.
func (r *Reader) AccountAt(
	ctx context.Context,
	account string,
	at evidence.BlockRef,
) (evidence.AccountState, error) {
	var state evidence.AccountState

	balance, err := r.callUint(ctx, account, "balanceOf(address)", at.Number, r.addr.Settlement)
	if err != nil {
		return state, fmt.Errorf("account balance: %w", err)
	}
	stateVersion, err := r.callUint(ctx, account, "stateVersion()", at.Number)
	if err != nil {
		return state, fmt.Errorf("account state version: %w", err)
	}
	debtOutstanding, err := r.callBool(ctx, account, "debtOutstanding()", at.Number)
	if err != nil {
		return state, fmt.Errorf("account debt flag: %w", err)
	}

	// Debt and facility limit come from the contracts that own that
	// accounting, read at the same block as the balance. Leaving them unset
	// would serialize as empty strings and the engine would reject the
	// snapshot — but worse, a zero default would understate leverage.
	debt, err := r.outstandingDebtAt(ctx, account, at.Number)
	if err != nil {
		return state, fmt.Errorf("account debt: %w", err)
	}
	facilityLimit, err := r.callUint(ctx, r.addr.LoanManager, "globalCap()", at.Number)
	if err != nil {
		// The vault owns the global cap; the loan manager does not expose
		// one, so fall back to the vault's.
		facilityLimit, err = r.callUint(ctx, r.addr.Vault, "globalCap()", at.Number)
		if err != nil {
			return state, fmt.Errorf("facility limit: %w", err)
		}
	}

	state.SettlementBalance = balance.String()
	state.StateVersion = stateVersion.Uint64()
	state.OutstandingDebt = debt.String()
	state.FacilityLimit = facilityLimit.String()
	state.Positions = []evidence.Position{}
	state.Restricted = false
	_ = debtOutstanding
	return state, nil
}

// outstandingDebtAt reads what the account currently owes.
//
// Zero is only correct when the loan manager reports no active loan. A
// failed read must not become zero, because that would understate leverage
// and inflate capacity (`docs/04`).
func (r *Reader) outstandingDebtAt(
	ctx context.Context,
	account string,
	blockNumber uint64,
) (*big.Int, error) {
	loanID, err := r.callUint(ctx, r.addr.LoanManager, "activeLoan(address)", blockNumber, account)
	if err != nil {
		return nil, fmt.Errorf("active loan: %w", err)
	}
	if loanID.Sign() == 0 {
		return big.NewInt(0), nil
	}

	data := append(selector("debtDue(uint256)"), encodeUint(loanID)...)
	out, err := r.rpc.CallContract(ctx, r.addr.LoanManager, data, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("debt due: %w", err)
	}
	return decodeUint(out, 0)
}

// LoanAt reads an active loan at a pinned block.
//
// Every amount is read, not defaulted. An unset field serializes as an
// empty string and the engine rejects the snapshot — which is the safe
// failure. The dangerous one would be defaulting to zero, which would
// report an outstanding loan as fully repaid.
func (r *Reader) LoanAt(
	ctx context.Context,
	loanID string,
	at evidence.BlockRef,
) (evidence.ActiveLoan, error) {
	var loan evidence.ActiveLoan
	id, ok := new(big.Int).SetString(loanID, 10)
	if !ok {
		return loan, fmt.Errorf("loan id %q is not numeric", loanID)
	}

	// loans(uint256) returns the full struct; decode the fields the engine
	// needs by word position.
	data := append(selector("loans(uint256)"), encodeUint(id)...)
	out, err := r.rpc.CallContract(ctx, r.addr.LoanManager, data, at.Number)
	if err != nil {
		return loan, fmt.Errorf("read loan: %w", err)
	}

	// Word layout matches the Loan struct in LoanManager.sol.
	principal, err := decodeUint(out, 1)
	if err != nil {
		return loan, fmt.Errorf("loan principal: %w", err)
	}
	outstanding, err := decodeUint(out, 2)
	if err != nil {
		return loan, fmt.Errorf("loan outstanding: %w", err)
	}
	interestCollected, err := decodeUint(out, 3)
	if err != nil {
		return loan, fmt.Errorf("loan interest collected: %w", err)
	}
	aprBps, err := decodeUint(out, 4)
	if err != nil {
		return loan, fmt.Errorf("loan apr: %w", err)
	}
	originatedAt, err := decodeUint(out, 5)
	if err != nil {
		return loan, fmt.Errorf("loan originated at: %w", err)
	}
	termSeconds, err := decodeUint(out, 6)
	if err != nil {
		return loan, fmt.Errorf("loan term: %w", err)
	}
	recoveryStartedAt, err := decodeUint(out, 7)
	if err != nil {
		return loan, fmt.Errorf("loan recovery state: %w", err)
	}

	loan.LoanID = loanID
	loan.SettlementAsset = r.addr.Settlement
	loan.OriginalPrincipal = principal.String()
	loan.OutstandingPrincipal = outstanding.String()
	loan.InterestCollected = interestCollected.String()
	loan.Apr = uint32(aprBps.Uint64())
	loan.OriginatedAt = originatedAt.Uint64()
	loan.MaturityAt = originatedAt.Uint64() + termSeconds.Uint64()
	loan.InRecovery = recoveryStartedAt.Sign() != 0

	return loan, nil
}

// AcceptedSequence reads the highest sequence the oracle accepted for a
// binding, at a pinned block.
func (r *Reader) AcceptedSequence(
	ctx context.Context,
	bindingKey string,
	at evidence.BlockRef,
) (uint64, error) {
	return r.acceptedSequenceAt(ctx, bindingKey, at.Number)
}

// --- publisher.Chain ---

// PublisherView adapts a Reader to publisher.Chain.
//
// The two interfaces both declare AcceptedSequence with different
// signatures — evidence pins a block, the publisher does not — so one type
// cannot satisfy both. That is not an accident to paper over: the
// collector asks "what was true at snapshot time?" while reconciliation
// asks "has this landed by now?". Different questions deserve different
// methods, and this adapter keeps both honest.
type PublisherView struct {
	reader *Reader
	submit func(ctx context.Context, payload, signature []byte) (string, error)
}

// ForPublisher returns the publisher-facing view. `submit` is supplied by
// the caller because transaction signing and nonce management belong to the
// wallet, not to a read-oriented client.
func (r *Reader) ForPublisher(
	submit func(ctx context.Context, payload, signature []byte) (string, error),
) *PublisherView {
	return &PublisherView{reader: r, submit: submit}
}

// AcceptedSequence reads the newest state, deliberately unpinned.
func (v *PublisherView) AcceptedSequence(ctx context.Context, bindingKey string) (uint64, error) {
	block, err := v.reader.rpc.LatestBlock(ctx)
	if err != nil {
		return 0, err
	}
	return v.reader.acceptedSequenceAt(ctx, bindingKey, block.Number)
}

func (v *PublisherView) SubmitAccept(
	ctx context.Context,
	payload, signature []byte,
) (string, error) {
	if v.submit == nil {
		return "", fmt.Errorf("chain: no submitter configured")
	}
	return v.submit(ctx, payload, signature)
}

func (v *PublisherView) ReceiptStatus(
	ctx context.Context,
	txHash string,
) (publisher.Status, error) {
	return v.reader.ReceiptStatus(ctx, txHash)
}

func (r *Reader) acceptedSequenceAt(
	ctx context.Context,
	bindingKey string,
	blockNumber uint64,
) (uint64, error) {
	key, err := encodeBytes32(bindingKey)
	if err != nil {
		return 0, fmt.Errorf("binding key: %w", err)
	}
	data := append(selector("lastSequence(bytes32)"), key...)
	out, err := r.rpc.CallContract(ctx, r.addr.Oracle, data, blockNumber)
	if err != nil {
		return 0, fmt.Errorf("last sequence: %w", err)
	}
	v, err := decodeUint(out, 0)
	if err != nil {
		return 0, fmt.Errorf("last sequence: %w", err)
	}
	return v.Uint64(), nil
}

// ReceiptStatus maps a receipt onto the publisher's status vocabulary.
//
// An unmined transaction is Pending, never Failed. Reporting it as failed
// is what would cause a resubmission of something already in flight.
func (r *Reader) ReceiptStatus(ctx context.Context, txHash string) (publisher.Status, error) {
	receipt, err := r.rpc.TransactionReceipt(ctx, txHash)
	if err != nil {
		return "", err
	}
	if !receipt.Found {
		return publisher.StatusPending, nil
	}
	if receipt.Status == 1 {
		return publisher.StatusConfirmed, nil
	}
	return publisher.StatusFailed, nil
}

func (r *Reader) callUint(
	ctx context.Context,
	to, signature string,
	blockNumber uint64,
	args ...string,
) (*big.Int, error) {
	data := selector(signature)
	for _, a := range args {
		encoded, err := encodeAddress(a)
		if err != nil {
			return nil, err
		}
		data = append(data, encoded...)
	}
	out, err := r.rpc.CallContract(ctx, to, data, blockNumber)
	if err != nil {
		return nil, err
	}
	return decodeUint(out, 0)
}

func (r *Reader) callBool(
	ctx context.Context,
	to, signature string,
	blockNumber uint64,
) (bool, error) {
	out, err := r.rpc.CallContract(ctx, to, selector(signature), blockNumber)
	if err != nil {
		return false, err
	}
	return decodeBool(out, 0)
}
