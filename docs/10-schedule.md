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

`scripts/release-check.sh` performs that re-run in one command and writes a dated record to `releases/`, naming every row it could not reach rather than counting it as a pass. `releases/README.md` carries the tagging procedure. **A first run on 20 September reported 15 passed, 0 failed, 2 skipped** — the skips being the independent reader, which needs funded keys on a public chain, and the indexed lifecycle, which needs Docker.

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
| CRE access and verification level | Access 13 Sep; level 25 Sep | **Resolved 19 Sep: deploy access is NOT obtainable for this release.** CRE supports `monad-testnet` and the forwarder is verified on chain, but the access request was answered with an invitation to a commercial call and an MNDA, not an approval. Simulation needs no approval and works. **Level is frozen at development attestation**, taking the 2 Oct failure row early rather than waiting on it |
| ERC-8004 deployment and feedback compatibility | 15 Sep | **Resolved 13 Sep** — registries live on Monad testnet, selectors verified against the standard; see [06 · Identity](06-identity-forecasts.md#registry-compatibility--resolved-13-september). **Reader rewritten 14 September**: `tools/reader/read_erc8004.py` reads the canonical registry |
| Policy thresholds and pricing schedule | 17 Sep | **Settled — recorded 19 Sep.** `config/engine-policy.json` pins every value as an exact integer: `base_rate_bps` 1200, discount and surcharge caps 400/800, APR floor and ceiling 500/4000, `base_history_cap` 6.2e9, `account_hard_cap` 12e9. Enforced in `risk-core` and pinned by the ablation, stress and engine suites. Conservative named assumptions, **no calibrated-PD claim** — `docs/12` forbids one |
| Coverage definitions | 17 Sep | **Settled — recorded 19 Sep.** [02 · Credit Accounts](02-credit-accounts.md) gives both formulas; `min_maintenance_coverage` is 23/20 and the origination minimum sits in the stress block, both read by `engine.rs` and `stress.rs`. Closeout is deducted in both, and passing marked coverage alone cannot authorize a draw |
| Kuru feasibility | 15 Sep | **Slipped, then run 19 Sep — no blocker.** Creation is permissionless (`deployProxy` simulates from an ordinary address), costs ~1.3M gas / 0.133 MON, and accepts the deployment's own tokens. The feasibility condition of the 25 Sep gate is met; what remains is scope, since `NoteSeries` is unbuilt and the 2 Oct gate wants redemption. See [08 · Integrations](08-integrations.md#feasibility--slipped-then-run-19-september) |
| Kuru implementation commitment | 25 Sep | **The instrument is built, 19 Sep** — `NoteSeries.sol` with 16 tests, added as a parallel funding source so the baseline manager is untouched. What remains for the gate is the MARKET half: listing on Kuru, a transfer for consideration, and a redemption on chain |
| Optional provider inclusion | 25 Sep | Only fields with measured coverage and visible product use |

### Slipped — the screen shell

**Milestone one promised a first screen shell by 13 September. It slipped**,
and was built on 14 September: React 19 on Vite 8. **All eight of
`docs/09`'s screens now exist** — agent overview (beat 4b), the
three-agent comparison (beats 1 and 5), assessment detail, the evidence
inspector, loan lifecycle with its forecasts (3, 4 and 7), lender epoch
ledgers, enforcement (beat 6), and replay (beat 8). The optional note view
is not built, and is marked optional in `docs/09`. The demo remains `scripts/`: bash driving forge and
the gateway binaries, printing what each beat proves. The interface reads
what those scripts retained rather than driving them.

Every figure on every screen comes from a retained fixture or a
transaction-derived record, as `docs/09` requires. The comparison is
assessed by the real engine and pinned by
`comparison_fixture_matches_the_engine`; the enforcement and lender
screens render what `credence-verify` read from chain.

The scripts alone were enough to record a technical walkthrough, and the
verification rows they produce are real. They were **not** enough for a
finance-track judging session, where a reviewer expects to see three
accounts, the binding constraint named beside capacity and APR, a
reverted withdrawal, and replay MATCH against a tampered mismatch — with
mode labels that cannot collapse into a single green pill.

All of those are now on screen: the three-agent comparison, capacity
beside its binding constraint, the reverted withdrawal showing the two
DIFFERENT revert selectors, replay with its tamper control, and per-item
evidence labels that `modes.test.ts` refuses to let collapse.

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
