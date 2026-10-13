# `web` — the interface

| Screen | Route |
|---|---|
| Agent overview | `#/agent` |
| Agent profiles | `#/compare` |
| Assessment detail | `#/assessment` |
| Evidence | `#/evidence` |
| Loan lifecycle | `#/arc` |
| Lender | `#/lender` |
| Replay | `#/replay` |
| Enforcement | `#/enforcement` |
| Borrow desk | `#/borrow` |
| What a record earns | `#/earned` |
| Indexed lifecycle | `#/indexed` |
| Live on Monad | `#/live` |
| Perpl market risk | `#/perpl` |
| Observatory, Atlas, Moons, Body, Log | `#/observatory` to `#/log` |
| Field guide | `#/guide` |

The globe renders agents from two sources, kept apart on purpose. The
**cohort** (`scripts/demo-cohort.sh`) is four agents that borrowed
on chain, each on its own `CreditAccount`; they appear on the
Observatory globe and the landing page. The **profiles**
(`fixtures/demo/comparison.json`) are eight scenarios the engine
assessed but which never touched a chain; they live in the Atlas. A
reader should never have to guess which kind of agent they are looking
at, which is why the two rooms do not mix them.

The enforcement screen renders what `credence-verify` recorded against the
live deployment, and shows the two refusals side by side with their raw
selectors: the owner is refused because debt is open (`0xe9d4b4e2`), a
stranger because they are not the owner (`0x72cb8533`). They must be
DIFFERENT errors — if they matched, the first would only be proving
access control, not lender protection. `enforcement.test.ts` pins that.

## The engine runs in the browser

The replay screen loads the **actual risk engine** — the same Rust
`risk-core` the gateway and CLI use, compiled to WebAssembly — rather than
a reimplementation that could drift. `risk-core` is pure (no I/O, no
clock, no entropy, per `docs/01`), which is what makes this possible at
all.

`src/lib/engine.test.ts` asserts the browser engine reproduces the exact
figures `docs/11` publishes for the scenario's third loan, and that a
tampered snapshot produces a `DIFFERENCE`. `risk-core`'s
`demo_third_loan_binds_on_stress_capacity` pins the same figures natively.
If the two ever disagree, one has drifted.

The artifact is ~137KB gzipped and lazily loaded, so the first paint never
waits on it.

## Data comes from the scenario runs, never from the interface

`docs/09`: *"Every displayed number comes from a retained fixture or a
transaction-derived record."*

`scripts/build-web-data.sh` reads the SQLite the scenario scripts retained
and emits `public/data/*.json`. That makes the rule structural rather than
a matter of discipline: a scenario run produced the file, or the screen
has nothing to render. There is no path where the interface computes a
figure the engine never returned.

It also means the build is a **static folder**, with no server to run.

## The design

The credit body is the interface: a persistent globe on the landing page
moves through shell, arcs, core, section, moons, eclipse and atlas views.
The Observatory, Atlas, Moons, Body and Log rooms use the same renderer.
The field guide explains the laws and links to the original evidence screens. The same five destinations appear in the header
on every page. Each record belongs to a room, keeps that room active in the
header, and provides a breadcrumb back. `src/lib/navigation.ts` defines the
shared titles and relationships. Room introductions expose their records;
detailed pages offer related records after the content. Globe/Map controls
change projection without changing rooms.

The original paper, graphite, teal, moss and vermilion palette is preserved,
including a remembered night theme. Space Grotesk carries the world,
Instrument Serif the personal views, and IBM Plex Mono the instruments.

`brand/physics.md` at the repository root is the canonical specification.
`World.tsx` renders the shell on canvas with a capped pixel density, pauses
when offscreen, and honours reduced motion. Interactive nodes remain native
HTML buttons. Docks support Escape and restore focus; all room controls are
keyboard accessible. Hold Alt for whisper mode.

The visualizations do not invent live network activity. The landing
and Observatory draw the retained repayment run and the cohort that
borrowed beside it; Atlas draws the independent comparison scenarios; Log
draws the independent indexed records. The score
barometer explicitly preserves the engine's native 300–900 scale. The
history scrubber changes the origination being inspected; it does not claim
to reconstruct historical vault balances.

## Mode labels

`docs/09` forbids collapsing environment and evidence modes into one
status indicator. `src/lib/modes.ts` gives each its own label, token and
treatment, and `modes.test.ts` asserts there is no shared "ok" token any
component could fall back to, and that an unrecognized origin renders
verbatim rather than defaulting to `Observed`.

## Running it

`src/engine/` and `public/data/` are **generated and gitignored**, and the
data comes from actual scenario runs. One command does the whole path:

```sh
scripts/web.sh        # from the repository root; or: make web
```

It checks the toolchain, starts a chain if none is listening, runs the
scenario scripts, compiles the engine, bundles what they retained, and
serves the app. It reuses evidence that already exists, so a second run
skips straight to the server. `--fresh` re-runs the scenarios;
`--no-serve` builds the static site instead.

By hand, the same steps in order:

```sh
cargo install wasm-bindgen-cli --version 0.2.128   # once
rustup target add wasm32-unknown-unknown           # once

anvil --port 8546 &
LOANS=5 scripts/demo-repay.sh
scripts/demo-monitor.sh
scripts/demo-growth.sh
scripts/demo-cohort.sh

cd web
pnpm install
pnpm wasm     # compile risk-core to WebAssembly
pnpm data     # bundle the scenarios' retained SQLite
pnpm dev
```

`pnpm wasm` needs the toolchain once:

```sh
cargo install wasm-bindgen-cli --version 0.2.128
rustup target add wasm32-unknown-unknown
```

`pnpm data` reads `/tmp/credence-{repay,monitor,default,growth,cohort}`
— run the scenario scripts first, or point `REPAY_DB` / `MONITOR_DB`
elsewhere.

```sh
pnpm test         # vitest, including the engine agreement tests
pnpm build       # -> dist/, a static folder
```

## Stack

React 19, Vite 8, TypeScript 7, React Router 7, Vitest 5. `viem` reads
Monad testnet directly on the **Live** record, which is the only screen
that touches the network; every other one reads the generated bundle and
works offline.

`src/engine/` and `public/data/` are generated and gitignored — committing
either would create a second copy of the engine or the evidence that could
fall behind the first.
