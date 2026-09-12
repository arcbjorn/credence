# 01 · Architecture

![Architecture](diagrams/architecture.svg)

## The seam

One boundary carries the load:

```text
RiskInput ──► [ pure fn, no I/O, pinned model ] ──► RiskAssessment
```

Every integration either **produces a field in `RiskInput`** or **consumes a field in `RiskAssessment`**. None sits on the critical path. An integration that fails degrades a field's provenance from `Live` to `Simulated`; it does not stop the system.

## Dependency order

```text
Versioned evidence + policy + explicit time
                    ↓
              Rust risk-core
                    ↓
             RiskAssessment
                    ↓
          eligible + authorized?
                    ↓
       LoanManager + CreditAccount
                    ↓
  execution · repayment · recovery · realized loss
                    ↓
        authoritative lending events
                    ↓
             next assessment
```

Provider APIs enrich evidence. CRE can validate and deliver an assessment. Kuru can expose a market signal. **None replaces the contract's independent accounting and execution checks.**

## Ownership

| Component | Owns | Does not own |
|---|---|---|
| Rust `risk-core` | Validation, valuation, eligibility, limits, pricing, stress, explanations | Network, signing, persistence, wall-clock |
| Rust `risk-service` | HTTP boundary, schema validation, resource limits | Database state, provider orchestration |
| Go `gateway` | Evidence acquisition, canonical snapshots, SQLite, contract events, product API, replay, **reassessment scheduling** | Discretionary changes to engine output |
| Go `simulator` | Virtual clock, ordered events, independent outcome generation | Pretending virtual age is onchain age |
| Agent runner | Observe, propose, commit forecast, request or decline, execute, repay | Risk approvals, arbitrary custody access |
| Solidity | Facility state, balances, debt, allowed operations, authorization, loss allocation | Estimating real-world default probability |
| Frontend | Comparison, lifecycle actions, evidence drilldown, lender outcomes | Manufacturing history, silently changing modes |

**Go's substantive job is the simulation clock** — compressing months of agent history into weeks — plus provider fan-out and the reassessment scheduler. **Rust's purity is a scheduling advantage**: golden-fixture testable offline while contracts are still moving.

## Stack

| Layer | Technology |
|---|---|
| Chain | Monad |
| Contracts | Solidity + Foundry, OpenZeppelin (pin after first green build) |
| Risk engine | Rust, `rust_decimal` or integer base units |
| Data plane | Go, SQLite/WAL |
| Internal transport | HTTP/JSON until the 5 Oct freeze, then protobuf/gRPC with Tonic |
| Indexer | Envio |
| Frontend | TypeScript + Next.js, viem/wagmi |
| Deployment | Docker Compose onto a small persistent VM |

**On transport:** the boundary is architectural and permanent; only its wire format changes. JSON while `RiskInput` churns avoids schema-churn tax; protobuf after the freeze buys type safety and speed. Both must pass identical reference vectors. If gRPC is not green by 6 October, ship JSON — the diagram is unchanged either way.

## Repository layout

```text
contracts/                  Solidity, Foundry tests, deploy scripts
risk/crates/risk-core/      Pure Rust library
risk/crates/risk-service/   HTTP service
risk/crates/risk-cli/       Offline assessment and replay
gateway/                    Go API, adapters, SQLite, reconciliation
gateway/sim/                Virtual clock, scenario orchestration
agent/                      Model-backed and deterministic runners
web/                        Agent, lender, evidence, replay interface
schemas/v1/                 JSON schemas, canonicalization vectors
fixtures/                   Versioned inputs, outcomes, expected assessments
scenarios/                  World generators, pinned definitions
deployments/                Chain, contract, token, policy manifests (no secrets)
docs/                       This documentation
```

Signing credentials stay in a restricted backend. Scenario identities and funding keys stay separate from any personal wallet. Record dependency versions, artifacts, addresses, and model, policy, and fixture hashes for the release.

**This directory is not yet a Git repository — initialize during implementation.** Conventional commits, one granular commit per goal, 50-character subject limit.
