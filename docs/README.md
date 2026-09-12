# Credence — Documentation

Autonomous credit markets for AI agents. Metropolis (Monad), Onchain Finance & Trading.
**Deadline 13 October 2026 · internal target 12 October.**

The product claim:

> An agent earns access to more capital inside an account whose execution and repayment rules Credence enforces.

---

## Read in this order

| # | Document | Answers |
|---|---|---|
| 00 | [Overview](00-overview.md) | What is this, what ships, what does not |
| 01 | [Architecture](01-architecture.md) | How the pieces fit and why the seam is where it is |
| 02 | [Credit Accounts](02-credit-accounts.md) | How borrowed capital is controlled after disbursement |
| 03 | [Underwriting](03-underwriting.md) | How a decision is made, priced, and explained |
| 04 | [Evidence Contract](04-evidence-contract.md) | Schemas, provenance, determinism, replay |
| 05 | [Contracts](05-contracts.md) | Onchain specification and invariants |
| 06 | [Identity & Forecasts](06-identity-forecasts.md) | Continuity, anti-farming, forecast scoring |
| 07 | [Simulation](07-simulation.md) | Virtual clock, scenarios, the agent runner |
| 08 | [Integrations](08-integrations.md) | Nine integrations, capability gates, failure behavior |
| 09 | [Demo](09-demo.md) | Screens and the demonstration script |
| 10 | [Schedule](10-schedule.md) | 32 days, milestones, fallbacks |
| 11 | [Verification](11-verification.md) | Tests, invariants, release acceptance |
| 12 | [Claims & Roadmap](12-claims-roadmap.md) | What may be claimed; the bridge to the vision |
| 13 | [Decision Log](13-decision-log.md) | What changed from the vision, and why |

Diagrams: [`diagrams/`](diagrams/) — architecture, loan lifecycle, underwriting order.

Source documents (unchanged, for provenance): the original vision, the build plan,
the execution plan, and the merged master plan in the repository root. The documents
in this directory define the current plan; older source documents are historical references.

---

## The five rules

Everything else follows from these.

1. **Enforceable capital control comes first.** History improves terms *within* limits that custody and stress establish — never instead of them.
2. **The engine is pure.** No I/O, no clock, no entropy. Same input plus the same pinned model and policy yields the same canonical output, byte for byte.
3. **Evidence quality gates authorization.** Invalid or unavailable required evidence yields `Unavailable` with blocked risk permission and no executable terms. Missing optional enrichment is omitted without favorable credit; it is never replaced by synthetic evidence in an observed facility.
4. **Claim only what is demonstrated.** Assumptions are labeled assumptions. Synthetic data is labeled synthetic. A hash commits to data; it does not make the data true.
5. **Scope reductions preserve three outcomes:** accountable decisions, enforced capital rules, visible recovery and losses. Cut integrations before weakening these.
