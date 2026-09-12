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
| Reputation is independently verifiable | Standalone reader using no Credence endpoint |
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
Yes — a policy model, labeled as such. Determinism gives auditability, not accuracy. LGD is likewise a named assumption: custody creates a recovery route, not a recovery rate, and realized LGD runs 0% to 88% across shock severities.

**"Does CRE actually verify anything?"**
It verifies its five named checks and nothing more. Consensus protects execution, not input truth.

**"Why not over-collateralize?"**
A secured account does not require the agent to supply all financed capital upfront — that is the distinction v0 demonstrates: 5,000 of equity controlling 10,000 of assets. Full over-collateralization requires the agent to already hold what it wants to deploy, and the premise disappears.

**"Someone already built agent credit scores."**
Conceptually adjacent work exists; we make no claim about what it does or does not demonstrate. State what Credence demonstrates directly: a forecast committed onchain before disbursement and scored against a defined rule, an assessment replayed byte-identically from retained evidence, a prohibited withdrawal reverting onchain, a default with reconciled lender loss, three sequential loans under one policy, and a controlled ablation showing which evidence moved the terms.

**"Is your demo data real?"**
No, and it says so on every screen. Synthetic scenarios, deliberately built, with provenance per factor. It is how a risk team evaluates an underwriting model before deployment, and it exercises failure paths real data could not surface in six weeks.
