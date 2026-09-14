# `web` — the interface

The eight screens `docs/09` specifies. Three are built: the ones that
carry the thesis.

| Screen | `docs/09` beat |
|---|---|
| Agent overview | 4b — the limit rises, the APR falls, the constraint switches |
| Assessment detail | which constraints bound one decision, and why |
| Replay | 8 — does retained input reproduce the assessment? |

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

## Mode labels

`docs/09` forbids collapsing environment and evidence modes into one green
indicator. `src/lib/modes.ts` gives each its own label, token and
treatment, and `modes.test.ts` asserts there is no shared "ok" token any
component could fall back to, and that an unrecognized origin renders
verbatim rather than defaulting to `Observed`.

## Running it

```sh
pnpm install
pnpm wasm     # compile risk-core to WebAssembly (needs wasm-bindgen-cli)
pnpm data     # generate the bundle from the demos' retained SQLite
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
