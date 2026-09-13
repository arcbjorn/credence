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
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/arcbjorn/credence/gateway/riskclient"
	"github.com/arcbjorn/credence/gateway/store"
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
	risky := flag.String("risky", "", "risky asset; required with -adapter")
	enroll := flag.String("enroll", "",
		"gateway database to enrol the open loan into, so it is monitored")
	priceSource := flag.String("price-source", "",
		"price source to refresh after advancing chain time")
	vault := flag.String("vault", "",
		"credit vault address; needed to read the facility limit")
	riskURL := flag.String("risk", "",
		"risk service url; terms come from the engine when set")
	policyPath := flag.String("policy", "",
		"engine policy file; required with -risk")
	deployBps := flag.Uint64("deploy-bps", 0,
		"basis points of principal to deploy into the risky asset")
	loans := flag.Int("loans", 1, "number of sequential loans to originate and repay")
	hold := flag.Uint64("hold-seconds", 5*86_400,
		"seconds to hold each loan before repayment, so interest accrues")
	flag.Parse()

	if err := run(*rpcURL, *oracle, *manager, *account, *settlement,
		*chainID, *deploymentID, *principal, *loans, *hold,
		*adapter, *risky, *priceSource, *enroll, *vault, *riskURL, *policyPath,
		*deployBps); err != nil {
		log.Fatalf("scenario: %v", err)
	}
}

