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
	flag.Parse()

	if err := run(*rpcURL, *oracle, *manager, *account, *settlement,
		*chainID, *deploymentID, *principal); err != nil {
		log.Fatalf("scenario: %v", err)
	}
}

func run(
	rpcURL, oracle, manager, account, settlement string,
	chainID, deploymentID, principal uint64,
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

	// Read the accepted sequence so a rerun does not collide with an
	// earlier quote.
	chainKey := attest.ChainKeyHex(binding)
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("latest block: %w", err)
	}
	reader := chain.NewReader(rpc, chain.Addresses{Oracle: oracle})
	accepted, err := reader.AcceptedSequence(ctx, chainKey, evidence.BlockRef{
		Number: block.Number, Hash: block.Hash, Timestamp: block.Timestamp,
	})
	if err != nil {
		return fmt.Errorf("accepted sequence: %w", err)
	}
	assessment.Sequence = accepted + 1

	domain := attest.DomainSeparator(new(big.Int).SetUint64(chainID), oracleAddr)
	signature, err := attest.Sign(attest.Digest(domain, assessment), attestKey)
	if err != nil {
		return fmt.Errorf("sign assessment: %w", err)
	}

	log.Printf("accepting quote: sequence %d principal %d", assessment.Sequence, principal)
	acceptTx, err := w.Send(ctx, oracle, attest.EncodeAccept(assessment, signature))
	if err != nil {
		return fmt.Errorf("accept quote: %w", err)
	}
	if err := waitMined(ctx, reader, acceptTx); err != nil {
		return fmt.Errorf("accept quote: %w", err)
	}
	log.Printf("quote accepted: %s", acceptTx)

	log.Printf("originating against the standing quote")
	originateTx, err := w.Send(ctx, manager, encodeOriginate(binding, principal))
	if err != nil {
		return fmt.Errorf("originate: %w", err)
	}
	if err := waitMined(ctx, reader, originateTx); err != nil {
		return fmt.Errorf("originate: %w", err)
	}
	log.Printf("loan originated: %s", originateTx)

	fmt.Printf("chain_key=%s\n", chainKey)
	return nil
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
