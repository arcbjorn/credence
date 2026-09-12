# 00 · Overview

## The problem

Autonomous agents operate on pre-funded capital. A human must give an agent money before it can act. There is no standardized way to answer: has this agent behaved responsibly, repaid before, estimated its own risk accurately, and how much capital should it be trusted with?

## The product

An agent contributes equity, borrows against it inside an account Credence controls, deploys the combined capital, and repays. Its behavior changes the terms of the next loan.

```text
equity 5,000  +  borrowed 5,000  =  10,000 deployed
                                    └─ inside CreditAccount, withdrawal locked
```

**This is collateralized lending.** The account secures the debt. The claim is *not* that reputation replaces collateral — it is that **an agent controls more capital than it owns, and its demonstrated behavior sets the multiplier and the price.** Prime brokerage is the right analogy for the shape; the delivered product is narrower.

Undercollateralized lending — credit extending beyond recoverable account assets — is a later milestone with its own economic design. See [12 · Claims & Roadmap](12-claims-roadmap.md).

## What ships

1. One settlement asset, one risky asset, one network
2. One restricted strategy adapter; one active loan per account
3. Fixed-epoch funding vault with explicit lender accounting and final settlement
4. Origination, execution, repayment, maturity, unwind, shortfall, lender withdrawal
5. Deterministic Rust engine with explanatory factors and reproducible replay
6. Go service owning persistence, adapters, event ingestion, virtual-time scenarios
7. Forecast commitments with defined outcomes, observation windows, and scoring
8. Three agent scenarios on one engine and one set of contract interfaces
9. Interface distinguishing scenario evidence, observed evidence, authorization paths
10. Reproducible deployment, invariant tests, public demonstration

## What does not ship

| Deferred | Reason |
|---|---|
| ETH delta-neutral basis trade | Six critical-path unknowns; spot has one. [08](08-integrations.md) |
| Executable market feedback from note prices | Manipulation economics unresolved. [08](08-integrations.md) |
| Undercollateralized lending | Needs a loss-bearing mechanism beyond the account. [12](12-claims-roadmap.md) |
| Calibrated default probabilities | Requires validation data that does not exist yet. [03](03-underwriting.md) |
| Monte Carlo, multi-strategy, cross-chain | Scope |
| Predicted-vs-actual return and duration | Release scores one forecast dimension. [06](06-identity-forecasts.md) |

## Three operating modes

| Mode | Evidence and time | Permitted effect |
|---|---|---|
| **Scenario** | Synthetic or recorded, explicit virtual clock | Evaluate policy; isolated test-token demonstration |
| **Observed / shadow** | Current observations, real timestamps | Assess and compare. **No disbursement.** |
| **Authorized prototype** | Actual contract state, chain time, validated attestation | Draw from the specifically bound prototype vault |

Scenario history must never cross into an observed-evidence facility through a relabeled field or a reused signature. Separate deployment addresses isolate scenario facilities; a frontend toggle cannot change a contract's evidence policy.

The release target is **a prototype with test capital.** Mainnet with real lender funds is a later decision, not an implied consequence of finishing this plan.

## Release boundary

| Component | Status |
|---|---|
| Credit account, lifecycle, liquidation, recovery | Essential |
| Deterministic underwriting and replay | Essential |
| Explainable decisions with provenance | Essential |
| Fixed-epoch vault accounting | Essential |
| ERC-8004 identity and repayment evidence | Keep; registry checked early |
| Forecast commitment and Brier scoring | Keep, narrowly defined |
| Envio indexing and market data | Keep |
| CRE workflow | Access 13 Sep; level fixed 25 Sep |
| Nansen enrichment | Only if it changes a demonstrated decision |
| Kuru note market | Gated — [08](08-integrations.md) |
