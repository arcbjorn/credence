# Contributing

## Build

Run `scripts/setup.sh`, then `scripts/web.sh` for the interface.

## Test

Run `forge test` in `contracts/`, `cargo test -p risk-core` in `risk/`,
`go test -race ./...` in `gateway/`, `pnpm test` in `web/` and `indexer/`,
and `bash scripts/check-docs.sh`.

## Release

Run `scripts/release-check.sh` before merging. The release rules are in
`docs/10-release-process.md`.

## Commits

Use conventional commits. Make one commit per goal and keep the subject at
most 50 characters.
