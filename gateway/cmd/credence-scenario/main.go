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
	"strings"
	"time"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/canonical"
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
	termSeconds := flag.Uint64("term-seconds", 10*86_400,
		"loan term; keep it short on a chain with no time travel")
	hold := flag.Uint64("hold-seconds", 5*86_400,
		"seconds to hold each loan before repayment, so interest accrues")
	// The identity the loans are bound to. Defaults keep the local demo
	// self-contained; against a real ERC-8004 registry these must name an
	// agent that actually exists, or the published feedback attributes an
	// outcome to a token id nobody holds.
	identityRegistry := flag.String("identity-registry",
		"0x000000000000000000000000000000000000aec0",
		"ERC-8004 identity registry the binding names")
	agentID := flag.Uint64("agent-id", 184,
		"registered agent id the loans are bound to")
	flag.Parse()

	if err := run(*rpcURL, *oracle, *manager, *account, *settlement,
		*chainID, *deploymentID, *principal, *loans, *hold,
		*adapter, *risky, *priceSource, *enroll, *vault, *riskURL, *policyPath,
		*termSeconds, *deployBps, *identityRegistry, *agentID); err != nil {
		log.Fatalf("scenario: %v", err)
	}
}

func run(
	rpcURL, oracle, manager, account, settlement string,
	chainID, deploymentID, principal uint64,
	loans int,
	holdSeconds uint64,
	adapter, riskyAsset, priceSource, enrollDB, vault, riskURL, policyPath string,
	termSeconds, deployBps uint64,
	identityRegistry string,
	agentID uint64,
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
	var repayments []repayment

	// Probe once, before anything is signed. A development chain exposes
	// evm_increaseTime; a public one does not, and finding that out
	// halfway through a run leaves a funded loan that cannot be held.
	warpable := advanceTime(ctx, rpc, 0) == nil
	if !warpable {
		log.Printf("chain does not support time travel; " +
			"terms must be short enough to observe in real time")
	}

	// The class this deployment may act on. Pinned in the oracle at
	// construction, so the envelope must match it rather than guess.
	executableClass, err := readExecutableClass(ctx, rpc, oracle)
	if err != nil {
		return fmt.Errorf("read executable class: %w", err)
	}

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
		IdentityRegistry:        mustAddress(identityRegistry),
		AgentID:                 new(big.Int).SetUint64(agentID),
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
		// Read from the deployment, not assumed. A scenario deployment
		// issues ScenarioOnly assessments and executes them; signing
		// PrototypeEligible against it would store no quote and surface
		// later as NoQuote, naming the wrong cause.
		Class:           executableClass,
		Sequence:        1,
		ValidAfter:      now - 60,
		ValidUntil:      now + 3600,
		RequestID:       attest.KeccakText("request-1"),
		IntentHash:      attest.KeccakText("intent-1"),
		SettlementAsset: settlementAddr,
		// v0 grants the exact requested amount or denies (`docs/03`), so
		// requested and approved must match.
		RequestedPrincipal: new(big.Int).SetUint64(principal),
		Terms: attest.Terms{
			ApprovedPrincipal:   new(big.Int).SetUint64(principal),
			AprBps:              1_200,
			TermSeconds:         termSeconds,
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
		// Commit the forecast BEFORE anything else. `docs/06` requires
		// commitment before disbursement -- and the engine prices the
		// forecast record, so a commitment made after assessment could
		// not have informed the decision it is supposed to inform.
		if enrollDB != "" {
			// The horizon must close before the loan resolves, or the
			// forecast is scored on a window that never elapsed. On a
			// warpable chain the demo's 20 minutes cost nothing; on a
			// real one the hold is the only time that actually passes,
			// so the horizon follows it.
			horizon := uint64(1_200)
			if !warpable && holdSeconds < horizon {
				horizon = holdSeconds
			}
			if err := commitForecast(
				ctx, enrollDB, binding, chainKey, i, block.Timestamp,
				horizon,
			); err != nil {
				return fmt.Errorf("loan %d: commit forecast: %w", i, err)
			}
			log.Printf("loan %d: forecast committed before funding", i)
		}

		if risk != nil {
			bindingJSON, err := bindingSnapshotJSON(assessment.Binding, accepted)
			if err != nil {
				return fmt.Errorf("loan %d: binding snapshot: %w", i, err)
			}
			terms, raw, err := originationTerms(
				ctx, risk, reader, rpc, account, settlement, bindingJSON,
				assessment.Sequence, principal, termSeconds,
				historyEvidence(block.Timestamp, repayments),
				forecastEvidence(ctx, enrollDB, chainKey, block.Timestamp),
				enginePolicy,
				// The loan deploys into the risky asset when an adapter is
				// configured, so that is what must be assessed.
				intendedAcquire(adapter, deployBps, riskyAsset),
			)
			if err != nil {
				return fmt.Errorf("loan %d: assess: %w", i, err)
			}
			if terms == nil {
				// Report WHY. A decline with no reason is
				// indistinguishable from a broken snapshot, and the
				// engine already names its binding constraint.
				log.Printf("loan %d: engine declined: %s", i, declineReason(raw))
				break
			}

			assessment.RequestedPrincipal = terms.ApprovedPrincipal
			assessment.Terms.ApprovedPrincipal = terms.ApprovedPrincipal
			assessment.Terms.AprBps = terms.AprBps
			assessment.Terms.TermSeconds = terms.TermSeconds
			assessment.Terms.RequiredEquity = terms.RequiredEquity

			// Commit to what the engine actually decided. Signing
			// placeholder hashes means the envelope attests to constants:
			// replay would "verify" any assessment against any other, and
			// the evidence root would claim no evidence at all.
			if err := bindEngineOutput(&assessment, terms, raw); err != nil {
				return fmt.Errorf("loan %d: bind assessment: %w", i, err)
			}

			// Retain what the decision was made from and produced.
			// `docs/04` requires replay from retained evidence, and an
			// assessment that reached chain with nothing behind it cannot
			// be replayed at all.
			if enrollDB != "" {
				if err := retainDecision(
					ctx, enrollDB, chainKey, terms.InputHash,
					terms.InputJSON, raw,
				); err != nil {
					return fmt.Errorf("loan %d: retain: %w", i, err)
				}
			}
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
		// The exact action the quote commits to, so origination can
		// perform it atomically rather than in a second transaction.
		var deployAction []byte
		var deployAdapter [20]byte
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
			// Kept so origination can perform exactly the action it was
			// priced against, rather than a separately encoded one.
			deployAction = acquire
			deployAdapter = adapterAddr
		}

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

		// Fund and trade in ONE transaction when the quote approves a
		// trade.
		//
		// `docs/05`: "Where an initial trade is part of the approval, fund
		// and trade atomically; revert everything if post-action checks
		// fail." This tool drew first and deployed afterwards, which left
		// exactly the gap that rule forbids -- and since 14 September the
		// contract refuses a plain draw against an action-bound quote
		// (`ActionRequired`), so the two-step sequence no longer works at
		// all.
		//
		// The adapter allowance still has to exist before the action runs,
		// so it is granted first. It moves no funds on its own.
		var originateData []byte
		if deployAction != nil {
			if err := approveAdapter(
				ctx, w, reader, account, settlement, adapter, deployAmount,
			); err != nil {
				return fmt.Errorf("loan %d: approve adapter: %w", i, err)
			}
			originateData = encodeOriginateAndExecute(
				bindingForDraw, principal, deployAdapter, deployAction)
		} else {
			originateData = encodeOriginate(bindingForDraw, principal)
		}

		originateTx, err := w.Send(ctx, manager, originateData)
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

		// Chain time at origination, so the eligible duration below is
		// measured rather than assumed.
		originBlock, err := rpc.LatestBlock(ctx)
		if err != nil {
			return fmt.Errorf("loan %d: pin block: %w", i, err)
		}
		originatedAt := originBlock.Timestamp

		// The principal is already deployed: `originateAndExecute`
		// performed the approved trade in the same transaction as the
		// draw, so there is nothing to do here.
		//
		// It used to happen in a second transaction. An account that only
		// holds settlement can always repay from what it borrowed, so
		// nothing about the credit decision is tested until the principal
		// is actually at risk -- and the window between the two was a
		// position the assessment had not priced.
		deployed := deployAction != nil
		if deployed {
			log.Printf("loan %d: deployed %d into the risky asset (atomic)",
				i, deployAmount)
		}

		// Let time pass so interest actually accrues. Without this the
		// demo shows a repayment of exactly the principal, which would
		// misrepresent the economics the whole system exists to price.
		// Sample the account's marked value so the drawdown forecast can
		// be resolved against what happened. `docs/06` makes the sampling
		// cadence part of the event definition: a sampled maximum
		// drawdown does not claim to observe every intermediate price.
		navPeak, navTrough, navSeen := navSample(
			ctx, reader, rpc, account, settlement, riskyAsset, priceSource, manager)

		if holdSeconds > 0 {
			if !warpable {
				// Real time is the only clock this chain has. Sleeping
				// is not a workaround for the missing RPC: it is the
				// honest version of the same hold, so the interest the
				// loan repays is interest that genuinely accrued.
				//
				// The contract reads block.timestamp, not wall clock, so
				// the hold is measured against the chain and confirmed
				// by a block that actually carries the later timestamp.
				log.Printf("loan %d: holding %ds in real time", i, holdSeconds)
				if err := holdUntilChainTime(ctx, rpc, holdSeconds); err != nil {
					return fmt.Errorf("loan %d: %w", i, err)
				}
			} else if err := advanceTime(ctx, rpc, holdSeconds); err != nil {
				// A public chain exposes no evm_increaseTime, and real
				// time passes far too slowly to hold a multi-day loan.
				// Refusing is the honest answer: continuing would repay
				// after seconds and report an interest figure that
				// describes a loan nobody made.
				//
				// Use -hold-seconds with a short -term-seconds against a
				// real chain, so the accrual is genuinely observed.
				if !warpable {
					return fmt.Errorf(
						"loan %d: cannot advance time on this chain (%w); "+
							"use a short -term-seconds and a real wait instead",
						i, err)
				}
				log.Printf("loan %d: could not advance time (%v)", i, err)
			}
			// Sample again after the hold: the position has been marked
			// across the horizon, so the peak and trough now bracket it.
			// Observability, not magnitude. Testing `p > 0` dropped a
			// genuinely insolvent sample as though nothing had been seen,
			// which is the one reading a drawdown forecast most needs.
			if p, t, seen := navSample(
				ctx, reader, rpc, account, settlement, riskyAsset,
				priceSource, manager,
			); seen {
				if !navSeen {
					navPeak, navTrough, navSeen = p, t, true
				} else {
					if p > navPeak {
						navPeak = p
					}
					if t < navTrough {
						navTrough = t
					}
				}
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
			// Observed, not assumed: the actual principal and the actual
			// seconds it was outstanding, which is what `docs/06` weights.
			after, err := rpc.LatestBlock(ctx)
			if err != nil {
				return fmt.Errorf("loan %d: pin block: %w", i, err)
			}
			repayments = append(repayments, repayment{
				Principal:       principal,
				EligibleSeconds: after.Timestamp - originatedAt,
			})

			// Resolve against what the account's value ACTUALLY did, not
			// against the fact that it repaid. `docs/06` defines the event
			// as a 5% drawdown from the observed peak, and a position that
			// fell and recovered before repayment still saw the drawdown
			// it forecast. Asserting "did not occur" after every repayment
			// scores a question nobody observed.
			if enrollDB != "" {
				at, err := rpc.LatestBlock(ctx)
				if err != nil {
					return fmt.Errorf("loan %d: pin block: %w", i, err)
				}
				// Three outcomes, not two. A sample that was never
				// observed resolves as NotObservable and carries no
				// score; testing `navPeak > 0` recorded it as "did not
				// occur", which credits the agent for a question nobody
				// answered -- and did the same to a genuinely insolvent
				// account whose NAV really was zero.
				resolution := store.NotObservable
				occurred := false
				if navSeen {
					occurred = navTrough*10_000 <
						navPeak*(10_000-drawdownThresholdBps)
					resolution = store.DidNotOccur
					if occurred {
						resolution = store.Occurred
					}
				}
				if err := resolveForecast(
					ctx, enrollDB, chainKey, i, resolution, at.Timestamp,
				); err != nil {
					return fmt.Errorf("loan %d: resolve forecast: %w", i, err)
				}
				log.Printf("loan %d: forecast resolved %s "+
					"(peak=%d trough=%d observed=%v)",
					i, resolution, navPeak, navTrough, navSeen)
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

// drawdownThresholdBps is the event `docs/06` defines: a fall of more than
// 5% below the observed peak.
const drawdownThresholdBps = 500

// navSample marks the account's NAV once, returning it as both a
// candidate peak and a candidate trough.
//
// `docs/06` defines NAV as marked assets MINUS outstanding principal and
// unpaid accrued interest. Borrowing adds an asset and an equal
// liability and creates no profit, so sampling gross assets would report
// a drawdown the agent never experienced -- every draw would look like a
// gain and every repayment like a loss.
//
// The risky position is marked at the CONFIGURED PRICE SOURCE, not a
// constant. A fixed mark cannot observe the event the forecast is about:
// the price would never move, so the drawdown could never occur.
//
// The third return reports whether the NAV was OBSERVED at all. A value
// that could not be read is not a value of zero: `docs/06` scores these
// samples into a forecast, and an unobservable moment must resolve as
// unobservable rather than as a number. An insolvent account genuinely at
// zero is reported as `(0, 0, true)`, which the caller must be able to
// tell apart.
func navSample(
	ctx context.Context,
	reader *chain.Reader,
	rpc *chain.RPC,
	account, settlement, riskyAsset, priceSource, manager string,
) (uint64, uint64, bool) {
	at, err := rpc.LatestBlock(ctx)
	if err != nil {
		return 0, 0, false
	}
	ref := evidence.BlockRef{Number: at.Number, Hash: at.Hash, Timestamp: at.Timestamp}

	cash, err := reader.TokenBalance(ctx, settlement, account, ref)
	if err != nil {
		return 0, 0, false
	}
	assets := new(big.Int).Set(cash)

	if riskyAsset != "" {
		risky, err := reader.TokenBalance(ctx, riskyAsset, account, ref)
		if err != nil {
			return 0, 0, false
		}
		if risky.Sign() > 0 {
			obs, err := reader.RiskyPriceAt(ctx, priceSource, at.Number)
			if err != nil {
				// The mark is unobservable, so the NAV is too.
				return 0, 0, false
			}
			// Risky units at the source's price, expressed in settlement
			// units: amount * price / 10^decimals.
			marked := new(big.Int).Mul(risky, obs.Price)
			marked.Div(marked, big.NewInt(1_000_000_000_000_000_000))
			assets.Add(assets, marked)
		}
	}

	// Less what the account owes.
	//
	// An UNREADABLE debt is not a zero debt. Both reads below used to fall
	// through to `debt = 0` on error, so a transient RPC failure produced
	// a plausible positive NAV for a levered account -- and that figure
	// then resolved a forecast that later underwriting scores. The asset
	// path above already returns unobservable rather than guessing; this
	// now matches it. Reported by review, 15 September.
	debt := big.NewInt(0)
	if manager != "" {
		loanID, err := readActiveLoan(ctx, rpc, manager, account)
		if err != nil {
			return 0, 0, false
		}
		if loanID != 0 {
			d, err := readDebtDue(ctx, rpc, manager, loanID, at.Number)
			if err != nil {
				return 0, 0, false
			}
			debt = d
		}
	}
	nav := new(big.Int).Sub(assets, debt)
	if nav.Sign() < 0 {
		// Insolvent. Reported as a real zero NAV, distinct from the
		// unobservable zero above -- the caller must be able to tell a
		// wiped-out account from a failed read.
		nav = big.NewInt(0)
	}

	v := nav.Uint64()
	return v, v, true
}

// readDebtDue reads what a loan currently owes, principal plus unpaid
// accrued interest, at a pinned block.
func readDebtDue(
	ctx context.Context,
	rpc *chain.RPC,
	manager string,
	loanID, blockNumber uint64,
) (*big.Int, error) {
	data := append(attest.Selector("debtDue(uint256)"),
		attest.Word(new(big.Int).SetUint64(loanID))...)
	out, err := rpc.CallContract(ctx, manager, data, blockNumber)
	if err != nil {
		return nil, err
	}
	if len(out) < 32 {
		return nil, fmt.Errorf("short debtDue result")
	}
	return new(big.Int).SetBytes(out[:32]), nil
}

// bindEngineOutput replaces the envelope's placeholders with the engine's
// own identity, content hash, and authorization class.
//
// `docs/04`: the signed hashes are what a reader verifies a retained
// assessment against. A constant there proves nothing, and an inflated
// class would claim an executable authorization the engine did not grant.
func bindEngineOutput(
	a *attest.Assessment,
	terms *assessedTerms,
	raw json.RawMessage,
) error {
	id, err := parseHashHex(terms.AssessmentID)
	if err != nil {
		return fmt.Errorf("assessment_id: %w", err)
	}
	input, err := parseHashHex(terms.InputHash)
	if err != nil {
		return fmt.Errorf("input_hash: %w", err)
	}

	// The canonical hash of the whole assessment, the same value
	// store.Retain computes, so chain and retained evidence agree.
	decoded, err := canonical.DecodeStrict(raw)
	if err != nil {
		return fmt.Errorf("canonicalize: %w", err)
	}
	contentHex, err := canonical.HashHex(decoded)
	if err != nil {
		return fmt.Errorf("hash assessment: %w", err)
	}
	content, err := parseHashHex(contentHex)
	if err != nil {
		return fmt.Errorf("assessment hash: %w", err)
	}

	class, err := mapClass(terms.Class)
	if err != nil {
		return err
	}

	a.AssessmentID = id
	a.InputHash = input
	a.AssessmentHash = content
	// The input hash commits to the evidence values the decision rested
	// on, which is what the root must reference.
	a.EvidenceRoot = input
	a.Class = class
	return nil
}

// mapClass refuses an unknown class rather than defaulting.
//
// A default would silently sign one authorization level while the engine
// granted another -- and ScenarioOnly is NON-EXECUTABLE by design, so
// defaulting upward is exactly the failure to avoid (`docs/04`).
func mapClass(name string) (uint8, error) {
	switch name {
	case "ScenarioOnly":
		return attest.ClassScenarioOnly, nil
	case "ObservedShadow":
		return attest.ClassObservedShadow, nil
	case "PrototypeEligible":
		return attest.ClassPrototypeEligible, nil
	default:
		return 0, fmt.Errorf("unknown authorization class %q", name)
	}
}

func parseHashHex(s string) (attest.Hash, error) {
	var h attest.Hash
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return h, fmt.Errorf("not hex: %w", err)
	}
	if len(b) != 32 {
		return h, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	copy(h[:], b)
	return h, nil
}

// readExecutableClass asks the oracle which class it will act on.
func readExecutableClass(
	ctx context.Context, rpc *chain.RPC, oracle string,
) (uint8, error) {
	// Pinned to a real block: 0 encodes as the genesis block, where the
	// oracle does not yet exist.
	at, err := rpc.LatestBlock(ctx)
	if err != nil {
		return 0, err
	}
	out, err := rpc.CallContract(ctx, oracle,
		attest.Selector("executableClass()"), at.Number)
	if err != nil {
		return 0, err
	}
	if len(out) < 32 {
		return 0, fmt.Errorf("short result")
	}
	return out[31], nil
}

// declineReason extracts the engine's own explanation.
func declineReason(raw json.RawMessage) string {
	var doc struct {
		Outcome struct {
			Decision string `json:"decision"`
		} `json:"outcome"`
		Reasons []json.RawMessage `json:"reasons"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "unreadable assessment"
	}
	parts := make([]string, 0, len(doc.Reasons))
	for _, r := range doc.Reasons {
		parts = append(parts, string(r))
	}
	return doc.Outcome.Decision + " " + strings.Join(parts, ", ")
}

// intendedAcquire describes the action this run will take, or nil when the
// principal simply stays as settlement.
func intendedAcquire(adapter string, deployBps uint64, riskyAsset string) *AcquireAction {
	if adapter == "" || deployBps == 0 || riskyAsset == "" {
		return nil
	}
	// The declared valuation for the test asset, matching what the chain
	// reader marks positions at.
	return &AcquireAction{
		Asset:      riskyAsset,
		Price:      2_000_000_000,
		PriceScale: 6,
		HaircutBps: 2_000,
		// The share this run will actually deploy, so the engine projects
		// the same book rather than a safer one.
		DeployBps: uint32(deployBps),
	}
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

	if err := approveAdapter(
		ctx, w, reader, account, settlement, adapter, amount,
	); err != nil {
		return err
	}
	_ = settlementAddr

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
// holdUntilChainTime waits until the chain's own clock has advanced by
// seconds. A public chain has no evm_increaseTime, and wall-clock sleep
// alone proves nothing: block.timestamp is what the contract charges
// interest against, and it only moves when a block is produced.
func holdUntilChainTime(ctx context.Context, rpc *chain.RPC, seconds uint64) error {
	start, err := rpc.LatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("read chain time: %w", err)
	}
	deadline := start.Timestamp + seconds
	// Generous relative to the hold: a chain that cannot produce a block
	// carrying a later timestamp within this window is not one the loan
	// can be honestly held on, and repaying anyway would report interest
	// for time that never passed.
	limit := time.Now().Add(time.Duration(seconds)*time.Second + 5*time.Minute)
	for {
		now, err := rpc.LatestBlock(ctx)
		if err != nil {
			return fmt.Errorf("read chain time: %w", err)
		}
		if now.Timestamp >= deadline {
			log.Printf("chain time advanced %ds over %d blocks",
				now.Timestamp-start.Timestamp, now.Number-start.Number)
			return nil
		}
		if time.Now().After(limit) {
			return fmt.Errorf(
				"chain time advanced only %ds of %ds required",
				now.Timestamp-start.Timestamp, seconds)
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

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

// approveAdapter grants the adapter an allowance on the account.
//
// Split out of `deployPrincipal` so atomic origination can grant it
// BEFORE the draw: `originateAndExecute` performs the approved trade
// inside the same transaction, and the adapter needs its allowance to
// already exist by then. The approval moves no funds on its own.
func approveAdapter(
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
	approve := append(attest.Selector("approveAdapter(address,address,uint256)"),
		attest.AddressWord(settlementAddr)...)
	approve = append(approve, attest.AddressWord(adapterAddr)...)
	approve = append(approve, attest.Word(new(big.Int).SetUint64(amount))...)
	return sendAndWait(ctx, w, reader, account, approve, "approveAdapter")
}

// encodeOriginateAndExecute funds the loan and performs the approved
// action in one transaction.
//
// `docs/05` requires the two to be atomic where a trade is part of the
// approval, and the contract now enforces it: a plain draw against an
// action-bound quote reverts with `ActionRequired`.
func encodeOriginateAndExecute(
	b attest.Binding,
	principal uint64,
	adapter [20]byte,
	action []byte,
) []byte {
	selector := attest.KeccakBytes([]byte(
		"originateAndExecute((uint256,address,uint256,address,address," +
			"uint256,uint64,uint64,bytes32,uint64,bytes32),uint256,address,bytes)"))

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
	out = append(out, attest.AddressWord(adapter)...)

	// The struct is static (11 words), so the dynamic `bytes` tail starts
	// after binding + principal + adapter + this offset word.
	const head = 11*32 + 32 + 32 + 32
	out = append(out, attest.Word(big.NewInt(head))...)
	out = append(out, attest.Word(big.NewInt(int64(len(action))))...)
	out = append(out, action...)
	if pad := len(action) % 32; pad != 0 {
		out = append(out, make([]byte, 32-pad)...)
	}
	return out
}
