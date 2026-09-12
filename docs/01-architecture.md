# 01 · Architecture

![Architecture](diagrams/architecture.svg)

## The seam

One boundary carries the load:

```text
RiskInput ──► [ pure fn, no I/O, pinned model ] ──► RiskAssessment
```

Evidence integrations produce fields in `RiskInput`; authorization and execution integrations consume assessments and generate lending events. **Optional enrichment can be omitted; required evidence and enforcement dependencies cannot.** Required evidence failure produces `Unavailable` and blocks risk-increasing authorization. Optional evidence is omitted with its absence recorded and no clean-history benefit.

An acquisition failure never changes evidence origin from observed to synthetic. The scenario baseline uses separately bound test deployments and retained fixtures; switching to it is an explicit environment change. The release still requires the selected chain and the registry acceptance described in [08 · Integrations](08-integrations.md).

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
| Rust `risk-service` | Service boundary, schema validation, resource limits; HTTP baseline, gated gRPC transport | Database state, provider orchestration |
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
| Internal transport | HTTP/JSON baseline; isolated protobuf/gRPC gate on 6 Oct after the 5 Oct freeze |
| Indexer | Envio |
| Frontend | TypeScript + Next.js, viem/wagmi |
| Deployment | Docker Compose onto a small persistent VM |

**On transport:** the boundary is architectural and permanent; only its wire format changes. Freeze contracts, schemas, model, fixtures, and a working JSON release candidate on 5 October. On 6 October, accept the isolated gRPC transport change only if it passes identical canonicalization vectors and the applicable acceptance matrix without changing those frozen artifacts. Otherwise ship JSON. The architecture diagram is unchanged either way.

## Planned implementation layout

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

The repository currently contains the documentation and historical plans; implementation directories above are planned. Use conventional commits, one granular commit per goal, with a 50-character subject limit.
