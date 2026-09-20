# 12 · Claims & Roadmap

## Claims the release can earn

**Status as of 19 September.** A claim is *earned* only when the support
beside it exists and was re-run; anything else says what is missing. Eight
of ten are earned, and the two that are not are unearned for different
reasons. **A deployed CRE workflow is closed**: deploy access needs a
commercial agreement with Chainlink, which is not a decision this project
gets to make. **A tradable note is unearned on a narrower
point than before**: the instrument exists, its redemption arithmetic is
tested, and it is listed on a real orderbook — but every participant so
far is project-controlled, and a controlled market has valued nothing.

| Claim | Required support | Status |
|---|---|---|
| Decisions are reproducible | Retained canonical inputs plus matching replay | **Earned** — `demo-monitor.sh` beat 8 reopens the retained SQLite and replays to MATCH, with a tampered control that must DIFFER |
| Credit grows with demonstrated behavior | Three sequential loans under one policy, plus controlled ablation | **Earned** — 1200 → 1147 → 1094 bps under one policy and model hash, binding switching to `StressCapacity` at loan 3; ablation in `cargo test -p risk-core --test ablation` |
| Current risk overrides reputation | Concentrated scenario with binding portfolio constraints | **Earned** — the Concentrated profile is `Denied` on a flawless record, on the three-agent screen and in `concentration_overrides_perfect_history` |
| Borrowed capital is constrained | Actual contract rejection and adapter boundaries | **Earned** — beat 6 reverts on chain with two DIFFERENT selectors: `0xe9d4b4e2` for open debt, `0x72cb8533` for a stranger |
| Losses are accounted for | Realized collection and final settlement reconcile | **Earned** — 4,000 lent = 1,400 recovered + 2,600 written off, reconciled on the lender screen since 18 September |
| Underwriting is continuous | Unprompted reassessment reaching chain within a declared interval | **Earned** — `trigger=price_move` produced a `Blocked` result on chain ~1s after the observation moved, against a 600s heartbeat ([03](03-underwriting.md)) |
| Forecasts are evaluated | Prior commitment, specified outcome protocol, retained resolutions | **Earned** — commitment precedes funding, and `LOANS=4` retains three resolved Brier scores. **Three observations permit an average, not a reliability claim** ([06](06-identity-forecasts.md)) |
| Reputation is independently verifiable | Standalone reader using the accepted selected-network registry and no Credence endpoint | **Earned, 19 September** — two repaid obligations for agent 1830 on the canonical registry, read back from `/tmp` with no Credence endpoint. Attributable and filterable; **not** proof the referenced evidence is true, and **not** third-party adoption ([11](11-verification.md)) |
| CRE checks an assessment | Exact deployed workflow checks and report evidence | **Not earned, and now closed rather than pending.** The receiver is deployed, 21 tests pass and the workflow simulates against the live forwarder — but deploy access turned out to require a commercial agreement, not an approval (19 Sep), so **no DON-signed report will reach the live receiver in this release** ([08](08-integrations.md)) |
| A note is tradable debt exposure | Defined holder rights, market transfer, redemption | **Still not earned, and now for a sharper reason.** The instrument is built and tested (`NoteSeries.sol`, 16 tests) and **listed on a real Kuru orderbook on testnet, 20 Sep**, which Kuru's Router confirms with `verifiedMarket() = true`. But both participants are project-controlled, so nothing independent has priced a note. `docs/08`: *a price nothing independent produced cannot move executable credit* ([08](08-integrations.md)) |

## Claims the release must not make

**Calibrated default probabilities** · **guaranteed liquidation recoveries** · **organic note liquidity** · **complete Sybil resistance** · **proof of offchain model execution** · **third-party adoption of published reputation** · **a bounty award**

High APR does not establish profitability. Synthetic tests do not establish demand. Determinism establishes auditability, not accuracy.

## Elements explicitly classified

**Independent wallet layer.** The original's daily exposure and per-transaction value policies are **largely replaced** by `CreditAccount` restrictions, which enforce the same protections at the custody boundary. One gap: per-day and per-transaction **velocity limits** have no equivalent, and are retained as optional hardening — not silently assumed.

