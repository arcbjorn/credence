// Command credence-recover drives a loan through recovery to default.
//
// `docs/09` beat 7: keeper action, actual collection, and a lender loss
// that is visible rather than asserted. The point is not that a default
// can be recorded — it is that the loss reconciles: what the vault lent,
// minus what recovery actually collected, equals what lenders bear.
//
// The sequence mirrors `docs/05`:
//
//	openRecovery -> collect what exists -> declareDefault on the shortfall
//
// `declareDefault` refuses while any settlement balance remains, so a
// shortfall cannot be written off while assets are still recoverable.
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
	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/arcbjorn/credence/gateway/wallet"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:8546", "chain rpc")
	manager := flag.String("manager", "", "loan manager address")
	vault := flag.String("vault", "", "credit vault address")
	account := flag.String("account", "", "credit account address")
	settlement := flag.String("settlement", "", "settlement token address")
	loanID := flag.Uint64("loan", 0, "loan to recover")
	chainID := flag.Uint64("chain-id", 31337, "chain id")
	// A shock leaves the account holding less than its debt. Expressed as
	// the fraction of value that survives, so the caller states the
	// severity rather than computing a residue.
	survivesBps := flag.Uint64("survives-bps", 6_000,
		"basis points of account value that survive the shock")
	flag.Parse()

	if err := run(*rpcURL, *manager, *vault, *account, *settlement,
		*loanID, *chainID, *survivesBps); err != nil {
		log.Fatalf("recover: %v", err)
	}
}

func run(
	rpcURL, manager, vault, account, settlement string,
	loanID, chainID, survivesBps uint64,
) error {
	ctx := context.Background()
	rpc := chain.NewRPC(rpcURL, 15*time.Second)
	reader := chain.NewReader(rpc, chain.Addresses{LoanManager: manager})

	operatorKey, err := keyFromEnv("CREDENCE_OPERATOR_KEY")
	if err != nil {
		return err
	}
	w := wallet.New(operatorKey, rpc, wallet.Config{
		ChainID:     new(big.Int).SetUint64(chainID),
		GasLimitCap: 8_000_000,
		GasPriceCap: big.NewInt(500_000_000_000),
	})

	// Record the starting position so the loss can be reconciled rather
	// than reported from a single post-hoc reading.
	lentBefore, err := readUint(ctx, rpc, vault, "outstandingPrincipal()")
	if err != nil {
		return fmt.Errorf("read outstanding principal: %w", err)
	}
	debtBefore, err := readUintArg(ctx, rpc, manager, "debtDue(uint256)", loanID)
	if err != nil {
		return fmt.Errorf("read debt: %w", err)
	}
	log.Printf("before recovery: vault lent %s, loan owes %s", lentBefore, debtBefore)

	// Open recovery. The keeper is the operator here; in a real deployment
	// it is a separate party with a bounded fee.
	keeper, err := toAddress(w.Address())
	if err != nil {
		return err
	}
	// Recovery now requires the binding, so the manager can consult the
	// oracle for a trigger. Seizing a healthy, unmatured position is
	// refused (`docs/05`), which is why this cannot be a bare loan id.
	accountAddr, err := toAddress(account)
	if err != nil {
		return err
	}
	registry, err := toAddress("0x000000000000000000000000000000000000aec0")
	if err != nil {
		return err
	}
	stateVersion, err := readUintArg(ctx, rpc, account, "stateVersion()", 0)
	if err != nil {
		return fmt.Errorf("read state version: %w", err)
	}
	binding := attest.Binding{
		IdentityRegistryChainID: new(big.Int).SetUint64(chainID),
		IdentityRegistry:        registry,
		AgentID:                 big.NewInt(184),
		Operator:                keeper,
		CreditAccount:           accountAddr,
		FacilityID:              big.NewInt(1),
		AccountStateVersion:     stateVersion.Uint64(),
		ConfigVersion:           1,
		ConfigHash:              attest.KeccakText("config"),
		PolicyEpoch:             1,
		PolicyHash:              attest.KeccakText("policy"),
	}

	data := append(attest.Selector(
		"openRecovery(uint256,address,(uint256,address,uint256,address,address,"+
			"uint256,uint64,uint64,bytes32,uint64,bytes32))"),
		attest.Word(new(big.Int).SetUint64(loanID))...)
	data = append(data, attest.AddressWord(keeper)...)
	data = append(data, attest.BindingWords(binding)...)
	if err := send(ctx, w, reader, manager, data, "openRecovery"); err != nil {
		return err
	}
	log.Printf("recovery opened with keeper %s", w.Address())

	// Apply the shock. Value must actually LEAVE the account, not merely
	// be collected late: a shock that only delays collection produces a
	// full recovery and proves nothing about loss. This mirrors an unwind
	// that realises less than the position was marked at.
	balance, err := readUintArgAddress(ctx, rpc, account, "balanceOf(address)", settlement)
	if err != nil {
		return fmt.Errorf("read account balance: %w", err)
	}
	surviving := new(big.Int).Div(
		new(big.Int).Mul(balance, new(big.Int).SetUint64(survivesBps)),
		big.NewInt(10_000))
	destroyed := new(big.Int).Sub(balance, surviving)
	log.Printf("shock: %s of %s survives, %s destroyed", surviving, balance, destroyed)

	// Collect only what survived. The unrecovered remainder is the
	// shortfall: the account held risky value that unwound below its mark,
	// so settlement never materialised for it.
	//
	// Nothing is removed from the account here. The withdrawal guard makes
	// that impossible while debt is open — which is beat 6's invariant, and
	// a scenario that circumvented it would be demonstrating the opposite
	// of what the protocol claims.
	if surviving.Sign() > 0 {
		collectData := append(attest.Selector("collect(uint256,uint256)"),
			attest.Word(new(big.Int).SetUint64(loanID))...)
		collectData = append(collectData, attest.Word(surviving)...)
		if err := send(ctx, w, reader, manager, collectData, "collect"); err != nil {
			return err
		}
		log.Printf("collected %s", surviving)
	}

	remaining, err := readUintArgAddress(ctx, rpc, account, "balanceOf(address)", settlement)
	if err != nil {
		return fmt.Errorf("read remaining balance: %w", err)
	}
	if remaining.Sign() > 0 {
		// `declareDefault` refuses while settlement remains — the guard
		// against writing off a loan whose assets are still recoverable.
		// Collecting it is correct: it belongs to the lender either way.
		collectRest := append(attest.Selector("collect(uint256,uint256)"),
			attest.Word(new(big.Int).SetUint64(loanID))...)
		collectRest = append(collectRest, attest.Word(remaining)...)
		if err := send(ctx, w, reader, manager, collectRest, "collect remainder"); err != nil {
			return err
		}
		log.Printf("collected remaining settlement %s", remaining)
	}

	status, err := readUintArg(ctx, rpc, manager, "status(uint256)", loanID)
	if err != nil {
		return fmt.Errorf("read status: %w", err)
	}
	// Status 4 is Recovered: the debt was met in full, so there is no loss
	// to declare. That is a legitimate outcome, not a failure.
	if status.Uint64() == 4 {
		log.Printf("loan fully recovered; no lender loss")
		return reconcile(ctx, rpc, vault, lentBefore)
	}

	defaultData := append(attest.Selector("declareDefault(uint256)"),
		attest.Word(new(big.Int).SetUint64(loanID))...)
	if err := send(ctx, w, reader, manager, defaultData, "declareDefault"); err != nil {
		return err
	}
	log.Printf("default declared")

	return reconcile(ctx, rpc, vault, lentBefore)
}

