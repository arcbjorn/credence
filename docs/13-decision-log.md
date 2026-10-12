# 13 · Decision Log

What changed from the original vision, and why. Each entry names the reasoning, so a decision can be revisited when its premise changes.

## Structural

| # | Decision | Reason |
|---|---|---|
| D1 | **`CreditAccount` added as the foundation** | Lowering a credit limit recovers nothing. The capital already disbursed must be controlled, or the borrow-and-abandon attack succeeds against a protocol that records it perfectly. |
| D2 | **Equity required in v0** | Equity provides the initial loss cushion. Custody does not guarantee recovery; principal LGD can reach 100%. Selects the secured portion of the original's design space, which already contained `required_collateral` and wallet policies. |
| D3 | **Enforceable capital control before behavioral scoring** | Sequencing. History improves terms *within* limits custody and stress establish. |
| D4 | **Three sequential loans made a release requirement** | A single lifecycle cannot show credit compounding, which is the original thesis. |
| D5 | **Controlled ablation made a release requirement** | `min()` can clamp every scenario, leaving behavioral underwriting decorative. Without this the product is a competent collateralized lender, not Credence. |
| D6 | **Continuous underwriting made an explicit acceptance case** | The original's central case is a shock changing risk while a loan is open. The reference scenario could otherwise pass without it. |

## Economic

| # | Decision | Reason |
|---|---|---|
| D7 | **Headline arithmetic rebuilt** | 9.8% APR earns 80.55 over 30 days; 1.7% PD implies 170.00 expected loss. The lender lost money in expectation. |
| D8 | **Distinguish assumed, illustrative, and realized LGD** | A liquidation path is a recovery *route*, not a guaranteed recovery *rate*. Hand-calculated examples span 0%–88% principal LGD; realized claims require retained executions and reconciled loss records. |
| D9 | **PD relabeled `assumed_pd`** | A deterministic engine reproduces a badly calibrated estimate perfectly. No validation data exists. |
| D10 | **Letter grades dropped permanently** | A/AAA implies a calibrated default scale that does not exist. |
| D11 | **Score restored as a derived quantity** | A monotone, rounded restatement of `agent_capacity` adds no new risk assumption. Publish the underlying capacity because rounding prevents exact inversion from the score alone. |
| D12 | **Score excludes lender-side constraints** | Otherwise vault scarcity moves a borrower-quality number: the same agent scores 750 or 525 depending on available cash. |
| D13 | **Factors measured by counterfactual, not weights** | Removing required evidence returns `Unavailable`. Valid reference inputs compare `agent_capacity`; deltas are reference minus actual in settlement-token units, not score contributions. They do not sum; `min()` is non-additive. |

## Scope

| # | Decision | Reason |
|---|---|---|
| D14 | **Basis trade excluded from v0 — final, no revisit** | Unknown 3 is closed: `isValidSignature` is absent from the Perpl implementation, so a contract cannot sign enrollment. Recovery would therefore run through an offchain Ed25519 keeper, making recovery timing **trust-dependent rather than contract-enforced**. That weakens the invariant every other claim rests on (02, 12). Even fully cleared, it trades the release's strongest claim for a more interesting strategy. |
| D15 | **Market feedback loop stays informational** | A controlled two-participant market has no independent validation. A depth gate is a *candidate* policy; the naive cost-versus-benefit argument does not establish an attacker's economic loss. |
| D16 | **Portability shipped in two of three parts** | Publishing and independent verification are buildable. Third-party *adoption* requires another team and cannot be claimed. |
| D17 | **Forecast surface narrowed to one scored event** | Drawdown probability resolves precisely. Return and duration comparison are retained roadmap items. |
| D18 | **gRPC retained, gated on transport equivalence** | Accept the isolated transport change only with identical reference vectors and passing applicable acceptance checks; otherwise ship JSON. |
| D19 | **Go retained with an expanded role** | Its substantive job is the simulation clock — compressing months of history into weeks — plus provider fan-out and the reassessment scheduler. |
| D20 | **All nine integrations retained, each gated** | A gate proves capability early; it is not a cut. The baseline must work with zero optional providers. |

## Honesty

| # | Decision | Reason |
|---|---|---|
| D21 | **"Undercollateralized" qualified** | Account assets secure the debt. The precise claim is that the agent posts far less than it controls. |
| D22 | **Absolute prior-art claim dropped** | Adjacent public work is findable in one search. State what Credence demonstrates directly instead. |
| D23 | **CRE verification claims narrowed** | Consensus protects workflow execution, not input truth. N nodes fetching one endpoint agree on one fabricated response. |
| D24 | **Field mapping marked as narrowing, not renaming** | `facility_limit` is the pinned debt ceiling; `agent_capacity` is the changing borrower limit; `available_credit` is headroom and `drawable_capacity` includes liquidity and gates. `required_equity` is contributed equity; `max_ltv` is a policy ceiling, separate from measured ratios. |
| D25 | **Synthetic data made visible rather than hidden** | Provenance per factor converts the honesty problem into a rigor signal. |
| D26 | **No claim that downstream changes cannot break the release** | Later changes introduce regressions. A pinned baseline, isolated extensions, and a re-run acceptance matrix are what protect it. |

