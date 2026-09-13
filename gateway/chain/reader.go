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
	// Risky is the one supported risky asset. Empty means the deployment
	// supports none, and accounts can hold only settlement tokens.
	Risky string
}

// RiskyAssetPolicy declares how the risky asset is valued.
//
// Price and haircut are policy inputs, not observations: this prototype has
// no oracle for the test asset, and inventing one would present a made-up
// number as a measurement. They are declared, versioned, and visible.
type RiskyAssetPolicy struct {
	// Price in settlement base units per whole risky unit.
	Price uint64
	// PriceScale is the decimal scale of Price.
	PriceScale uint32
	// Decimals of the risky token itself, which need not match the
	// settlement token's.
	Decimals   uint32
	HaircutBps uint32
}

// Reader implements evidence.ChainReader and publisher.Chain over JSON-RPC.
//
// Every read takes an explicit block, so the two interfaces cannot
// accidentally observe different heights. `publisher.Chain` is the one
// exception: reconciliation deliberately reads the newest state, because
// its question is "has this landed by now?" rather than "what was true at
// snapshot time?".
type Reader struct {
	rpc   *RPC
	addr  Addresses
	risky RiskyAssetPolicy
}

func NewReader(rpc *RPC, addr Addresses) *Reader {
	return &Reader{rpc: rpc, addr: addr}
}

// WithRiskyAsset sets the valuation policy for the risky asset.
func (r *Reader) WithRiskyAsset(policy RiskyAssetPolicy) *Reader {
	r.risky = policy
	return r
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

	// Read what the account actually holds. A hardcoded empty list would
	// report every account as unconcentrated no matter what it owns, so
	// the engine could never see a dangerous book.
	positions, err := r.positionsAt(ctx, account, at.Number)
	if err != nil {
		return state, fmt.Errorf("account positions: %w", err)
	}

	state.SettlementBalance = balance.String()
	state.StateVersion = stateVersion.Uint64()
	state.OutstandingDebt = debt.String()
	state.FacilityLimit = facilityLimit.String()
	state.Positions = positions
	state.Restricted = false
	_ = debtOutstanding
	return state, nil
}

// positionsAt reads the account's holdings of supported risky assets.
//
// Returns an empty slice when the deployment supports no risky asset, which
// is different from failing to read one: an unreadable balance is an error,
// because treating it as zero would understate concentration.
func (r *Reader) positionsAt(
	ctx context.Context,
	account string,
	blockNumber uint64,
) ([]evidence.Position, error) {
	if r.addr.Risky == "" {
		return []evidence.Position{}, nil
	}

	amount, err := r.callUint(ctx, account, "balanceOf(address)", blockNumber, r.addr.Risky)
	if err != nil {
		return nil, fmt.Errorf("risky balance: %w", err)
	}
	if amount.Sign() == 0 {
		return []evidence.Position{}, nil
	}

	// The engine marks a position as amount * price / 10^price_scale, so
	// the amount must be expressed in the same scale as the price. A raw
	// 18-decimal balance against a 6-decimal price would overstate the
	// position by twelve orders of magnitude.
	normalized := amount
	if r.risky.Decimals > r.risky.PriceScale {
		divisor := new(big.Int).Exp(big.NewInt(10),
			big.NewInt(int64(r.risky.Decimals-r.risky.PriceScale)), nil)
		normalized = new(big.Int).Div(amount, divisor)
	} else if r.risky.PriceScale > r.risky.Decimals {
		multiplier := new(big.Int).Exp(big.NewInt(10),
			big.NewInt(int64(r.risky.PriceScale-r.risky.Decimals)), nil)
		normalized = new(big.Int).Mul(amount, multiplier)
	}

	return []evidence.Position{{
		Asset:      r.addr.Risky,
		Amount:     normalized.String(),
		Price:      new(big.Int).SetUint64(r.risky.Price).String(),
		PriceScale: r.risky.PriceScale,
		HaircutBps: r.risky.HaircutBps,
	}}, nil
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

// AcceptedSequence reads the highest sequence the oracle accepted, at a
// pinned block.
//
// `chainKey` must be the oracle's own derived key — `attest.ChainKeyHex` —
// not the gateway's local binding identifier. Passing the wrong one reads
// an unrelated slot that is always zero, so a published assessment looks
// unpublished and the publisher retries a sequence the chain has already
// accepted. The validation below refuses a key that is not 32 bytes, which
// catches the string form immediately.
func (r *Reader) AcceptedSequence(
	ctx context.Context,
	chainKey string,
	at evidence.BlockRef,
) (uint64, error) {
	return r.acceptedSequenceAt(ctx, chainKey, at.Number)
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
//
// Takes the oracle's derived chain key, for the reason given on
// `Reader.AcceptedSequence`.
func (v *PublisherView) AcceptedSequence(ctx context.Context, chainKey string) (uint64, error) {
	block, err := v.reader.rpc.LatestBlock(ctx)
	if err != nil {
		return 0, err
	}
	return v.reader.acceptedSequenceAt(ctx, chainKey, block.Number)
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
	chainKey string,
	blockNumber uint64,
) (uint64, error) {
	key, err := encodeBytes32(chainKey)
	if err != nil {
		return 0, fmt.Errorf(
			"chain key must be the oracle's derived key (attest.ChainKeyHex), not a "+
				"local binding identifier: %w", err)
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

// TokenBalance reads an ERC-20 balance at a pinned block.
//
// The height is explicit for the same reason every other read pins one
// (`docs/04`): a balance combined with facts from a different height is
// not a consistent view of the account.
func (r *Reader) TokenBalance(
	ctx context.Context,
	token, holder string,
	at evidence.BlockRef,
) (*big.Int, error) {
	return r.callUint(ctx, token, "balanceOf(address)", at.Number, holder)
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
