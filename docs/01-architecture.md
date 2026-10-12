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
| Go `gateway` | Evidence acquisition, canonical snapshots, SQLite, chain state reads, product API, replay, **reassessment scheduling** | Discretionary changes to engine output |
| Go `simulator` | Virtual clock, ordered events, independent outcome generation | Pretending virtual age is onchain age |
| Agent runner | Observe, propose, commit forecast, request or decline, execute, repay | Risk approvals, arbitrary custody access |
| Solidity | Facility state, balances, debt, allowed operations, authorization, loss allocation | Estimating real-world default probability |
| Frontend | Comparison, lifecycle actions, evidence drilldown, lender outcomes | Manufacturing history, silently changing modes |

**Go's substantive job is the data plane** — evidence acquisition,
canonical snapshots, the assessment API, provider fan-out and the
reassessment scheduler. The simulation clock, which compresses months of
agent history, is one package inside it (`gateway/sim`). **Rust's purity
keeps the engine golden-fixture testable offline**, independent of the
contracts.

## Stack

| Layer | Stack |
|---|---|
| Chain | Monad testnet, chain 10143 |
| Contracts | Solidity + Foundry. **No OpenZeppelin** — `forge-std` is the only submodule; the contracts implement their own access control and reentrancy discipline, which `docs/05` specifies and tests |
| Risk engine | Rust, **integer base units only**. No `rust_decimal`: the engine must hash byte-identically against Go and Solidity, and a decimal type with its own rounding would break that |
| Data plane | Go, SQLite/WAL |
| Internal transport | **HTTP/JSON and gRPC.** HTTP/JSON is the default path; gRPC over tonic starts alongside it when `RISK_SERVICE_GRPC_ADDR` is set. `scripts/verify-grpc.sh` proves they return **byte-identical** assessments for the same snapshot, which is the gate (D35). Stubs generated from `schemas/proto/risk.proto` on both ends |
| Indexer | Envio (`indexer/`). HyperIndex over HyperSync on 10143, RPC on anvil. It reconstructs the loan lifecycle from the event log, which is a different question from the one the gateway answers: the gateway reads loan **structs** over `eth_call` — current state — and a struct cannot say when recovery opened or what was collected on the way. The gateway does not follow contract events directly. |
| Frontend | **React 19 + Vite 8 + TypeScript 7**. All eight of `docs/09`'s screens are built, plus eight beyond that list: an indexed-lifecycle ninth over the Envio rows, a tenth showing observed Perpl market data driving the three stress scenarios, an eleventh that is the way in, a twelfth serving the four observatory rooms from one component, a thirteenth field guide to the physics, a fourteenth borrow desk running the engine on a request the reader composes, a fifteenth plotting what a record earns, and a sixteenth reading the live deployment. No Next.js: its value is SSR, routing and data fetching, and this interface renders a generated static bundle |
| Deployment | No Docker Compose or VM deployment. Everything runs from `scripts/`, against anvil or Monad testnet |

**On transport — see D35.** Both transports are built. HTTP/JSON is the
default; gRPC over tonic starts alongside it when
`RISK_SERVICE_GRPC_ADDR` is set, and the scenario scripts run on JSON.
The boundary is architectural and permanent; only its wire format
changes, and the architecture diagram is the same for both. The gRPC
path shares the JSON path's contracts, schemas, model and fixtures byte
for byte, verified.

> **gRPC passes.** `scripts/verify-grpc.sh` evaluates one retained
> snapshot over both transports and compares the bytes: same sha256,
> replay MATCHes on both. The Rust server is tonic, the Go client is
> generated by `protoc-gen-go-grpc`, and both come from the same proto —
> so the two ends cannot drift.
>
> tonic takes `risk-service` from 31 crates to **77**. In exchange it
> provides generated clients in any language and a codec better tested
> than a bespoke one.

## Implementation layout

```text
contracts/                  Solidity, Foundry tests, deploy scripts
contracts/deployments/      Chain, contract, token, policy manifests (no secrets)
risk/crates/risk-core/      Pure Rust library
risk/crates/risk-service/   HTTP and gRPC service
risk/crates/risk-cli/       Offline assessment and replay
gateway/                    Go API, adapters, SQLite, reconciliation
gateway/sim/                Virtual clock, scenario orchestration, deterministic agent runner
cre/                        CRE workflow
indexer/                    Envio HyperIndex lifecycle indexer
tools/reader/               Standalone readers that import nothing from this repository
web/                        Agent, lender, evidence, replay interface
schemas/v1/                 JSON schemas, canonicalization vectors
fixtures/                   Versioned inputs, outcomes, expected assessments
docs/                       This documentation
```

Signing credentials stay in a restricted backend. Scenario identities and funding keys stay separate from any personal wallet. Record dependency versions, artifacts, addresses, and model, policy, and fixture hashes for the release.

`web/` is React 19 on Vite 8, with all eight of `docs/09`'s screens built plus eight beyond that list: an
indexed-lifecycle ninth, a Perpl market-risk tenth, a landing eleventh,
a twelfth behind the four observatory rooms, a thirteenth field guide,
and three that let a reader act rather than read -- a fourteenth borrow
desk that prices a request the reader composes, a fifteenth plotting
what a record earns under one controlled run, and a sixteenth that
reads the live deployment. The scenario runs are the scripts in
`scripts/`; the interface reads what they retained rather than driving
them. There is no `agent/` directory: the deterministic runner lives in
`gateway/sim` because only the simulation harness drives it, and there
is no model-backed runner.
