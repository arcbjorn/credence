# 09 · Interface

The interface renders what the scenario runs retained and what the
deployment holds on chain. It computes no rate, capacity or decision of
its own: where a screen evaluates anything, it runs the same Rust engine
the gateway uses, compiled to WebAssembly. Routes are hash routes, for
example `#/agent`.

## Screens

| Screen | Route | Question it answers |
|---|---|---|
| Agent overview | `#/agent` | What can this agent borrow, what does it owe, and why? |
| Agent profiles | `#/compare` | How do history, exposure and forecast behaviour produce different outcomes under one policy? |
| Assessment detail | `#/assessment` | Which constraints bound this decision, and what evidence supports them? |
| Evidence | `#/evidence` | Where did each input come from, how fresh and complete is it, and is it synthetic? |
| Loan lifecycle | `#/arc` | Where did funds go, what is owed, and what happened at repayment or default? |
| Lender | `#/lender` | What cash is available, what principal is exposed, and what loss was realized? |
| Replay | `#/replay` | Does retained input reproduce the assessment under the pinned model? |
| Enforcement | `#/enforcement` | Which limits does the account enforce on chain, and which calls revert? |
| Borrow desk | `#/borrow` | What terms does the engine give a request composed now? |
| What a record earns | `#/earned` | How does the rate change when only the record changes? |
| Indexed lifecycle | `#/indexed` | What does the event log alone record about each loan? |
| Live on Monad | `#/live` | What does the deployment hold on chain now? |
| Perpl market risk | `#/perpl` | How does the observed perps market move the stress scenarios? |
| Observatory, Atlas, Moons, Body, Log | `#/observatory` to `#/log` | The same book drawn as one body: the pool, its agents, and each loan's path |
| Field guide | `#/guide` | How the drawing maps to the ledger |

The first eight are the record screens. A note view is specified and not
built; notes are covered by `scripts/demo-notes.sh` and
[08 · Integrations](08-integrations.md).

## Display rules

> **Every displayed number comes from a retained fixture or a transaction-derived record.**

Lead with amounts, terms, actions and reasons. Hashes, model versions and
service names go in expandable details. Reading a screen must not require
knowing Go, Rust or RPCs.

**Always show environment and evidence mode** in plain labels:
`Scenario history`, `Observed account state`, `Development attestation`,
`Deployed CRE policy checks`. **Never collapse these into one ambiguous
status indicator.** Live transactions, historical fixtures and simulated
outcomes stay visually distinct at all times.

The frontend never changes to make an expected result appear. When the
engine declines a loan, the decline is shown; a scenario whose economics
meet policy is declared separately.

### Check the built site

Tests assert what they were written to assert. Three classes of defect
passed every test and were visible only in the built static bundle:

| What a reader saw | Cause |
|---|---|
| `6,200HistoryGrowthCap` on one line | `td.num` had no right padding, so a right-aligned figure ran into the next cell |
| Every evidence row labelled `OBSERVED ACCOUNT STATE` | The origin label named an item type, and the inspector renders one label per item |
| Identical capacity in both Perpl columns, with prose claiming the market was deep enough | Capacity is a `min()` and `HistoryGrowthCap` was binding; the page inferred from a zero delta instead of reading `agent_binding` |

The label defect now has a test that fails if any origin label names an
item type. Releases are checked by serving `web/dist` and walking every
screen.

## Reference scenario

The scenario scripts drive the full path end to end from a clean chain.
Code and other documents refer to these steps by number.