## Specification clarifications

| # | Decision | Reason |
|---|---|---|
| D27 | **Evidence failure preserves origin and domain** | Required evidence failure blocks risk permission with `Unavailable`; missing optional enrichment is omitted without favorable credit. Scenario fallback uses a separate deployment. |
| D28 | **One authorization route per facility epoch** | Development signing cannot bypass a CRE facility. Both origination and monitoring use the facility's pinned route and explicit policy/configuration identifier-hash pairs. |
| D29 | **Exact requested principal or denial in v0** | Capacity is not a grant. Partial or larger draws change the committed financing and action assumptions; a different amount requires a new request. |
| D30 | **Monitoring is a separate assessment kind** | Continuous underwriting has no new borrower request. Monitoring can refresh or block risk permission and supersede unused quotes, but never originate a loan. |
| D31 | **Forecast penalty sets a bounded principal cap** | The cap participates in borrower capacity; APR remains separately priced. Insufficient comparable outcomes use the maximum penalty so missing data cannot earn favorable treatment. |
| D33 | **Perpl hedging at the vault, not through the basis trade** | The custody constraint binds `CreditAccount`, not the offchain services that already hold keys. A vault-level hedge needs no onchain position marking and touches none of unknowns 4–6. It also answers an open question: what protects the lender when recovery unwinds into a falling market. |
| D32 | **Registry fallback is development-only** | Local adapters keep the baseline moving, but release publication and independent reading must use the accepted selected-network registry. |
| D36 | **Product API added; it holds no authority** | The MetaMask plugin was specified as "a thin TypeScript plugin over the existing `/v1` product API". **There was no such API** — the gateway served `/health` and `/v1/assess`, and the latter exists for the CRE route and deliberately does not publish. So the work was three parts: extract the lifecycle from `cmd/credence-scenario` into `gateway/loans` (it was implemented and unshareable), add `gateway/productapi` with borrow/repay/loan, and write the plugin. **The constraint is that the API can only ASK**: every endpoint builds calldata with `loans` and sends it with `wallet`, and `LoanManager` still checks the oracle's quote, the binding, the state version and the caps. Checkable rather than asserted — the acceptance matrix passes unchanged at 21/21, and a live repay from a key holding no settlement tokens returned `publish_failed: execution reverted 0x90b8ec18` (`TransferFailed()`). `/v1/borrow` accepts a caller's binding because the oracle validates it, so accepting it grants no authority. |
| D35 | **gRPC over tonic, superseding an earlier hand-written codec** | The isolated transport change is accepted only if it passes identical canonicalization vectors and the applicable acceptance matrix without changing those frozen artifacts. It does: `scripts/verify-grpc.sh` evaluates one retained snapshot over **both transports** and compares the bytes — same sha256, and replay MATCHes on both. Real tonic server, real `protoc-gen-go-grpc` client, both generated from `schemas/proto/risk.proto`, so the ends cannot drift. Adding tonic takes the dependency count from 31 to 77 crates. gRPC is the standard, `prost` is far better tested than a 200-line hand-written codec, and a bespoke codec is code a reader must audit. The dependency cost is real but not decisive. **Every payload stays `bytes`** — decomposing the snapshot into protobuf fields would re-serialize it and change what the engine hashes. Both transports delegate to the same `handler`, so neither can validate differently. **It costs CI about 40 seconds** — measured on warm caches, 2m23s to 3m02s — because `risk-service` compiles tonic into the scenario scripts' debug build. That cost is accepted for the standard. |
| D34 | **Kuru included: both gates met** | The first gate requires feasibility with no blocker and a baseline demonstrating custody, repayment, recovery and loss accounting — all four carry PASS rows in `releases/2026-09-21.md`, at the tagged commit. The second requires origination, market transfer, collection, redemption and a tested default haircut; each maps to a named test in `contracts/test/NoteSeries.t.sol` (`subscriptionIssuesNotesOneForOne`, `transferMovesTheClaim`, `earlyRepaymentPaysOnlyAccruedInterest`, `repaidSeriesRedeemsAboveFaceValue`, `defaultedSeriesRedeemsBelowFaceValue`). Commit `1c46b57` added `NoteSeries.sol` and modified no baseline contract, because the series implements the funding-source interface `LoanManager` already calls. **What is included is the instrument, not price discovery** — both sides of any trade are project-controlled, and D15 keeps market feedback informational. |

## Revisit triggers

| Decision | Revisit when |
|---|---|
| ~~D14 basis trade~~ | **Settled — does not reopen.** A future release may revisit only if a venue offers contract-callable trading *and* a contract-callable forced unwind. |
| D15 market feedback | A market with independent participants and adversarially evaluated depth policy exists |
| D9 assumed PD | Independent repayment and recovery evidence supports validation |
| D2 equity requirement | A loss-bearing mechanism beyond the account exists — stake, insurance, tranching, first-loss |
| D17 forecast surface | Sample size supports reliability bins and a stated baseline |
