# 13 · Decision Log

What changed from the original vision, and why. Each entry names the reasoning, so a decision can be revisited when its premise changes.

## Structural

| # | Decision | Reason |
|---|---|---|
| D1 | **`CreditAccount` added as the foundation** | Lowering a credit limit recovers nothing. The capital already disbursed must be controlled, or the borrow-and-abandon attack succeeds against a protocol that records it perfectly. |
| D2 | **Equity required in v0** | Without it there is no cushion, and LGD is unbounded. Selects the secured portion of the original's design space, which already contained `required_collateral` and wallet policies. |
| D3 | **Enforceable capital control before behavioral scoring** | Sequencing. History improves terms *within* limits custody and stress establish. |
| D4 | **Three sequential loans made a release requirement** | A single lifecycle cannot show credit compounding, which is the original thesis. |
| D5 | **Controlled ablation made a release requirement** | `min()` can clamp every scenario, leaving behavioral underwriting decorative. Without this the product is a competent collateralized lender, not Credence. |
| D6 | **Continuous underwriting made an explicit acceptance case** | The original's climax is a shock changing risk while a loan is open. The demo script could otherwise pass without it. |

## Economic

| # | Decision | Reason |
|---|---|---|
| D7 | **Headline arithmetic rebuilt** | 9.8% APR earns 80.55 over 30 days; 1.7% PD implies 170.00 expected loss. The lender lost money in expectation. |
| D8 | **LGD is a named assumption, never a bound** | A liquidation path is a recovery *route*, not a recovery *rate*. Realized LGD spans 0%–88% across shock severities; execution cost dominates at moderate shocks. |
| D9 | **PD relabeled `assumed_pd`** | A deterministic engine reproduces a badly calibrated estimate perfectly. No validation data exists. |
| D10 | **Letter grades dropped permanently** | A/AAA implies a calibrated default scale that does not exist. |
| D11 | **Score restored as a derived quantity** | The problem was opacity, not the number. A monotone restatement of capacity adds no assumption and is invertible by the reader. |
| D12 | **Score excludes lender-side constraints** | Otherwise vault scarcity moves a borrower-quality number: the same agent scores 750 or 525 depending on available cash. |
| D13 | **Factors measured by counterfactual, not weights** | Ablating to *absent* triggers the evidence policy and returns `Unavailable` — the limit is undefined, not higher. Reference values keep every counterfactual a valid portfolio. Deltas do not sum; `min()` is non-additive. |

## Scope

| # | Decision | Reason |
|---|---|---|
| D14 | **Basis trade deferred to adapter #2** | Not architecturally impossible — Perpl is fully onchain and API keys cannot withdraw. **Six critical-path unknowns versus one for spot, with 11 days to the baseline.** Revisit after a 2-day spike. |
| D15 | **Market feedback loop stays informational** | A controlled two-participant market has no independent validation. A depth gate is a *candidate* policy; the naive cost-versus-benefit argument does not establish an attacker's economic loss. |
| D16 | **Portability shipped in two of three parts** | Publishing and independent verification are buildable. Third-party *adoption* requires another team and cannot be claimed. |
| D17 | **Forecast surface narrowed to one scored event** | Drawdown probability resolves precisely. Return and duration comparison are retained roadmap items. |
| D18 | **gRPC retained, gated to after the 5 Oct freeze** | The boundary is architectural and permanent; only the wire format changes. JSON avoids schema-churn tax while `RiskInput` moves. |
| D19 | **Go retained with an expanded role** | Its substantive job is the simulation clock — compressing months of history into weeks — plus provider fan-out and the reassessment scheduler. |
| D20 | **All nine integrations retained, each gated** | A gate proves capability early; it is not a cut. The baseline must work with zero optional providers. |

## Honesty

| # | Decision | Reason |
|---|---|---|
| D21 | **"Undercollateralized" qualified** | Account assets secure the debt. The precise claim is that the agent posts far less than it controls. |
| D22 | **Absolute prior-art claim dropped** | Adjacent public work is findable in one search. State what Credence demonstrates directly instead. |
| D23 | **CRE verification claims narrowed** | Consensus protects workflow execution, not input truth. N nodes fetching one endpoint agree on one fabricated response. |
| D24 | **Field mapping marked as narrowing, not renaming** | Five quantities the original conflated are now distinct. `available_credit` is headroom, not drawable; `required_equity` is not total collateral; `max_ltv` needs its formula published. |
| D25 | **Synthetic data made visible rather than hidden** | Provenance per factor converts the honesty problem into a rigor signal. |
| D26 | **"Nothing downstream can break the submission" retracted** | Later changes introduce regressions. A pinned baseline, isolated extensions, and a re-run acceptance matrix are what actually protect it. |

## Revisit triggers

| Decision | Revisit when |
|---|---|
| D14 basis trade | The 2-day spike resolves contract-callable trading and forced unwind |
| D15 market feedback | A market with independent participants and adversarially evaluated depth policy exists |
| D9 assumed PD | Independent repayment and recovery evidence supports validation |
| D2 equity requirement | A loss-bearing mechanism beyond the account exists — stake, insurance, tranching, first-loss |
| D17 forecast surface | Sample size supports reliability bins and a stated baseline |
