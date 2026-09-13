// Command credence-scenario drives an origination end to end.
//
// This is the one path that cannot be scripted in Solidity: `originate`
// requires a quote the oracle has accepted, and that quote must carry an
// EIP-712 signature over an assessment the engine produced. Minting a loan
// from a deploy script would bypass exactly the authorization the demo
// exists to demonstrate.
//
// The sequence mirrors `docs/05`:
//
//	assess -> accept the quote on chain -> originate against it
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/arcbjorn/credence/gateway/wallet"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func main() {
	rpcURL := flag.String("rpc", "http://127.0.0.1:8546", "chain rpc")
	oracle := flag.String("oracle", "", "risk oracle address")
	manager := flag.String("manager", "", "loan manager address")
	account := flag.String("account", "", "credit account address")
	settlement := flag.String("settlement", "", "settlement token address")
	chainID := flag.Uint64("chain-id", 31337, "chain id")
	deploymentID := flag.Uint64("deployment-id", 7, "deployment id")
	principal := flag.Uint64("principal", 2_000_000_000, "principal in base units")
	adapter := flag.String("adapter", "", "strategy adapter; deploys principal when set")
	deployBps := flag.Uint64("deploy-bps", 0,
		"basis points of principal to deploy into the risky asset")
	loans := flag.Int("loans", 1, "number of sequential loans to originate and repay")
	hold := flag.Uint64("hold-seconds", 5*86_400,
		"seconds to hold each loan before repayment, so interest accrues")
	flag.Parse()

	if err := run(*rpcURL, *oracle, *manager, *account, *settlement,
		*chainID, *deploymentID, *principal, *loans, *hold,
		*adapter, *deployBps); err != nil {
		log.Fatalf("scenario: %v", err)
	}
}

