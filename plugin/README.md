# Credence MetaMask plugin

Credit for a MetaMask agent: `health`, `loan`, `borrow`, `repay`.

The agent borrows against its own demonstrated behaviour.

## What it cannot do

It is a client. Every command becomes a transaction the contracts judge:

- `borrow` sends `originate(binding, principal)`. `LoanManager` reads
  the oracle's quote for that exact binding and reverts if there is
  none, if the facility caps are exceeded, or if a loan is already open.
- `repay` sends `repay(loanId)`, which reverts on a loan that is not the
  caller's, already settled, or in recovery.

Neither the plugin nor the API behind it can grant credit. A convenience
layer that could bypass on-chain authorization would refute this
project's central claim rather than package it.

Verified rather than asserted: a repay from a key holding no settlement
tokens came back `publish_failed: execution reverted: custom error
0x90b8ec18` — `TransferFailed()`. The chain refused and every layer said
so.

## Running it

```sh
# The API it talks to.
CREDENCE_AGENT_KEY=<32 hex bytes> \
  go run ./gateway/cmd/credence-productd \
  -manager <loan manager> -account <credit account>

# Then, from a host that loads the plugin:
npm install && npm run build
```

`CREDENCE_API_TOKEN` sets a bearer token. The daemon **refuses to start
without one** on anything but loopback: an open borrow endpoint is a
button anyone can press.

## Tests

```sh
npm test        # 9 tests, no network
npm run typecheck
```
