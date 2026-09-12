# 12 · Claims & Roadmap

## Claims the release can earn

| Claim | Required support |
|---|---|
| Decisions are reproducible | Retained canonical inputs plus matching replay |
| Credit grows with demonstrated behavior | Three sequential loans under one policy, plus controlled ablation |
| Current risk overrides reputation | Concentrated scenario with binding portfolio constraints |
| Borrowed capital is constrained | Actual contract rejection and adapter boundaries |
| Losses are accounted for | Realized collection and final settlement reconcile |
| Underwriting is continuous | Unprompted reassessment reaching chain within a declared interval |
| Forecasts are evaluated | Prior commitment, specified outcome protocol, retained resolutions |
| Reputation is independently verifiable | Standalone reader using the accepted selected-network registry and no Credence endpoint |
| CRE checks an assessment | Exact deployed workflow checks and report evidence |
| A note is tradable debt exposure | Defined holder rights, market transfer, redemption |

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