func run(
	rpcURL, oracle, manager, account, settlement string,
	chainID, deploymentID, principal uint64,
	loans int,
	holdSeconds uint64,
	adapter string,
	deployBps uint64,
) error {
	ctx := context.Background()
	rpc := chain.NewRPC(rpcURL, 15*time.Second)

	attestKey, err := keyFromEnv("CREDENCE_SIGNER_KEY")
	if err != nil {
		return err
	}
	operatorKey, err := keyFromEnv("CREDENCE_OPERATOR_KEY")
	if err != nil {
		return err
	}

	w := wallet.New(operatorKey, rpc, wallet.Config{
		ChainID:     new(big.Int).SetUint64(chainID),
		GasLimitCap: 8_000_000,
		GasPriceCap: big.NewInt(500_000_000_000),
	})

	oracleAddr, err := toAddress(oracle)
	if err != nil {
		return fmt.Errorf("oracle address: %w", err)
	}
	accountAddr, err := toAddress(account)
	if err != nil {
		return fmt.Errorf("account address: %w", err)
	}
	settlementAddr, err := toAddress(settlement)
	if err != nil {
		return fmt.Errorf("settlement address: %w", err)
	}

	binding := attest.Binding{
		IdentityRegistryChainID: new(big.Int).SetUint64(chainID),
		IdentityRegistry:        mustAddress("0x000000000000000000000000000000000000aec0"),
		AgentID:                 big.NewInt(184),
		Operator:                mustAddress(w.Address()),
		CreditAccount:           accountAddr,
		FacilityID:              big.NewInt(1),
		// The account has not executed yet, so its version is zero. The
		// contract rechecks this at draw time, so a stale value fails
		// rather than authorizing against a portfolio that moved.
		AccountStateVersion: 0,
		ConfigVersion:       1,
		ConfigHash:          attest.KeccakText("config"),
		PolicyEpoch:         1,
		PolicyHash:          attest.KeccakText("policy"),
	}

	now := uint64(time.Now().Unix())
	assessment := attest.Assessment{
		DeploymentID:     new(big.Int).SetUint64(deploymentID),
		EvidencePolicyID: attest.KeccakText("evidence-v1"),
		Route:            attest.RouteDevelopment,
		Binding:          binding,
		Kind:             attest.KindOrigination,
		AssessmentID:     attest.KeccakText(fmt.Sprintf("origination-%d", now)),
		AssessmentHash:   attest.KeccakText("output"),
		Decision:         attest.DecisionApproved,
		Permission:       attest.PermissionAllowed,
		ModelHash:        attest.KeccakText("model-v1"),
		InputHash:        attest.KeccakText("input"),
		EvidenceRoot:     attest.KeccakText("evidence"),
		Class:            attest.ClassPrototypeEligible,
		Sequence:         1,
		ValidAfter:       now - 60,
		ValidUntil:       now + 3600,
		RequestID:        attest.KeccakText("request-1"),
		IntentHash:       attest.KeccakText("intent-1"),
		SettlementAsset:  settlementAddr,
		// v0 grants the exact requested amount or denies (`docs/03`), so
		// requested and approved must match.
		RequestedPrincipal: new(big.Int).SetUint64(principal),
		Terms: attest.Terms{
			ApprovedPrincipal:   new(big.Int).SetUint64(principal),
			AprBps:              1_200,
			TermSeconds:         10 * 86_400,
			RequiredEquity:      big.NewInt(0),
			PermittedActionHash: attest.KeccakText("action"),
		},
		LoanID: big.NewInt(0),
	}

	chainKey := attest.ChainKeyHex(binding)
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("latest block: %w", err)
	}
	reader := chain.NewReader(rpc, chain.Addresses{Oracle: oracle})
	domain := attest.DomainSeparator(new(big.Int).SetUint64(chainID), oracleAddr)

	// Each loan is originated against its own accepted quote and repaid
	// before the next. `docs/09` beat 4b: three sequential loans under one
	// policy, so the binding constraint can be seen to move as history
	// accumulates — a single loan cannot show that.
	for i := 1; i <= loans; i++ {
		accepted, err := reader.AcceptedSequence(ctx, chainKey, evidence.BlockRef{
			Number: block.Number, Hash: block.Hash, Timestamp: block.Timestamp,
		})
		if err != nil {
			return fmt.Errorf("loan %d: accepted sequence: %w", i, err)
		}

		// Validity must be measured against CHAIN time, not the local
		// clock. A scenario that advances the chain (or any chain whose
		// time drifts from the host's) would otherwise present a quote the
		// oracle sees as already expired.
		now := block.Timestamp
		assessment.Sequence = accepted + 1
		assessment.ValidAfter = now - 60
		assessment.ValidUntil = now + 3600
		assessment.AssessmentID = attest.KeccakText(
			fmt.Sprintf("origination-%d-%d", now, i))
		assessment.RequestID = attest.KeccakText(fmt.Sprintf("request-%d", i))

		// The account's state version advances as it executes, and the
		// contract rechecks it at draw time, so it must be read rather than
		// assumed.
		stateVersion, err := readStateVersion(ctx, rpc, account)
		if err != nil {
			return fmt.Errorf("loan %d: state version: %w", i, err)
		}
		assessment.Binding.AccountStateVersion = stateVersion
		bindingForDraw := assessment.Binding

		signature, err := attest.Sign(attest.Digest(domain, assessment), attestKey)
		if err != nil {
			return fmt.Errorf("loan %d: sign: %w", i, err)
		}

		log.Printf("loan %d: accepting quote at sequence %d", i, assessment.Sequence)
		acceptTx, err := w.Send(ctx, oracle, attest.EncodeAccept(assessment, signature))
		if err != nil {
			return fmt.Errorf("loan %d: accept quote: %w", i, err)
		}
		if err := waitMined(ctx, reader, acceptTx); err != nil {
			return fmt.Errorf("loan %d: accept quote: %w", i, err)
		}

		originateTx, err := w.Send(ctx, manager, encodeOriginate(bindingForDraw, principal))
		if err != nil {
			return fmt.Errorf("loan %d: originate: %w", i, err)
		}
		if err := waitMined(ctx, reader, originateTx); err != nil {
			return fmt.Errorf("loan %d: originate: %w", i, err)
		}

		loanID, err := readActiveLoan(ctx, rpc, manager, account)
		if err != nil {
			return fmt.Errorf("loan %d: read active loan: %w", i, err)
		}
		log.Printf("loan %d: originated as loan %d", i, loanID)

		// Deploy the principal. An account that only holds settlement can
		// always repay from what it borrowed, so nothing about the credit
		// decision is being tested until the principal is actually at risk.
		if adapter != "" && deployBps > 0 {
			// Measured against the account's whole settlement balance,
			// not the principal alone. Agent equity is the first loss
			// layer, so leaving it in settlement while only the borrowed
			// funds trade would let equity absorb any crash from the
			// sidelines -- lenders could never be shown taking a loss,
			// however far the asset fell.
			at, err := reader.LatestBlock(ctx)
			if err != nil {
				return fmt.Errorf("loan %d: pin block: %w", i, err)
			}
			balance, err := reader.TokenBalance(ctx, settlement, account, at)
			if err != nil {
				return fmt.Errorf("loan %d: read account balance: %w", i, err)
			}
			amount := new(big.Int).Div(
				new(big.Int).Mul(balance, new(big.Int).SetUint64(deployBps)),
				big.NewInt(10_000)).Uint64()
			if err := deployPrincipal(ctx, w, reader, account, settlement,
				adapter, amount); err != nil {
				return fmt.Errorf("loan %d: deploy principal: %w", i, err)
			}
			log.Printf("loan %d: deployed %d into the risky asset", i, amount)
		}

		// Let time pass so interest actually accrues. Without this the
		// demo shows a repayment of exactly the principal, which would
		// misrepresent the economics the whole system exists to price.
		if holdSeconds > 0 {
			if err := advanceTime(ctx, rpc, holdSeconds); err != nil {
				log.Printf("loan %d: could not advance time (%v); interest will be zero", i, err)
			}
		}

		// Repay unless this is the last loan, so the demo can end with an
		// open position for the monitoring beats.
		if i < loans {
			repayTx, err := w.Send(ctx, manager, encodeRepay(loanID))
			if err != nil {
				return fmt.Errorf("loan %d: repay: %w", i, err)
			}
			if err := waitMined(ctx, reader, repayTx); err != nil {
				return fmt.Errorf("loan %d: repay: %w", i, err)
			}
			log.Printf("loan %d: repaid", i)
		}

		// Refresh after any time advance so the next quote is written
		// against the chain's current view.
		block, err = rpc.LatestBlock(ctx)
		if err != nil {
			return fmt.Errorf("loan %d: refresh block: %w", i, err)
		}
	}

	fmt.Printf("chain_key=%s\n", chainKey)
	return nil
}