**Third-party assessment consumption.** The original's broader ambition — others using Credence assessments for **payment terms, treasury mandates, and insurance premiums**, beyond loans Credence originates — is a roadmap item. The independent reader is the first concrete step toward it.

## The bridge back to the vision

Deferring the hardest promises requires saying what would make them feasible. None of these are in scope for 13 October.

| Milestone | Evidence needed |
|---|---|
| **Behavioral underwriting adds value** | Compare decisions and realized outcomes against a baseline policy using only collateral and current positions. The ablation shows behavior *changes* terms; this shows the change is *better*. |
| **Broader credit becomes supportable** | Independent repayment and recovery evidence, plus an explicit mechanism for bearing exposure beyond recoverable assets — operator stake, insurance, tranching, or a first-loss provider |
| **Market prices inform underwriting** | Redeemable instruments, market observations credible enough to act on, and a tested policy for their bounded influence |

> **More repayment history alone is not a bridge to unsecured lending.** That transition needs a separate economic design, and naming it as such is more credible than implying history accumulation gets there on its own.

## Product validation alongside the build

Prepare a one-page facility example for an agent operator and a capital provider: required equity, permitted strategy, offered terms, custody restrictions, loss waterfall, withdrawal timing.

The useful questions are concrete:

- Would the operator accept these restrictions?
- Does the facility enable activity they otherwise cannot finance?
- Would a lender fund the documented recovery exposure?
- Which operational failures would change their decision?

> **Which agent operator needs this financing today, and which capital provider would fund it under these recovery rules?**

One credible operator and one credible lender evaluating the same facility would strengthen the submission more than any additional integration.

Treat interviews and counterparties as separate from synthetic usage metrics.

## Adversarial Q&A

**"Isn't this just collateralized lending?"**
With respect to borrowed capital, yes — the account secures it. The claim is that the agent posts far less than it controls, with the multiplier and price set by demonstrated behavior.

**"What stops Sybil farming?"**
Nothing stops identity creation, and we claim **no profitability bound.** Three mechanisms *constrain* it: exposure×duration weighting makes cheap history earn little; the credit account stops a defaulting agent walking away with principal; v0 requires agent equity. Cheap history still earns something, and imperfect execution rules leave room for value extraction — which is why bad-price trades through approved venues are tested explicitly.

**"Your PD is made up."**
Yes — `assumed_pd` is a named scenario assumption, separate from the policy APR. Determinism gives auditability, not accuracy. Custody creates a recovery route, not a guaranteed recovery rate. The hand-calculated examples in [02 · Credit Accounts](02-credit-accounts.md) produce illustrative principal LGDs of 0%–88%; they are not measured results. Label realized scenario LGD only after retaining the corresponding collection and loss records.

**"Does CRE actually verify anything?"**
When its deployed path is demonstrated, it performs the five named policy checks in [08 · Integrations](08-integrations.md). A development attestation alone earns no CRE claim. Consensus protects workflow execution, not input truth or offchain model execution.

**"Why not over-collateralize?"**
The account can be overcollateralized while the agent contributes less than the capital it deploys. With 5,000 of equity and 5,000 borrowed, 10,000 of gross account assets secure 5,000 of principal; borrowed-funded assets are collateral too. Net stressed coverage also accounts for price shocks, closeout costs, and reserved interest. The distinction is contributed equity versus total collateral, with history setting capacity and price within those coverage constraints.

**"Someone already built agent credit scores."**
Conceptually adjacent work exists; we make no claim about what it does or does not demonstrate. State what Credence demonstrates directly: a forecast committed onchain before disbursement and scored against a defined rule, an assessment replayed byte-identically from retained evidence, a prohibited withdrawal reverting onchain, a default with reconciled lender loss, three sequential loans under one policy, and a controlled ablation showing which evidence moved the terms.

**"Is your demo data real?"**
The economic histories and generated market scenarios are synthetic and labeled per factor. Contract transactions and account observations may be real on the test network, while still reflecting project-generated economic activity. The interface distinguishes both origins and any replayed agent trace. These scenarios exercise failure paths that an independent borrower population could not establish during the 32-day build.
