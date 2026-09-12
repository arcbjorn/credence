# 08 · Integrations

## The rule

Five deliverables each: documented capability check, normalized adapter, fixture-based contract test, visible product use, defined failure behavior.

> **An integration is complete only when the submitted environment demonstrates its actual behavior. A logo, an installed SDK, or an architecture box does not satisfy this.**

Status: `Unverified` · `Proven in spike` · `Integrated` · `Accepted` · `Deferred`.

**The baseline must work with zero optional providers.** Each has a defined omission behavior. Required chain access, custody execution, and release identity verification remain dependencies. A fallback for development does not automatically satisfy release acceptance, and no fallback changes the evidence or authorization policy of an existing facility.

## Matrix

| Integration | Capability check | Acceptance | If unavailable |
|---|---|---|---|
| Monad RPC | Chain, tokens, read consistency, transactions, explorer | Full state plus a successful and a reverting transaction | Another RPC; selected-chain deployment still required |
| Execution venue | Assets, custody, approvals, liquidity, pricing, bounded exit | Trade and unwind, proceeds to account | Labeled scenario venue; no external-market claim |
| ERC-8004 | Registry addresses, ABI, binding and feedback permissions | Registered identity, attributable feedback, transfer handling, independent reader on the selected network | Local adapter for development only; integration incomplete and release identity gate remains open |
| Envio | Events, lag, reorg handling, hosting | Reconstruct lifecycle with evidence links | Direct log indexer is baseline |
| Perpl | Supported endpoints on chosen network | A documented field changes an assessment | Feature absent |
| Nansen | Account coverage, quotas | A sourced feature demonstrably affects a decision | Omit, disclose coverage |
| CRE | Deployment access, network and forwarder, consumer path | Valid report accepted; invalid bounds and wrong routes rejected | Separate development-attested baseline; an existing CRE facility blocks new risk and cannot fall back to a development key |
| Kuru | Market creation, quote asset, fills, redemption | Tradable claim with completed redemption | Omitted from working-product claims |
| Agent wallet | SDK and policy boundary | Agent signs permitted action; prohibited custody fails | Basic runner wallet; no plugin claim |

## Strategy — economic purpose required

"One risky asset, one adapter" says what is *permitted*, not why an agent would *borrow*.

**By 14 September**, specify in one paragraph: the activity, why it needs financing, how the agent decides financing is worthwhile at the offered APR, and how it exits. Without this, demo beat 2 — where the agent accepts or declines terms — has no content.

### Why the basis trade is out of v0

Two common arguments against it are **wrong**, and the record matters because it changes what to check at the venue gate.

*Wrong: "margin sits off-chain, outside the custody domain."* Perpl is fully onchain on Monad, with a real Exchange contract on mainnet and testnet, and `createAccount(uint256)` is callable **onchain with collateral and no API authentication**. A contract can open and fund the account.

*Wrong: "the perp leg is unrecoverable, so the failure path cannot cover it."* Two documented facts rescue custody:

- **Withdrawals are never permitted via an API key, regardless of scope.** A trade-scope key cannot move margin out. Withdrawal stays onchain, gated by `CreditAccount`. **The owner-cannot-withdraw invariant holds.**
- Perpl's own liquidation engine closes undercollateralized positions and returns the bulk of remaining margin to the trader — landing **inside** the custody domain if the trader is `CreditAccount`.

So a workable shape exists: `CreditAccount` owns the Perpl account; an offchain keeper holds a trade-scope key it can use to close the hedge but never to withdraw. The cost is a **downgrade, not a break** — recovery timing depends on a keeper acting rather than being contract-enforced.

**The actual blocker is the calendar.** Six unresolved unknowns, all on the critical path:

1. Does `createAccount` succeed when called from a contract?
2. May `target_profile` in a delegated enrollment be a contract address?
3. Does enrollment accept EIP-1271? *(unmentioned in the docs — assume no)*
4. Can a keeper key close positions reliably under stress?
5. How is a perp position marked onchain for coverage math?
6. How does isolated margin compose with account coverage?

Spot-only carries **one** unknown: which venue. Eleven days to the baseline, one builder, custody plus recovery is the entire foundation. **Six critical-path unknowns do not fit; one does.**

**Retained as strategy adapter #2** with a bounded 2-day spike after the baseline is pinned, resolving unknowns 1–3. The venue capability to check is **contract-callable trading and a contract-callable forced unwind** — not "is it onchain", which Perpl already is. An exchange's own liquidation mechanism does not supply Credence's required recovery *timing*.

## CRE — what it proves

Consensus across independent nodes on a workflow that fetches your endpoint means **N nodes agreeing on the same fabricated response.** Consensus protects workflow *execution*, not input *truth*.

Two adapters, separate authorization domains: development EIP-712, and a CRE receiver validating expected forwarder and workflow identity. **Each facility pins its required route for the epoch.** A facility claiming CRE verification requires its CRE path for both origination and monitoring; a development key cannot remain an equally powerful alternative. The baseline development facility is separately bound. Adapter authentication precedes normalization into the common fields in [05 · Contracts](05-contracts.md).

| Level | What was established |
|---|---|
| Development attestation | A trusted signer approved the payload |
| CRE policy-checked report | The deployed workflow checked binding, freshness, allowed identifiers, explicit bounds |
| CRE recomputed assessment | The workflow ran pinned model logic on the committed input. **Claim only if demonstrated** |

**A returned `model_hash` string does not prove that model ran.**

The first useful workflow performs five named checks:

1. Independently read account and facility state and match the identity, configuration, policy, and state-version binding.
2. Check the assessment kind: origination matches its committed request; monitoring matches the existing loan and contains no origination terms.
3. Validate permitted identifiers and explicit arithmetic and policy bounds, including amount, APR, term, and risk permission where applicable.
4. Validate input and evidence commitments against the retained payloads.
5. Check sequence and validity windows; required evidence freshness is necessary for an allowed result. A stale-evidence monitoring report may only publish blocked permission.

