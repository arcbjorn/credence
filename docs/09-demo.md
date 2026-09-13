# 09 · Demo

## Screens

| Screen | Question answered |
|---|---|
| Agent overview | What can this agent borrow, what does it owe, and why? |
| Three-agent comparison | How do history, exposure, and forecast behavior produce different outcomes? |
| Assessment detail | Which constraints bound this decision, and what evidence supports them? |
| Evidence inspector | Where did this come from, how fresh, how complete, is it synthetic? |
| Loan lifecycle | Where did funds go, what is owed, what happened at repayment or default? |
| Lender / epoch | What cash is available, what principal is exposed, what was realized? |
| Replay | Does retained input reproduce the assessment under the pinned model? |
| Note view *(optional)* | What does this entitle the holder to, where did the price come from? |

Lead with amounts, terms, actions, reasons. Hashes, model versions, and service names go in accessible details. The default flow must not require understanding Go, Rust, or RPCs.

**Always show environment and evidence mode** in plain labels — `Scenario history`, `Observed account state`, `Development attestation`, `Deployed CRE policy checks`. **Never collapse these into an ambiguous green indicator.**

## Script

| Beat | Action | Evidence shown |
|---|---|---|
| 1 | Introduce three accounts and the financing problem | Different histories and exposures, clear scenario labels |
| 2 | Performer proposes an allocation, requested amount, and financing ceiling | Actual agent trace, or explicitly labeled replay; expected benefit after costs on the loan horizon |
| 3 | Commit forecast and request, assess, accept or decline terms, fund if accepted | Commitment reference, exact requested grant, binding reason, transaction, funds in the restricted account |
| 4 | Close strategy and repay | Principal, realized flows, costs, accrued interest, updated history |
| 4b | **Repeat twice more** | Three sequential loans, same policy and model hash; limit rises, APR falls, **binding constraint switches from history to stress capacity** |
| 5 | Assess the concentrated account | Good history, denial caused by **current portfolio limits** |
| 5b | **Unprompted reassessment** | Market moves while a loan is open, no agent request; a monitoring update with no origination terms reaches chain within the declared interval |
| 6 | **Attempt a prohibited withdrawal** | Reverting transaction — lender protection demonstrated, not claimed |
| 7 | Overconfident scenario and recovery | Repeated forecasts and outcomes, keeper action, actual collection, lender loss |
| 8 | Replay a retained assessment | Exact model and input identifiers, verified match |
| 9 | *(optional)* Transfer and redeem a note | Holder claim, disclosed controlled market, redemption |

**Beat 6 is the single most convincing moment** — the difference between claiming lender protection and demonstrating it, in one reverted transaction.

**Beats 4b and 5b carry the thesis.** Without 4b, credit never visibly compounds. Without 5b, "continuous underwriting" is asserted rather than shown.

In beat 4b, the rising limit is `agent_capacity`; show `facility_limit`, debt headroom (`available_credit`), `drawable_capacity`, and requested/granted principal separately. Beat 5b and the withdrawal attempt require an active loan: open the overconfident account's loan before those beats, then continue its outcomes and recovery in beat 7. Do not rely on the performer's already repaid loan to demonstrate an active-debt restriction.

## Driving beats 4, 5b and 7

Both run end to end from a clean chain, so neither is reassembled by hand
on demo day:

```
scripts/demo-repay.sh     # beat 4: deploy, hold, close, repay
scripts/demo-monitor.sh   # beat 5b: price moves, gateway reassesses
scripts/demo-default.sh   # beat 7: deploy, crash, unwind, default
```

`demo-repay.sh` leaves a second loan open and deployed, which is the live
position beats 5b and 6 need.

**Beat 5b must show `trigger=price_move`, not `trigger=staleness`.**
A staleness cycle only proves a clock still ticks; it says nothing about
the gateway reacting to the market. The run is only valid if the log shows
the loop go quiet after a cycle lands, and then wake on the move:

```
trigger=staleness  permission=Blocked tx=0x05f79d21...   <- reference set
                                                            (quiet)
trigger=price_move permission=Blocked tx=0xe5e59833...   <- the move
```

`permission=Blocked` is the correct outcome, not a failure: the asset fell
20%, so withdrawing permission is the right answer. What is demonstrated
is that the reassessment happened unprompted and reached chain.

**Agent equity is the first loss layer, and the demo must respect it.**
Lenders only lose once a fall exceeds the agent's own capital, so the
severity is not cosmetic:

| Asset falls to | Book (9,000) unwinds to | Who bears it | `realized_loss` |
|---|---|---|---|
| 50% (`CRASH_BPS=5000`) | 4,500 | Agent equity absorbs all of it | 0 — lenders recover principal **plus** interest |
| 10% (`CRASH_BPS=1000`) | 900 | Equity wiped, then lenders | 3,100,000,000 |

The mild case is the control: if a 50% crash also produced a lender loss,
the demo would be manufacturing one rather than deriving it. Both figures
come from the same script with one input changed.

The whole book must be deployed, not the borrowed portion alone. Equity
left sitting in settlement absorbs a crash from the sidelines, and no
fall — however severe — would ever reach lenders.

## Rules

> **Every displayed number comes from a retained fixture or an actual transaction-derived record.**

Never edit the frontend to make the expected story appear. If the agent declines an unattractive loan, **demonstrate that decision** and use a separately declared scenario whose economics meet policy.

Live transactions, historical fixtures, and simulated outcomes must be **visually distinguishable at all times.**

## Recordings

A short primary recording and a longer technical walkthrough. Adjust lengths to the verified submission rules.

## Why Monad

Demonstrate observable benefits: onchain action constraints, frequent small account updates, short feedback from transaction to reassessment. Measure request-to-confirmation, event-to-index, and input-to-assessment latency **in the submitted deployment**.

**Do not attribute offchain provider speed to the chain, or cite theoretical throughput.** Report what the prototype actually exercised.
