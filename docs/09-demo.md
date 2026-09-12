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

## Rules

> **Every displayed number comes from a retained fixture or an actual transaction-derived record.**

Never edit the frontend to make the expected story appear. If the agent declines an unattractive loan, **demonstrate that decision** and use a separately declared scenario whose economics meet policy.

Live transactions, historical fixtures, and simulated outcomes must be **visually distinguishable at all times.**

## Recordings

A short primary recording and a longer technical walkthrough. Adjust lengths to the verified submission rules.

## Why Monad

Demonstrate observable benefits: onchain action constraints, frequent small account updates, short feedback from transaction to reassessment. Measure request-to-confirmation, event-to-index, and input-to-assessment latency **in the submitted deployment**.

**Do not attribute offchain provider speed to the chain, or cite theoretical throughput.** Report what the prototype actually exercised.