State which upstream data remain trusted — **identical responses from one project-controlled API do not establish independent evidence.** These checks do not establish model recomputation.

Contracts retain their own exposure, epoch, state, and expiry checks because state changes after workflow execution. Prove rejection of at least one fabricated out-of-bounds assessment; **do not claim this establishes the truth of every in-bounds assessment.**

Request deployment access during milestone one. Preserve the distinction between local simulation and a deployed workflow.

## Kuru — gated extension

**Feasibility experiment by 15 September:** bounded test of market creation for an existing token, settlement-token quote, fill, holder redemption. Produces an estimate; commits nothing.

**Structure.** A separate `NoteSeries` funding route for one loan in an isolated deployment. **Never tokenize a receivable belonging to the baseline pooled vault** — each `LoanManager` binds to its allowed funding source. The series defines settlement asset, target principal, borrower, term, rate, maturity, recovery waterfall, and redemption method before funding. Fixed supply issued at origination; notes are a pro-rata claim on that loan's actual net collections. Early repayment pays accrued interest only — **the note does not promise unearned full-term interest.** Default can redeem below principal; pending recovery means pending redemption.

**The same loan must never appear as both a redeemable pooled-vault asset and an independently redeemable note claim.** Notes are not eligible collateral in this release.

### Market feedback stays informational

Show fundamental assessment and market-implied pricing **separately**. Do not convert note price into real-world PD without explicit models for recovery, timing, interest, and liquidity premiums.

**Why.** The note market is two participants you control. That does not imply zero displayed depth — it implies the market is **controlled, with observations lacking independent validation.** A price nothing independent produced cannot move executable credit.

**What would close it is unsettled.** A depth-based gate is a *candidate* policy requiring adversarial evaluation:

```text
CANDIDATE — unproven:
  observed depth   >= a declared multiple of the influence it can exert
  participants     >= 3 independent, none project-controlled
  observation age  <= freshness bound
  influence        <= bounded fraction of limit, tighten-only, rate-limited
```

**Do not claim a depth threshold makes manipulation unprofitable.** Comparing cost-to-cross-the-book against a financing benefit does not establish an attacker's economic loss: trading notional is not money irreversibly spent, true cost depends on the executable curve and the unwind, displayed liquidity can vanish or belong to cooperating participants, three addresses are not three independent actors, and the payoff can sit outside the affected facility. **Tightening is itself exploitable** — it can force sales benefiting an attacker's other positions.

The honest statement: **the market feedback loop is a meaningful future capability whose economic design is outstanding.** The counterfactual panel demonstrates the *mechanism* against synthetic prices; it does not show the mechanism is safe to connect.

**Gates.** 25 Sep — proceed only if the baseline demonstrates custody restrictions, repayment, recovery, and loss accounting, and feasibility showed no blocker. 2 Oct — must demonstrate origination, market transfer, collection, redemption, and a tested default haircut, or be excluded from the recorded demo. **No late baseline contract rewrite is justified to include Kuru.**

## Bounties

**Track: Onchain Finance & Trading** ($30,000, split three ways). The track explicitly names undercollateralised lending priced on onchain credit history.

Pursue only where the integration already serves the demo. Fits below are assessed against each bounty's **stated requirement**, not its sponsor name.

### Pursue — already on the spine

| Sponsor | Prize | Requirement | Why it fits |
|---|---|---|---|
| Envio | $1,000 | HyperIndex/HyperSync/HyperRPC powering a **core feature** | Lifecycle reconstruction from indexed events is a core feature, not a bolt-on |
| Chainlink | $3,000 | CRE workflow as an orchestration layer — **"build, simulate, or deploy"** | Simulation qualifies, so the attestation path can enter even if deployment access never lands |

### Conditional — genuine fit, gated

| Sponsor | Prize | Requirement | Condition |
|---|---|---|---|
| Kuru | $5,000 | **New assets and markets** — a new tradable class *plus the infrastructure to make it viable* | `NoteSeries` tokenized agent debt is literally a new asset class. Behind the 25 Sep and 2 Oct gates above. |
| Nansen | $5,000 | A product experience **beyond exposing raw data** | Provenance-tagged enrichment that demonstrably changes an underwriting decision |

### Do not chase — would require building something else

| Sponsor | Prize | Requirement | Why it does not fit |
|---|---|---|---|
| Perpl | $3,000 | Analytics/risk dashboard **focused on Perpl** | The stress engine underwrites Credence accounts, not Perpl positions |
| Perpl | $5,000 | **Production-ready trading bot** on Perpl | The agent trades spot through our own adapter; the basis trade is deferred to adapter #2 |
| MetaMask | $2,500 | A **plugin** extending the Agent Wallet's plugin architecture | A separate deliverable, not a byproduct of the account policy layer |

Everything else — Agora (mobile apps), Cleanverse, Aurora, Privy, Dynamic, Alchemy, Hunyuan, KIMI, Qwen, Mera — requires a product shape this release does not have.

**Check eligibility, not fit:** Monad Foundation's *Best Community Team Project* ($5,000) turns on whether the team qualifies as a Metropolis community supporter. Confirm during milestone one.

### Expectation

| Outcome | Range |
|---|---|
| Likely — Envio + CRE | $1k–$4k |
| Kuru clears its gates | $6k–$9k |
| Everything lands | ~$14k |

**Bounties reward eligible submissions at judges' discretion. They are not expected revenue or a guaranteed return on integration work.** An integration that would not otherwise earn a place in the demo is not worth its bounty.
