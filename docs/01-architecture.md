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

**Go's substantive job is the simulation clock** — compressing months of
agent history into weeks — plus provider fan-out and the reassessment
scheduler. **Rust's purity is a scheduling advantage**: golden-fixture
testable offline while contracts are still moving.

> **Corrected 14 September.** As built, Go's substantive job turned out to
> be the *data plane* — evidence acquisition, canonical snapshots, the
> assessment API and the reassessment scheduler — with the simulation
> clock one package inside it (`gateway/sim`). The sentence above ranked
> them the other way round, and a reader taking it at face value would
> look for the project's weight in the wrong place.

## Stack

This is the stack as **planned**. The right-hand column records what is
actually in the tree on 14 September, because a stack table that lists
dependencies the repository does not have is a reader's first wrong turn.

| Layer | Planned | As built, 14 Sep |
|---|---|---|
| Chain | Monad | Monad testnet, chain 10143 |
| Contracts | Solidity + Foundry, OpenZeppelin (pin after first green build) | Solidity + Foundry. **No OpenZeppelin** — `forge-std` is the only submodule; the contracts implement their own access control and reentrancy discipline, which `docs/05` specifies and tests |
| Risk engine | Rust, `rust_decimal` or integer base units | Rust, **integer base units only**. No `rust_decimal`: the engine must hash byte-identically against Go and Solidity, and a decimal type with its own rounding would break that |
| Data plane | Go, SQLite/WAL | As planned |
| Internal transport | HTTP/JSON baseline; isolated protobuf/gRPC gate on 6 Oct | **Both.** HTTP/JSON is the default path; gRPC over tonic starts alongside it when `RISK_SERVICE_GRPC_ADDR` is set. `scripts/verify-grpc.sh` proves they return **byte-identical** assessments for the same snapshot, which is the gate (D35). Stubs generated from `schemas/proto/risk.proto` on both ends |
| Indexer | Envio | **Built** (`indexer/`). HyperIndex over HyperSync on 10143, RPC on anvil. It reconstructs the loan lifecycle from the event log, which is a different question from the one the gateway answers: the gateway reads loan **structs** over `eth_call` — current state — and a struct cannot say when recovery opened or what was collected on the way. An earlier version of this row said the gateway "follows contract events directly". It does not; it reads state. |
| Frontend | TypeScript + Next.js, viem/wagmi | **React 19 + Vite 8 + TypeScript 7**, not Next.js. All eight of `docs/09`'s screens are built, plus eight beyond that list: an indexed-lifecycle ninth over the Envio rows, a tenth showing observed Perpl market data driving the three stress scenarios, an eleventh that is the way in, a twelfth serving the four observatory rooms from one component, a thirteenth field guide to the physics, a fourteenth borrow desk running the engine on a request the reader composes, a fifteenth plotting what a record earns, and a sixteenth reading the live deployment -- `docs/09` specifies the screens and a nine-beat script, and the interface had the screens without the script. Next.js was dropped deliberately: its value is SSR, routing and data fetching, and this interface renders a generated static bundle |
| Deployment | Docker Compose onto a small persistent VM | **Not built.** Everything runs from `scripts/`, against anvil or Monad testnet |

**On transport — resolved, see D35.** Both transports are built.
HTTP/JSON is the default; gRPC over tonic starts alongside it when
`RISK_SERVICE_GRPC_ADDR` is set, and the demos still run on JSON so the
acceptance matrix is unchanged. The frozen artifacts are byte-identical,
verified.

> **gRPC is built, and it passes.** `scripts/verify-grpc.sh` evaluates
> one retained snapshot over both transports and compares the bytes:
> same sha256, replay MATCHes on both. The Rust server is tonic, the Go
> client is generated by `protoc-gen-go-grpc`, and both come from the
> same proto — so the two ends cannot drift.
>
> An earlier revision of this document argued *against* gRPC on
> dependency grounds and got the figure wrong: tonic takes
> `risk-service` from 31 crates to **77**, not "roughly a hundred". It
> also under-weighted the real argument for the standard — generated
> clients in any language, and a codec far better tested than one
> written here. The bespoke codec it chose instead has been removed.

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

**As built, 14 September:** `risk/`, `gateway/`, `contracts/`,
`schemas/v1/`, `fixtures/` and `deployments/` exist as laid out, plus two
this layout did not anticipate — `cre/` (the CRE workflow) and
`tools/reader/` (standalone readers that import nothing from this
repository). `web/` now exists — React 19 on Vite 8, with all eight of
`docs/09`'s screens built plus eight beyond that list: an
indexed-lifecycle ninth, a Perpl market-risk tenth, a landing eleventh,
a twelfth behind the four observatory rooms, a thirteenth field guide,
and three that let a reader act rather than read -- a fourteenth borrow
desk that prices a request the reader composes, a fifteenth plotting
what a record earns under one controlled run, and a sixteenth that
reads the live deployment; the demo is still the scripts in
`scripts/`, and the interface reads what they retained rather than
driving them. One planned path does not exist: `agent/`, whose
deterministic runner lives in `gateway/sim` instead because only the
simulation harness drives it and there is no model-backed runner.

Use conventional commits, one granular commit per goal, with a 50-character subject limit.
