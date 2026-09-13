// Command credence-publish writes resolved obligations to the feedback
// registry.
//
// `docs/06`: attributable events with loan IDs, amounts, durations,
// outcome classes, and evidence references. **Contract state is
// authoritative even if registry publication lags**, so this reads the
// loan manager rather than a local cache — a queue that trusted its own
// record could publish an outcome the chain never had.
//
// Idempotent by construction: the registry refuses a second entry for the
// same loan, so a retry after an ambiguous send cannot double-count.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/wallet"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Outcome classes, matching FeedbackRegistry.Outcome.
const (
	outcomeRepaid    = 0
	outcomeRecovered = 1
	outcomeDefaulted = 2
)

// Loan statuses in LoanManager.sol.
const (
	statusRepaid    = 2
	statusRecovered = 4
	statusDefaulted = 5
)

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:8546", "chain rpc")
	manager := flag.String("manager", "", "loan manager address")
	registry := flag.String("registry", "", "feedback registry address")
	agentID := flag.Uint64("agent", 184, "agent id the loans belong to")
	fromLoan := flag.Uint64("from", 1, "first loan id to consider")
	toLoan := flag.Uint64("to", 0, "last loan id; 0 means read the manager's counter")
	chainID := flag.Uint64("chain-id", 31337, "chain id")
	flag.Parse()

	if err := run(*rpcURL, *manager, *registry, *agentID, *fromLoan, *toLoan, *chainID); err != nil {
		log.Fatalf("publish: %v", err)
	}
}

func run(rpcURL, manager, registry string, agentID, fromLoan, toLoan, chainID uint64) error {
	ctx := context.Background()
	rpc := chain.NewRPC(rpcURL, 15*time.Second)
	reader := chain.NewReader(rpc, chain.Addresses{LoanManager: manager})

	key, err := keyFromEnv("CREDENCE_OPERATOR_KEY")
	if err != nil {
		return err
	}
	w := wallet.New(key, rpc, wallet.Config{
		ChainID:     new(big.Int).SetUint64(chainID),
		GasLimitCap: 2_000_000,
		GasPriceCap: big.NewInt(500_000_000_000),
	})

	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("pin block: %w", err)
	}

	if toLoan == 0 {
		next, err := callUint(ctx, rpc, manager, "nextLoanId()", block.Number)
		if err != nil {
			return fmt.Errorf("read loan counter: %w", err)
		}
		if next.Uint64() == 0 {
			return nil
		}
		toLoan = next.Uint64() - 1
	}

	published := 0
	for id := fromLoan; id <= toLoan; id++ {
		entry, resolved, err := readResolved(
			ctx, rpc, manager, id, agentID, block.Number)
		if err != nil {
			return fmt.Errorf("loan %d: %w", id, err)
		}
		if !resolved {
			// Still active or recovering. `docs/06` publishes resolved
			// outcomes; an in-flight loan is not an outcome.
			continue
		}

		txHash, err := w.Send(ctx, registry, encodePublish(entry))
		if err != nil {
			// The registry refuses a duplicate, which is the idempotency
			// guarantee working rather than a failure to report.
			if strings.Contains(err.Error(), "reverted") {
				log.Printf("loan %d: already published", id)
				continue
			}
			return fmt.Errorf("loan %d: publish: %w", id, err)
		}
		if err := waitMined(ctx, reader, txHash); err != nil {
			return fmt.Errorf("loan %d: publish: %w", id, err)
		}
		log.Printf("loan %d: published %s (principal=%s interest=%s loss=%s)",
			id, outcomeName(entry.Outcome), entry.Principal, entry.InterestPaid,
			entry.PrincipalLoss)
		published++
	}

	fmt.Printf("published=%d\n", published)
	return nil
}

// feedback mirrors FeedbackRegistry.Feedback.
type feedback struct {
	AgentID       *big.Int
	CreditAccount attest.Address
	LoanID        *big.Int
	Outcome       uint8
	Principal     *big.Int
	InterestPaid  *big.Int
	PrincipalLoss *big.Int
	OriginatedAt  uint64
	ResolvedAt    uint64
	EvidenceRef   attest.Hash
}