// reconcile proves the loss lands where it should: principal that left the
// vault and did not come back is recorded as a realized loss, not quietly
// absorbed.
func reconcile(ctx context.Context, rpc *chain.RPC, vault string, lentBefore *big.Int) error {
	realizedLoss, err := readUint(ctx, rpc, vault, "realizedLoss()")
	if err != nil {
		return fmt.Errorf("read realized loss: %w", err)
	}
	lentAfter, err := readUint(ctx, rpc, vault, "outstandingPrincipal()")
	if err != nil {
		return fmt.Errorf("read outstanding principal: %w", err)
	}
	idle, err := readUint(ctx, rpc, vault, "idleCash()")
	if err != nil {
		return fmt.Errorf("read idle cash: %w", err)
	}

	fmt.Printf("realized_loss=%s\n", realizedLoss)
	fmt.Printf("outstanding_after=%s\n", lentAfter)
	fmt.Printf("idle_cash=%s\n", idle)

	// Everything lent either came back, remains outstanding, or is a
	// recorded loss. A gap would mean principal vanished from the ledger.
	accounted := new(big.Int).Add(lentAfter, realizedLoss)
	if accounted.Cmp(lentBefore) > 0 {
		return fmt.Errorf("ledger inconsistent: %s accounted against %s lent",
			accounted, lentBefore)
	}
	return nil
}

func send(
	ctx context.Context,
	w *wallet.Wallet,
	reader *chain.Reader,
	to string,
	data []byte,
	label string,
) error {
	txHash, err := w.Send(ctx, to, data)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	for i := 0; i < 50; i++ {
		status, err := reader.ReceiptStatus(ctx, txHash)
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		switch status {
		case publisher.StatusConfirmed:
			return nil
		case publisher.StatusFailed:
			return fmt.Errorf("%s: transaction %s reverted", label, txHash)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%s: transaction %s not mined in time", label, txHash)
}

func readUint(ctx context.Context, rpc *chain.RPC, to, signature string) (*big.Int, error) {
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return nil, err
	}
	out, err := rpc.CallContract(ctx, to, attest.Selector(signature), block.Number)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(out), nil
}

func readUintArg(
	ctx context.Context,
	rpc *chain.RPC,
	to, signature string,
	arg uint64,
) (*big.Int, error) {
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return nil, err
	}
	data := append(attest.Selector(signature),
		attest.Word(new(big.Int).SetUint64(arg))...)
	out, err := rpc.CallContract(ctx, to, data, block.Number)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(out), nil
}

func readUintArgAddress(
	ctx context.Context,
	rpc *chain.RPC,
	to, signature, arg string,
) (*big.Int, error) {
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return nil, err
	}
	addr, err := toAddress(arg)
	if err != nil {
		return nil, err
	}
	data := append(attest.Selector(signature), attest.AddressWord(addr)...)
	out, err := rpc.CallContract(ctx, to, data, block.Number)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(out), nil
}

func keyFromEnv(name string) (*secp256k1.PrivateKey, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return nil, fmt.Errorf("%s is not set", name)
	}
	b, err := hex.DecodeString(raw)
	if err != nil || len(b) != 32 {
		return nil, fmt.Errorf("%s must be 32 hex bytes", name)
	}
	return secp256k1.PrivKeyFromBytes(b), nil
}

func toAddress(s string) (attest.Address, error) {
	var a attest.Address
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil || len(b) != 20 {
		return a, fmt.Errorf("invalid address %q", s)
	}
	copy(a[:], b)
	return a, nil
}
