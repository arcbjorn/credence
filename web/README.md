# `web` — the interface

The eight screens `docs/09` specifies, all built. The optional note view
is not, and `docs/09` marks it optional.

| Screen | `docs/09` beat |
|---|---|
| Agent overview | 4b — the limit rises, the APR falls, the constraint switches |
| Three agents | 1 and 5 — a flawless record denied by its own book |
| Assessment detail | which constraints bound one decision, and why |
| Loan lifecycle | 3, 4, 7 — what was borrowed, and how its forecast scored |
| Evidence | per item: origin, freshness, coverage, content hash |
| Lender | the epoch's ledgers, kept separate |
| Enforcement | 6 — the prohibited withdrawal, reverted on chain |
| Replay | 8 — does retained input reproduce the assessment? |

Beat 6 is the one `docs/09` calls **"the single most convincing moment"**.
The screen renders what `credence-verify` recorded against the live
deployment, and shows the two refusals side by side with their raw
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
figures `docs/11` publishes for the demo's third loan, and that a tampered
snapshot produces a `DIFFERENCE`. `risk-core`'s
`demo_third_loan_binds_on_stress_capacity` pins the same figures natively.
If the two ever disagree, one has drifted.

The artifact is ~137KB gzipped and lazily loaded, so the first paint never
waits on it.

## Data comes from the demos, never from the interface

`docs/09`: *"Every displayed number comes from a retained fixture or an
actual transaction-derived record."*

`scripts/build-web-data.sh` reads the SQLite the demo scripts retained and
emits `public/data/*.json`. That makes the rule structural rather than a
matter of discipline: the demo produced the file, or the screen has
nothing to render. There is no path where the interface computes a figure
the engine never returned.

It also means the build is a **static folder** — no server to run during
judging, nothing to fail.

## The design

The credit body is the interface: a persistent globe on the landing page
moves through shell, arcs, core, section, moons, eclipse and atlas views.
The Observatory, Atlas, Moons, Body and Log rooms use the same renderer.
The field guide explains the laws and links to the original evidence screens.

The original paper, graphite, teal, moss and vermilion palette is preserved,
including a remembered night theme. Space Grotesk carries the world,
Instrument Serif the personal views, and IBM Plex Mono the instruments.

`brand/physics.md` at the repository root is the canonical specification.
`World.tsx` renders the shell on canvas with a capped pixel density, pauses
when offscreen, and honours reduced motion. Interactive nodes remain native
HTML buttons. Docks support Escape and restore focus; all room controls are
keyboard accessible. Hold Alt for whisper mode.

The new visualizations do not invent live network activity. The landing and
Observatory draw the retained repayment run; Atlas draws the independent
comparison scenarios; Log draws the independent indexed records. The score
barometer explicitly preserves the engine's native 300–900 scale. The
history scrubber changes the origination being inspected; it does not claim
to reconstruct historical vault balances.

## Mode labels

`docs/09` forbids collapsing environment and evidence modes into one green
indicator. `src/lib/modes.ts` gives each its own label, token and
treatment, and `modes.test.ts` asserts there is no shared "ok" token any
component could fall back to, and that an unrecognized origin renders
verbatim rather than defaulting to `Observed`.

## Running it

`src/engine/` and `public/data/` are **generated and gitignored**, and the
data comes from actual demo runs. One command does the whole path:

```sh
scripts/web.sh        # from the repository root; or: make web
```

It checks the toolchain, starts a chain if none is listening, runs the
demos, compiles the engine, bundles what they retained, and serves the
app. It reuses evidence that already exists, so a second run skips
straight to the server. `--fresh` re-runs the demos; `--no-serve` builds
the static site instead.

By hand, the same steps in order:

```sh
cargo install wasm-bindgen-cli --version 0.2.128   # once
rustup target add wasm32-unknown-unknown           # once

anvil --port 8546 &
LOANS=3 scripts/demo-repay.sh
scripts/demo-monitor.sh

cd web
pnpm install
pnpm wasm     # compile risk-core to WebAssembly
pnpm data     # bundle the demos' retained SQLite
pnpm dev
```

`pnpm wasm` needs the toolchain once:

```sh
cargo install wasm-bindgen-cli --version 0.2.128
rustup target add wasm32-unknown-unknown
```

`pnpm data` reads `/tmp/credence-{repay,monitor}` — run the demos
first, or point `REPAY_DB` / `MONITOR_DB` elsewhere.

```sh
pnpm test         # vitest, including the engine agreement tests
pnpm build       # -> dist/, a static folder
```

## Stack

React 19, Vite 8, TypeScript 7, React Router 7, Vitest 5. `viem` is
present for direct chain reads; the screens built so far read the
generated bundle instead.

`src/engine/` and `public/data/` are generated and gitignored — committing
either would create a second copy of the engine or the evidence that could
fall behind the first.