// readResolved builds one entry from chain state, or reports that the loan
// has not resolved.
//
// Amounts come from the manager's own struct rather than being
// reconstructed: the contract is authoritative, and a recomputed figure
// would be a second opinion published as fact.
func readResolved(
	ctx context.Context,
	rpc *chain.RPC,
	manager string,
	loanID, agentID, blockNumber uint64,
) (feedback, bool, error) {
	var f feedback

	id := new(big.Int).SetUint64(loanID)
	data := append(selector("loans(uint256)"), word(id)...)
	out, err := rpc.CallContract(ctx, manager, data, blockNumber)
	if err != nil {
		return f, false, fmt.Errorf("read loan: %w", err)
	}
	if len(out) < 32*11 {
		return f, false, fmt.Errorf("short loan struct: %d bytes", len(out))
	}

	// Word layout matches LoanManager.Loan.
	account := wordAt(out, 0)
	principal := new(big.Int).SetBytes(wordAt(out, 1))
	interestCollected := new(big.Int).SetBytes(wordAt(out, 3))
	originatedAt := new(big.Int).SetBytes(wordAt(out, 5)).Uint64()
	resolvedAt := new(big.Int).SetBytes(wordAt(out, 9)).Uint64()
	status := new(big.Int).SetBytes(wordAt(out, 10)).Uint64()

	var outcome uint8
	switch status {
	case statusRepaid:
		outcome = outcomeRepaid
	case statusRecovered:
		outcome = outcomeRecovered
	case statusDefaulted:
		outcome = outcomeDefaulted
	default:
		return f, false, nil
	}

	// A default's loss is principal that never came back.
	loss := big.NewInt(0)
	if outcome == outcomeDefaulted {
		loss = new(big.Int).Sub(principal, interestCollected)
		if loss.Sign() < 0 {
			loss = big.NewInt(0)
		}
	}

	var acct attest.Address
	copy(acct[:], account[12:])

	return feedback{
		AgentID:       new(big.Int).SetUint64(agentID),
		CreditAccount: acct,
		LoanID:        id,
		Outcome:       outcome,
		Principal:     principal,
		InterestPaid:  interestCollected,
		PrincipalLoss: loss,
		OriginatedAt:  originatedAt,
		// When the loan actually resolved, from the contract. Pairing a
		// chain `originatedAt` with a wall clock produces a NEGATIVE
		// duration on any chain whose time has advanced, and duration is
		// one of the facts `docs/06` publishes.
		ResolvedAt: resolvedAt,
		// The retained assessment this outcome belongs to. Keyed by loan
		// so a reader can demand the record rather than take the claim.
		EvidenceRef: attest.KeccakText(fmt.Sprintf("loan-%d", loanID)),
	}, true, nil
}

func encodePublish(f feedback) []byte {
	sig := "publish((uint256,address,uint256,uint8,uint256,uint256,uint256," +
		"uint64,uint64,bytes32))"
	out := append([]byte{}, selector(sig)...)
	out = append(out, word(f.AgentID)...)
	out = append(out, attest.AddressWord(f.CreditAccount)...)
	out = append(out, word(f.LoanID)...)
	out = append(out, word(big.NewInt(int64(f.Outcome)))...)
	out = append(out, word(f.Principal)...)
	out = append(out, word(f.InterestPaid)...)
	out = append(out, word(f.PrincipalLoss)...)
	out = append(out, word(new(big.Int).SetUint64(f.OriginatedAt))...)
	out = append(out, word(new(big.Int).SetUint64(f.ResolvedAt))...)
	out = append(out, f.EvidenceRef[:]...)
	return out
}

func outcomeName(o uint8) string {
	switch o {
	case outcomeRepaid:
		return "Repaid"
	case outcomeRecovered:
		return "Recovered"
	case outcomeDefaulted:
		return "Defaulted"
	}
	return "Unknown"
}

func selector(sig string) []byte {
	h := attest.KeccakText(sig)
	return h[:4]
}

func word(v *big.Int) []byte { return attest.Word(v) }

func wordAt(b []byte, i int) []byte {
	start := i * 32
	if start+32 > len(b) {
		return make([]byte, 32)
	}
	return b[start : start+32]
}

func callUint(
	ctx context.Context, rpc *chain.RPC, to, sig string, blockNumber uint64,
) (*big.Int, error) {
	out, err := rpc.CallContract(ctx, to, selector(sig), blockNumber)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(out), nil
}

func waitMined(ctx context.Context, reader *chain.Reader, txHash string) error {
	for i := 0; i < 60; i++ {
		status, err := reader.ReceiptStatus(ctx, txHash)
		if err != nil {
			return err
		}
		if status == "Confirmed" {
			return nil
		}
		if status == "Failed" {
			return fmt.Errorf("tx %s reverted", txHash)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("tx %s not mined", txHash)
}

func keyFromEnv(name string) (*secp256k1.PrivateKey, error) {
	raw := strings.TrimPrefix(os.Getenv(name), "0x")
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("%s must be 32 hex bytes", name)
	}
	return secp256k1.PrivKeyFromBytes(b), nil
}
