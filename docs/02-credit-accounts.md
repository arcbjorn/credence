# 02 · Credit Accounts

The foundation. Week one's work. Everything else assumes this exists.

## The attack this closes

An operator spins up several agents, builds cheap repayment histories, borrows the maximum, and abandons them. The protocol records the defaults perfectly. **Lenders still lose.** Reputation alone cannot solve this: the attack is profitable even when identities persist.

The corollary that reorders the whole plan:

> **Lowering a credit limit recovers nothing.** It stops future borrowing. The capital already disbursed is untouched.

## Mechanism

Borrowed funds never reach the agent's wallet.

```text
Lender capital ──► CreditVault ──► CreditAccount ◄── agent equity
                                         │
                                   [ policy gate ]
                                         │
                              approved adapters only
                                         │
                                   strategy venue
```

### Invariants — contract-enforced

1. **Whitelisted adapters only.** Registered adapters, fixed selectors. No arbitrary `call` or `delegatecall`.
2. **No withdrawal while debt is active or recovering.** Binds the **owner**, not only the agent. This is the load-bearing rule and the one the demo shows failing.
3. **Health maintained.** Any adapter call that would breach coverage reverts.
4. **Proceeds return to the account.** Adapters cannot redirect output.
5. **Close-out ordering.** Principal, then accrued interest, then residual equity to the bound recipient.

Initial funding requires agent equity. **An agent with no equity receives no credit in v0.**

Precedent worth reading first: Gearbox's credit-account model — pools funding restricted accounts with approved execution routes and solvency checks.

## Coverage — one definition

```text
coverage = recoverable_account_value / (principal_outstanding + accrued_interest)
```

Two distinct uses, never conflated:

| Measure | Prices used | Purpose |
|---|---|---|
| **Marked maintenance coverage** | Current | Health, recovery eligibility |
| **Stressed origination coverage** | Shocked | New-draw capacity |

Closeout costs are **already deducted** from the numerator in both. An offchain stress scenario is not an executable liquidation price.

## Failure path

![Loan lifecycle](diagrams/loan-lifecycle.svg)

| Trigger | Action |
|---|---|
| Coverage < 1.20 | New borrowing blocked |
| Coverage < 1.15 | Warning; no risk-increasing action |
| Coverage < 1.10 | Recovery eligible after reserving bounded closeout cost |
| Maturity passed | Recovery triggers immediately; no discretionary grace |
| Post-unwind shortfall | Loss allocated to vault, recorded against the original binding |

These are **scenario-policy starting values**, to be checked against the actual asset and venue in milestone two. They do not guarantee that a fast move cannot jump below solvency.

A dedicated recovery path must be able to reduce risk in an unhealthy account **even when it cannot restore the normal threshold in one step** — otherwise the health check itself prevents liquidation.

## Loss given default is not bounded by custody

A liquidation path creates a recovery **route**, not a recovery **rate**.

Account: 5,000 equity + 5,000 borrowed, 10,000 deployed. Illustrative calculations, not tested outcomes — execution cost is the named assumption and separates otherwise identical rows.

| Shock | Gross | Cost | Net | Principal loss | Realized LGD |
|---|---|---|---|---|---|
| 40% | 6,000 | 80 | 5,920 | 0 | 0.0% |
| 50% | 5,000 | 60 | 4,940 | 60 | 1.2% |
| 60% | 4,000 | 120 | 3,880 | 1,120 | 22.4% |
| 60% | 4,000 | 60 | 3,940 | 1,060 | 21.2% |
| 75% | 2,500 | 200 | 2,300 | 2,700 | 54.0% |
| 90% | 1,000 | 400 | 600 | 4,400 | 88.0% |

The two 60% rows are the **same shock under different cost assumptions.** Cost dominates LGD at moderate shocks. LGD is bounded by the **equity cushion and execution quality** — gaps, bad execution, stale prices, and vanished liquidity all push it higher.

**Any LGD figure shown is a named scenario assumption, never a guarantee.** [07 · Simulation](07-simulation.md) measures what the implemented collection path actually realizes, including near-zero recovery.

## Worked loss path

5,000 equity, 5,000 borrowed at 12% for ten days, 10,000 deployed. Full-term interest ≈ 16.44, debt ≈ 5,016.44.

A shock at maturity cuts realizable proceeds to 4,000. Venue cost 40, bounded recovery fee 20 → net collection 3,940. Principal loss 1,060; 16.44 interest uncollected; total shortfall ≈ 1,076.44. **Realized LGD 21.2%.**

The agent receives no residual equity. The ledger records principal loss separately from unpaid interest.

**This exceeds the equity cushion and demonstrates that enforced custody cannot eliminate market losses.**