func run(
	rpcURL, oracle, manager, account, settlement string,
	chainID, deploymentID, principal uint64,
	loans int,
	holdSeconds uint64,
	adapter, riskyAsset, priceSource, enrollDB, vault, riskURL, policyPath string,
	deployBps uint64,
) error {
	ctx := context.Background()
	rpc := chain.NewRPC(rpcURL, 15*time.Second)

	// Without the risky asset address a deployed position could never be
	// closed, and the scenario would strand itself at repayment.
	if adapter != "" && deployBps > 0 && riskyAsset == "" {
		return fmt.Errorf("-risky is required when deploying through -adapter")
	}

	// The engine, when one is configured. Without it the scenario signs
	// fixed terms, which is honest for a smoke test but cannot demonstrate
	// credit compounding (`docs/11`).
	var risk *riskclient.Client
	var enginePolicy json.RawMessage
	if riskURL != "" {
		if policyPath == "" {
			return fmt.Errorf("-policy is required with -risk")
		}
		raw, err := os.ReadFile(policyPath)
		if err != nil {
			return fmt.Errorf("read policy: %w", err)
		}
		enginePolicy = raw
		risk = riskclient.New(riskURL, 10*time.Second)
	}
	repaidSoFar := 0

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
	// The full address set: AccountAt reads settlement, debt, and
	// positions, so a reader with only the oracle cannot describe an
	// account to the engine.
	reader := chain.NewReader(rpc, chain.Addresses{
		Oracle:      oracle,
		LoanManager: manager,
		Settlement:  settlement,
		Vault:       vault,
		Risky:       riskyAsset,
	}).WithRiskyAsset(chain.RiskyAssetPolicy{
		Price: 2_000_000_000, PriceScale: 6, Decimals: 18, HaircutBps: 2_000,
	}).WithRiskySource(priceSource)
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

		// Ask the engine what this request is worth, rather than signing
		// fixed terms. `docs/11` requires three sequential loans to show a
		// rising limit and a falling APR under one policy and model hash,
		// and identical terms every time would make that unprovable.
		if risk != nil {
			bindingJSON, err := bindingSnapshotJSON(assessment.Binding, accepted)
			if err != nil {
				return fmt.Errorf("loan %d: binding snapshot: %w", i, err)
			}
			terms, raw, err := originationTerms(
				ctx, risk, reader, rpc, account, settlement, bindingJSON,
				assessment.Sequence, principal, 10*86_400,
				historyEvidence(block.Timestamp, repaidSoFar), enginePolicy,
			)
			if err != nil {
				return fmt.Errorf("loan %d: assess: %w", i, err)
			}
			if terms == nil {
				log.Printf("loan %d: engine declined; nothing to originate", i)
				_ = raw
				break
			}

			assessment.RequestedPrincipal = terms.ApprovedPrincipal
			assessment.Terms.ApprovedPrincipal = terms.ApprovedPrincipal
			assessment.Terms.AprBps = terms.AprBps
			assessment.Terms.TermSeconds = terms.TermSeconds
			assessment.Terms.RequiredEquity = terms.RequiredEquity
			principal = terms.ApprovedPrincipal.Uint64()

			log.Printf("loan %d: engine terms apr=%dbps capacity=%s binding=%s",
				i, terms.AprBps, terms.AgentCapacity, terms.AgentBinding)
		}

		// Commit to the exact action this loan will take. The account
		// compares every execution against this hash, so it must be the
		// real call -- adapter and calldata both -- not a placeholder.
		//
		// The deploy amount is knowable before origination: it is the
		// balance the account will hold once the principal lands.
		deployAmount := uint64(0)
		if adapter != "" && deployBps > 0 {
			at, err := reader.LatestBlock(ctx)
			if err != nil {
				return fmt.Errorf("loan %d: pin block: %w", i, err)
			}
			held, err := reader.TokenBalance(ctx, settlement, account, at)
			if err != nil {
				return fmt.Errorf("loan %d: read account balance: %w", i, err)
			}
			after := new(big.Int).Add(held, new(big.Int).SetUint64(principal))
			deployAmount = new(big.Int).Div(
				new(big.Int).Mul(after, new(big.Int).SetUint64(deployBps)),
				big.NewInt(10_000)).Uint64()

			adapterAddr, err := toAddress(adapter)
			if err != nil {
				return err
			}
			acquire := append(attest.Selector("acquire(uint256)"),
				attest.Word(new(big.Int).SetUint64(deployAmount))...)
			assessment.Terms.PermittedActionHash = actionHash(adapterAddr, acquire)
		}

		signature, err := attest.Sign(attest.Digest(domain, assessment), attestKey)
		if err != nil {
			return fmt.Errorf("loan %d: sign: %w", i, err)
		}

		// Commit the forecast BEFORE the quote is accepted, let alone
		// funded. `docs/06`: a forecast recorded after the outcome is
		// known is not a forecast, so commitment order is what makes the
		// Brier score mean anything.
		if enrollDB != "" {
			if err := commitForecast(
				ctx, enrollDB, binding, chainKey, i, block.Timestamp,
			); err != nil {
				return fmt.Errorf("loan %d: commit forecast: %w", i, err)
			}
			log.Printf("loan %d: forecast committed before funding", i)
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
		deployed := false
		if adapter != "" && deployBps > 0 {
			// The amount committed to above. Measured against the
			// account's whole settlement balance, not the principal
			// alone: agent equity is the first loss layer, so leaving it
			// in settlement while only the borrowed funds trade would let
			// equity absorb any crash from the sidelines.
			amount := deployAmount
			if err := deployPrincipal(ctx, w, reader, account, settlement,
				adapter, amount); err != nil {
				return fmt.Errorf("loan %d: deploy principal: %w", i, err)
			}
			log.Printf("loan %d: deployed %d into the risky asset", i, amount)
			deployed = true
		}

		// Let time pass so interest actually accrues. Without this the
		// demo shows a repayment of exactly the principal, which would
		// misrepresent the economics the whole system exists to price.
		if holdSeconds > 0 {
			if err := advanceTime(ctx, rpc, holdSeconds); err != nil {
				log.Printf("loan %d: could not advance time (%v); interest will be zero", i, err)
			}
			// A real feed publishes continuously, so skipping days of
			// chain time must not leave the price looking stale. Without
			// this the adapter refuses to trade -- correctly, since it
			// cannot tell a demo's time jump from a dead oracle.
			if priceSource != "" {
				if err := sendAndWait(ctx, w, reader, priceSource,
					attest.Selector("touch()"), "touch price"); err != nil {
					return fmt.Errorf("loan %d: refresh price: %w", i, err)
				}
			}
		}

		// Repay unless this is the last loan, so the demo can end with an
		// open position for the monitoring beats.
		if i < loans {
			// A deployed account holds the risky asset, not settlement,
			// so the position must be closed before the debt can be
			// met. This is the ordinary path (`docs/09` beat 4): exit
			// the strategy, then repay from the proceeds.
			if deployed {
				if err := unwindAll(ctx, w, reader, rpc,
					account, riskyAsset, adapter); err != nil {
					return fmt.Errorf("loan %d: unwind: %w", i, err)
				}
				log.Printf("loan %d: closed strategy position", i)
			}
			repayTx, err := w.Send(ctx, manager, encodeRepay(loanID))
			if err != nil {
				return fmt.Errorf("loan %d: repay: %w", i, err)
			}
			if err := waitMined(ctx, reader, repayTx); err != nil {
				return fmt.Errorf("loan %d: repay: %w", i, err)
			}
			log.Printf("loan %d: repaid", i)
			repaidSoFar++

			// The horizon has passed, so the forecast resolves. A loan
			// that repaid without the strategy being closed at a loss did
			// not see the drawdown it forecast.
			if enrollDB != "" {
				after, err := rpc.LatestBlock(ctx)
				if err != nil {
					return fmt.Errorf("loan %d: pin block: %w", i, err)
				}
				if err := resolveForecast(
					ctx, enrollDB, chainKey, i, false, after.Timestamp,
				); err != nil {
					return fmt.Errorf("loan %d: resolve forecast: %w", i, err)
				}
				log.Printf("loan %d: forecast resolved", i)
			}
		} else if enrollDB != "" {
			// Only the loan left open is enrolled. Continuous
			// underwriting governs live debt, so enrolling a repaid loan
			// would have the gateway reassess an obligation that no
			// longer exists.
			// Read the account's CURRENT state version, not the one the
			// binding was originated with: deploying the principal calls
			// execute, which bumps it. Enrolling the stale value makes
			// every monitoring cycle fail as a version mismatch.
			at, err := rpc.LatestBlock(ctx)
			if err != nil {
				return fmt.Errorf("loan %d: pin block: %w", i, err)
			}
			out, err := rpc.CallContract(ctx, account,
				attest.Selector("stateVersion()"), at.Number)
			if err != nil {
				return fmt.Errorf("loan %d: read state version: %w", i, err)
			}
			stateVersion := new(big.Int).SetBytes(out).Uint64()
			if err := enrollLoan(ctx, enrollDB, binding, chainKey,
				loanID, accountAddr, w.Address(), stateVersion); err != nil {
				return fmt.Errorf("loan %d: enrol: %w", i, err)
			}
			log.Printf("loan %d: enrolled for monitoring", i)
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

// addressHex and hashHex render fixed-size values for the registry, which
// stores them as lowercase hex strings.
func addressHex(a attest.Address) string { return "0x" + hex.EncodeToString(a[:]) }
func hashHex(h attest.Hash) string       { return "0x" + hex.EncodeToString(h[:]) }

// enrollLoan registers the agent, binding, and open loan in the gateway's
// store so the monitoring loop picks the loan up at startup.
//
// The gateway has no chain discovery: it reassesses what its registry says
// is live. Without this the loop runs correctly and tracks nothing, which
// looks healthy in the logs while no loan is actually being underwritten.
func enrollLoan(
	ctx context.Context,
	dbPath string,
	binding attest.Binding,
	chainKey string,
	loanID uint64,
	account attest.Address,
	operator string,
	stateVersion uint64,
) error {
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()
	reg := store.NewRegistry(db)

	if err := registerBindingFor(ctx, reg, binding, chainKey); err != nil {
		return err
	}

	// No observation time is known yet, so zero is recorded deliberately:
	// the scheduler reads that as maximally stale and reassesses on the
	// first tick rather than assuming the origination still holds.
	return reg.OpenLoan(ctx, chainKey, strconv.FormatUint(loanID, 10),
		stateVersion, 0)
}

// actionHash mirrors the credit account's permitted-action commitment:
// keccak256(abi.encode(adapter, data)).
//
// abi.encode of (address, bytes) is a head of two words -- the address and
// the offset to the bytes -- followed by the length and the right-padded
// payload. Computed here rather than approximated, because a mismatch
// makes every execution revert as an unpermitted action.
func actionHash(adapter attest.Address, data []byte) attest.Hash {
	out := attest.AddressWord(adapter)
	out = append(out, attest.Word(big.NewInt(64))...)
	out = append(out, attest.Word(big.NewInt(int64(len(data))))...)
	out = append(out, data...)
	if pad := len(data) % 32; pad != 0 {
		out = append(out, make([]byte, 32-pad)...)
	}
	return attest.KeccakBytes(out)
}

// registerBindingFor records the agent and binding a run operates under.
//
// Shared because a forecast is committed before the loan is enrolled, and
// both need the binding row to exist. Idempotent, so whichever runs first
// establishes it.
func registerBindingFor(
	ctx context.Context,
	reg *store.Registry,
	binding attest.Binding,
	chainKey string,
) error {
	agentKey := fmt.Sprintf("%s:%s:%s",
		binding.IdentityRegistryChainID,
		addressHex(binding.IdentityRegistry),
		binding.AgentID)

	if err := reg.RegisterAgent(ctx, store.Agent{
		AgentKey:                agentKey,
		IdentityRegistryChainID: binding.IdentityRegistryChainID.Uint64(),
		IdentityRegistry:        addressHex(binding.IdentityRegistry),
		AgentID:                 binding.AgentID.String(),
	}); err != nil {
		return fmt.Errorf("register agent: %w", err)
	}

	// The binding key is the on-chain one, not a local string: the oracle
	// rejects an assessment bound to anything else.
	return reg.RegisterBinding(ctx, store.Binding{
		BindingKey:    chainKey,
		AgentKey:      agentKey,
		Operator:      addressHex(binding.Operator),
		CreditAccount: addressHex(binding.CreditAccount),
		FacilityID:    binding.FacilityID.String(),
		ConfigVersion: binding.ConfigVersion,
		ConfigHash:    hashHex(binding.ConfigHash),
		PolicyEpoch:   binding.PolicyEpoch,
		PolicyHash:    hashHex(binding.PolicyHash),
		// The scenario creates the account in this same run, so indexing
		// genuinely covers its whole history. A real deployment must
		// record where its indexer actually started, and leave this zero
		// when it cannot prove coverage.
		IndexedFrom: 1,
	})
}

// unwindAll closes the whole risky position back into settlement.
//
// The mirror of deployPrincipal: approve the adapter for the risky
// balance, then trade through the account. The adapter's slippage guard
// still applies, so an exit at a venue price that disagrees with the
// independent source reverts rather than dumping the position.
func unwindAll(
	ctx context.Context,
	w *wallet.Wallet,
	reader *chain.Reader,
	rpc *chain.RPC,
	account, risky, adapter string,
) error {
	at, err := reader.LatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("pin block: %w", err)
	}
	balance, err := reader.TokenBalance(ctx, risky, account, at)
	if err != nil {
		return fmt.Errorf("read risky balance: %w", err)
	}
	if balance.Sign() == 0 {
		return nil
	}

	riskyAddr, err := toAddress(risky)
	if err != nil {
		return err
	}
	adapterAddr, err := toAddress(adapter)
	if err != nil {
		return err
	}

	approve := append(attest.Selector("approveAdapter(address,address,uint256)"),
		attest.AddressWord(riskyAddr)...)
	approve = append(approve, attest.AddressWord(adapterAddr)...)
	approve = append(approve, attest.Word(balance)...)
	if err := sendAndWait(ctx, w, reader, account, approve, "approveAdapter risky"); err != nil {
		return err
	}

	unwind := append(attest.Selector("unwind(uint256)"), attest.Word(balance)...)
	execute := append(attest.Selector("execute(address,bytes)"),
		attest.AddressWord(adapterAddr)...)
	execute = append(execute, attest.Word(big.NewInt(64))...)
	execute = append(execute, attest.Word(big.NewInt(int64(len(unwind))))...)
	execute = append(execute, unwind...)
	if pad := len(unwind) % 32; pad != 0 {
		execute = append(execute, make([]byte, 32-pad)...)
	}
	return sendAndWait(ctx, w, reader, account, execute, "execute unwind")
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