// deployPrincipal converts settlement into the risky asset through the
// registered adapter.
//
// Two calls, both through the account: a bounded approval, then the trade.
// The adapter pulls what it was approved for and returns the output to the
// account, so nothing is left sitting with the adapter.
func deployPrincipal(
	ctx context.Context,
	w *wallet.Wallet,
	reader *chain.Reader,
	account, settlement, adapter string,
	amount uint64,
) error {
	settlementAddr, err := toAddress(settlement)
	if err != nil {
		return err
	}
	adapterAddr, err := toAddress(adapter)
	if err != nil {
		return err
	}
	value := new(big.Int).SetUint64(amount)

	approve := append(attest.Selector("approveAdapter(address,address,uint256)"),
		attest.AddressWord(settlementAddr)...)
	approve = append(approve, attest.AddressWord(adapterAddr)...)
	approve = append(approve, attest.Word(value)...)
	if err := sendAndWait(ctx, w, reader, account, approve, "approveAdapter"); err != nil {
		return err
	}

	acquire := append(attest.Selector("acquire(uint256)"), attest.Word(value)...)
	execute := append(attest.Selector("execute(address,bytes)"),
		attest.AddressWord(adapterAddr)...)
	// Dynamic bytes: offset, length, then the padded payload.
	execute = append(execute, attest.Word(big.NewInt(64))...)
	execute = append(execute, attest.Word(big.NewInt(int64(len(acquire))))...)
	execute = append(execute, acquire...)
	if pad := len(acquire) % 32; pad != 0 {
		execute = append(execute, make([]byte, 32-pad)...)
	}
	return sendAndWait(ctx, w, reader, account, execute, "execute acquire")
}

func sendAndWait(
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
	if err := waitMined(ctx, reader, txHash); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

// readStateVersion reads the account's execution counter.
func readStateVersion(ctx context.Context, rpc *chain.RPC, account string) (uint64, error) {
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return 0, err
	}
	out, err := rpc.CallContract(ctx, account,
		attest.Selector("stateVersion()"), block.Number)
	if err != nil {
		return 0, err
	}
	return new(big.Int).SetBytes(out).Uint64(), nil
}

func readActiveLoan(
	ctx context.Context,
	rpc *chain.RPC,
	manager, account string,
) (uint64, error) {
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return 0, err
	}
	accountAddr, err := toAddress(account)
	if err != nil {
		return 0, err
	}
	data := append(attest.Selector("activeLoan(address)"),
		attest.AddressWord(accountAddr)...)
	out, err := rpc.CallContract(ctx, manager, data, block.Number)
	if err != nil {
		return 0, err
	}
	return new(big.Int).SetBytes(out).Uint64(), nil
}

// advanceTime moves a development chain forward.
//
// Only works against a node that exposes evm_increaseTime, which is the
// point: a scenario that silently skipped this on a real chain would
// misreport interest, so the caller is told when it fails.
func advanceTime(ctx context.Context, rpc *chain.RPC, seconds uint64) error {
	if err := rpc.Call(ctx, "evm_increaseTime", []any{seconds}, nil); err != nil {
		return err
	}
	return rpc.Call(ctx, "evm_mine", []any{}, nil)
}

func encodeRepay(loanID uint64) []byte {
	out := append([]byte{}, attest.Selector("repay(uint256)")...)
	return append(out, attest.Word(new(big.Int).SetUint64(loanID))...)
}

// encodeOriginate builds calldata for originate(Binding, uint256).
//
// Binding is entirely static, so it inlines with no offset.
func encodeOriginate(b attest.Binding, principal uint64) []byte {
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

// waitMined polls until the receipt exists, so a later step cannot run
// against state the previous one has not yet produced.
func waitMined(ctx context.Context, reader *chain.Reader, txHash string) error {
	for i := 0; i < 50; i++ {
		status, err := reader.ReceiptStatus(ctx, txHash)
		if err != nil {
			return err
		}
		switch status {
		case publisher.StatusConfirmed:
			return nil
		case publisher.StatusFailed:
			return fmt.Errorf("transaction %s reverted", txHash)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("transaction %s not mined in time", txHash)
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
	if len(s) < 2 {
		return a, fmt.Errorf("empty address")
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil || len(b) != 20 {
		return a, fmt.Errorf("expected 20 hex bytes")
	}
	copy(a[:], b)
	return a, nil
}

func mustAddress(s string) attest.Address {
	a, err := toAddress(s)
	if err != nil {
		panic(err)
	}
	return a
}
