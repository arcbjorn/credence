# 10 · Schedule

**32 days.** Dates, not nominal weeks. **If a milestone slips, use its fallback immediately** rather than pushing work toward the deadline.

| Dates | Work | Completion evidence |
|---|---|---|
| **11–13 Sep · milestone one** | Demo script; freeze v0 conventions; specify custody and accounting; confirm event rules and CRE access | Example snapshots, state diagrams, capability checklist, ~~first screen shell~~ **slipped** |
| **14–17 Sep · milestone two** | risk-core skeleton, virtual clock, vault/account/loan lifecycle on local chain; probe venue, registry, and Kuru | Deterministic scenario plus borrow, trade, repay, prohibited withdrawal, loss-accounting tests |
| **18–22 Sep · baseline milestone** | Connect gateway, contracts, interface; deploy prototype network | One complete performer path and one failure path with retained evidence and transaction references |
| **23–25 Sep** | Other two scenarios, forecast scoring, identity continuity, registry publication and independent reader, event reconciliation | Same engine handles all agents; release identity evidence complete; Kuru gate decision |
| **26 Sep–2 Oct** | Selected provider integrations and CRE; Kuru only after its gate | Each claimed integration passes acceptance; note redemption complete if included |
| **3–5 Oct** | Resolve cross-component issues; **freeze contracts, schemas, model, fixtures**; prepare isolated transport change | Working JSON release candidate; no blocker on mandatory paths |
| **6 Oct** | Gate the isolated protobuf/gRPC transport change against frozen artifacts | Same reference vectors and applicable acceptance matrix pass; otherwise retain JSON |
| **7–9 Oct** | Exercise failures, restore from retained data, rehearse, record | Verification report, accessible deployment, both recordings |
| **10–12 Oct** | Public profile, write-up, code access, integration evidence, bounty entries | Submission complete, links checked from a clean viewer context |
| **13 Oct** | Contingency | Essential corrections only; **no feature implementation** |

## 22 September — the commitment line

From here everything raises fidelity. **Later changes can still introduce regressions**, so the claim is narrower than "nothing can break it." What actually protects the submission:

- A **pinned baseline release tagged on 22 September**
- Extensions developed in **isolation** from it
- All acceptance rows applicable to the current baseline and included extensions **re-run before every merge** into the demo branch; remaining release rows stay explicitly pending until completed

## Milestone failures

| Condition | Response |
|---|---|
| Venue unproven 14 Sep | Labeled scenario venue so custody and accounting finish |
| Loss accounting incomplete 17 Sep | Stop enrichment work; finish it first |
| End-to-end loop incomplete 22 Sep | Defer Kuru and nonessential providers |
| Registry or independent reader incomplete 25 Sep | Prioritize the required identity path; a local adapter does not close the release gate |
| CRE unresolved 2 Oct | Freeze development attestation; describe status accurately |
| Kuru settlement incomplete 2 Oct | Omit live note trading from the primary demo |
| gRPC not green 6 Oct | Ship JSON; boundary and diagram unchanged |
| 5 Oct | Every included feature has a working path **and** a defined failure path on JSON; the isolated 6 Oct transport gate adds no feature or schema change |

> **Scope reductions preserve three outcomes: accountable decisions, enforced capital rules, visible recovery and losses. Remove secondary integrations before weakening these.**

## Decisions to resolve early

| Decision | Due | Working default |
|---|---|---|
| Event environment and submission specifics | 13 Sep | Monad testnet prototype; submit 12 Oct. **Demos verified portable 13 Sep**: run against chain id 10143 with no code change, manifests keyed by chain |
| Settlement and risky assets, venue, valuation | 14 Sep | **Settled 13 Sep** — working default taken; see [08 · Integrations](08-integrations.md#assets-venue-and-valuation--settled-13-september) |
| Strategy economic purpose | 14 Sep | **Settled 13 Sep** — see [07 · Simulation](07-simulation.md#what-the-strategy-does-and-why-it-is-financed) |
| CRE access and verification level | Access 13 Sep; level 25 Sep | **Available 13 Sep** — CRE supports `monad-testnet`, forwarder verified on chain. Access is self-service via `cre account access`; simulation needs no approval. Level stays development attestation until a workflow is deployed |
| ERC-8004 deployment and feedback compatibility | 15 Sep | **Resolved 13 Sep** — registries live on Monad testnet, selectors verified against the standard; see [06 · Identity](06-identity-forecasts.md#registry-compatibility--resolved-13-september). **Reader rewritten 14 September**: `tools/reader/read_erc8004.py` reads the canonical registry |
| Policy thresholds and pricing schedule | 17 Sep | Conservative named assumptions; no calibrated-PD claim |
| Coverage definitions | 17 Sep | Marked vs stressed; closeout deducted in both |
| Kuru feasibility | 15 Sep | Small isolated experiment |
| Kuru implementation commitment | 25 Sep | Only after both gates |
| Optional provider inclusion | 25 Sep | Only fields with measured coverage and visible product use |

### Slipped — the screen shell

**Milestone one promised a first screen shell by 13 September. It slipped**,
and was built on 14 September: React 19 on Vite 8, with four of
`docs/09`'s eight screens — agent overview (beat 4b), assessment detail,
enforcement (beat 6), and replay (beat 8). The demo remains `scripts/`: bash driving forge and
the gateway binaries, printing what each beat proves. The interface reads
what those scripts retained rather than driving them.

Four screens are still unbuilt: three-agent comparison, evidence
inspector, loan lifecycle, and lender/epoch, plus the optional note view.

The scripts alone were enough to record a technical walkthrough, and the
verification rows they produce are real. They were **not** enough for a
finance-track judging session, where a reviewer expects to see three
accounts, the binding constraint named beside capacity and APR, a
reverted withdrawal, and replay MATCH against a tampered mismatch — with
mode labels that cannot collapse into a single green pill.

Of those, the built screens cover the binding constraint beside capacity
and APR, replay with its tamper control, and the reverted withdrawal —
which `credence-verify` records as JSON so the screen shows the two
DIFFERENT revert selectors rather than a sentence about them. The
three-account comparison is still script-only.

Recorded here rather than left implicit: an unflagged slip on the most
visible deliverable is the kind of gap a schedule exists to surface.
Reported by review, 14 September.

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

> **The baseline milestone on 22 September is complete when one request can be explained, enforced, repaid or recovered, and replayed from retained evidence. Every later feature must preserve that behavior. Milestone one on 13 September establishes the conventions and design artifacts used to build it.**