| Step | Action | Evidence |
|---|---|---|
| 1 | Three accounts with different histories and exposures | Scenario labels on every account |
| 2 | The performer proposes an allocation, a requested amount and a financing ceiling | The agent trace, or an explicitly labelled replay; expected benefit after costs over the loan horizon |
| 3 | Commit a forecast and a request, assess, accept or decline terms, fund if accepted | Commitment reference, exact requested grant, binding reason, transaction, funds in the restricted account |
| 4 | Close the strategy and repay | Principal, realized flows, costs, accrued interest, updated history |
| 4b | Repeat twice more | Three sequential loans under one policy and model hash: the limit rises, the APR falls, and the binding constraint moves from history to stress capacity |
| 5 | Assess the concentrated account | Good history, denied by current portfolio limits |
| 5b | Unprompted reassessment | The market moves while a loan is open and no agent asks; a monitoring update with no origination terms reaches chain within the declared interval, a 600 s heartbeat or the next tick after a 5% price move ([03 · Underwriting](03-underwriting.md)) |
| 6 | Attempt a prohibited withdrawal | The transaction reverts |
| 7 | Overconfident borrower and recovery | `scripts/demo-default.sh` prices the loan through the engine and commits a 30% forecast before funding, crashes the asset, recovers what exists and writes off 2.6e9. The loan defaults before its forecast horizon, so that forecast stays unresolved and unscored; `LOANS=4 scripts/demo-repay.sh` produces resolved forecasts and their Brier scores |
| 8 | Replay a retained assessment | Exact model and input identifiers, and a verified match |
| 9 | Transfer and redeem a note | `scripts/demo-notes.sh`: a series is funded, its notes move to a second holder, and that holder redeems against actual collections, above face by the collected interest on a clean repayment and below face by 2,600,000,000 when only 1,400 of 4,000 comes back. The seller then redeems nothing. The Kuru market for these notes is controlled: both participants are project addresses (`docs/08`) |

In step 4b the rising limit is `agent_capacity`. It is shown beside
`facility_limit`, debt headroom (`available_credit`), `drawable_capacity`,
and requested and granted principal, each separately. Steps 5b and 6 need
an active loan, so the overconfident account's loan is opened before them
and its outcomes and recovery continue in step 7. A repaid loan cannot
demonstrate an active-debt restriction.

## Running the scenarios

```
scripts/demo-repay.sh     # steps 3, 4, 4b: deploy, hold, close, repay
scripts/demo-monitor.sh   # steps 5b and 8: reassess on a move, then replay
scripts/demo-default.sh   # step 7: deploy, crash, unwind, default
```

`demo-repay.sh` leaves its last loan open and deployed, which is the live
position steps 5b and 6 need.

**Step 5b is valid only with `trigger=price_move`.** A staleness cycle
shows only that a clock ticks. The log must show the loop go quiet after
a cycle lands, then wake on the move:

```
trigger=staleness  permission=Blocked tx=0x05f79d21...   <- reference set
                                                            (quiet)
trigger=price_move permission=Blocked tx=0xe5e59833...   <- the move
```

`permission=Blocked` is the correct outcome: the asset fell 20%, so
permission is withdrawn. The decision also changes for the right reason.
Before the move the retained assessments read `MonitoringAllowed` /
`Eligible`; after it they read `MonitoringBlocked` /
`MonitoringCoverageBreach`. That change happens only because positions are
marked at the observed price. With a declared constant price the
scheduler still fires and the engine re-runs, but every answer is the
same.

**Step 8 runs at the end of the same script.** The last retained snapshot
and assessment are replayed, then replayed again with one digit of the
position's mark altered. A `MATCH` counts as evidence only if the
tampered run produces a `DIFFERENCE`, and the script fails if it does not.

**Agent equity is the first loss layer.** Lenders lose only once a fall
exceeds the agent's own capital:

| Asset falls to | Book (14,000) unwinds to | Who bears it | `realized_loss` |
|---|---|---|---|
| 50% (`CRASH_BPS=5000`) | 7,000 | Agent equity absorbs all of it | 0; lenders recover principal plus interest |
| 10% (`CRASH_BPS=1000`) | 1,400 | Equity wiped, then lenders | 2,600,000,000 |

`Scenario.s.sol` sets `AGENT_EQUITY` to 10,000e6, so the book is 10,000
of equity plus 4,000 borrowed, and the loss is 4,000 lent less 1,400
recovered. The 50% case is the control: if it also produced a lender
loss, the scenario would be manufacturing one. Both rows come from the
same script with one input changed.

The whole book must be deployed, not only the borrowed part. Equity left
in settlement would absorb a crash from the sidelines, and no fall would
ever reach lenders.
