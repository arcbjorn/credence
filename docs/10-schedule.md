# 10 · Schedule

**32 days.** Dates, not nominal weeks. **If a milestone slips, use its fallback immediately** rather than pushing work toward the deadline.

| Dates | Work | Completion evidence |
|---|---|---|
| **11–13 Sep** | Demo script; freeze v0 conventions; specify custody and accounting; confirm event rules and CRE access | Example snapshots, state diagrams, capability checklist, first screen shell |
| **14–17 Sep** | risk-core skeleton, virtual clock, vault/account/loan lifecycle on local chain; probe venue and Kuru | Deterministic scenario plus borrow, trade, repay, prohibited withdrawal, loss-accounting tests |
| **18–22 Sep** | Connect gateway, contracts, interface; deploy prototype network | One complete performer path and one failure path with retained evidence and transaction references |
| **23–25 Sep** | Other two scenarios, forecast scoring, identity continuity, event reconciliation | Same engine handles all agents; Kuru gate decision |
| **26 Sep–2 Oct** | Selected provider integrations and CRE; Kuru only after its gate | Each claimed integration passes acceptance; note redemption complete if included |
| **3–5 Oct** | Resolve cross-component issues; **freeze contracts, schemas, model, fixtures**; migrate transport to protobuf/gRPC | Release candidate; gRPC passing the same reference vectors as JSON; no blocker on mandatory paths |
| **6–9 Oct** | Exercise failures, restore from retained data, rehearse, record | Verification report, accessible deployment, both recordings |
| **10–12 Oct** | Public profile, write-up, code access, integration evidence, bounty entries | Submission complete, links checked from a clean viewer context |
| **13 Oct** | Contingency | Essential corrections only; **no feature implementation** |

## 22 September — the commitment line

From here everything raises fidelity. **Later changes can still introduce regressions**, so the claim is narrower than "nothing can break it." What actually protects the submission:

- A **pinned baseline release tagged on 22 September**
- Extensions developed in **isolation** from it
- The acceptance matrix **re-run before every merge** into the demo branch

## Milestone failures

| Condition | Response |
|---|---|
| Venue unproven 14 Sep | Labeled scenario venue so custody and accounting finish |
| Loss accounting incomplete 17 Sep | Stop enrichment work; finish it first |
| End-to-end loop incomplete 22 Sep | Defer Kuru and nonessential providers |
| CRE unresolved 2 Oct | Freeze development attestation; describe status accurately |
| Kuru settlement incomplete 2 Oct | Omit live note trading from the primary demo |
| gRPC not green 6 Oct | Ship JSON; boundary and diagram unchanged |
| 5 Oct | Every included feature has a working path **and** a defined failure path |

> **Scope reductions preserve three outcomes: accountable decisions, enforced capital rules, visible recovery and losses. Remove secondary integrations before weakening these.**

## Decisions to resolve early

| Decision | Due | Working default |
|---|---|---|
| Event environment and submission specifics | 13 Sep | Monad testnet prototype; submit 12 Oct |
| Settlement and risky assets, venue, valuation | 14 Sep | Named test assets; isolated scenario venue if unproven |
| Strategy economic purpose | 14 Sep | One paragraph: activity, why financed, accept/decline logic, exit |
| CRE access and verification level | Access 13 Sep; level 25 Sep | Development attestation baseline |
| ERC-8004 deployment and feedback compatibility | 15 Sep | Explicit local adapter during development |
| Policy thresholds and pricing schedule | 17 Sep | Conservative named assumptions; no calibrated-PD claim |
| Coverage definitions | 17 Sep | Marked vs stressed; closeout deducted in both |
| Kuru feasibility | 15 Sep | Small isolated experiment |
| Kuru implementation commitment | 25 Sep | Only after both gates |
| Optional provider inclusion | 25 Sep | Only fields with measured coverage and visible product use |

## First implementation tasks

1. **Confirm event intake, deployment requirements, submission cutoff, and CRE access path**
2. Write the three-agent demo script and lender loss walkthrough **before** any score logic
3. Create schema conventions, missing-data examples, canonical hash reference vectors
4. Build a pure Rust evaluator that approves, denies, and returns unavailable on hand-checked inputs
5. Implement local vault, account, and loan transitions with conservation-of-funds tests
6. Add the virtual clock and independent generator; preserve origin in every derived feature
7. Connect one end-to-end lifecycle to a minimal interface
8. Run venue, identity, CRE, and Kuru capability checks against the **actual intended environment**
9. Lock the first working baseline before adding integration breadth

> **Milestone one is complete when one request can be explained, enforced, repaid or recovered, and replayed from retained evidence. Every later feature must preserve that behavior.**
