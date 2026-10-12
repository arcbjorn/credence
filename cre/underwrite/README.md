# `underwrite` — the CRE monitoring workflow

A Chainlink CRE workflow that reassesses open loans on a schedule and
writes the result to `CREReceiver` on Monad testnet. It is the second
authorization route: `docs/05` calls the first one a signed assessment
from the operator, and this one a report delivered through the Keystone
forwarder, where the receiver authenticates the transport rather than a
signature.

## What it does, in one cycle

1. The cron trigger fires on `schedule`.
2. For each configured loan, `RunInNodeMode` asks `credence-assessd` for
   an assessment and aggregates with `ConsensusIdenticalAggregation` —
   every node must produce byte-identical bytes or there is no report.
3. `checkAssessment` refuses a contradictory result before it can become
   a permission: a `Denied` decision claiming `Allowed` permission, or a
   missing class.
4. The result is ABI-encoded as a `MonitoringReport` (13 static words)
   and written to `ReceiverAddress`.

Failure is per loan, not per cycle. A loan whose assessment cannot be
produced leaves its previous permission standing until it expires, and an
expired permission blocks risk increases on its own. Stopping at the first
error would leave later loans unassessed for the same reason, which is
strictly worse.

## What the consensus establishes

The nodes reach consensus on **one reply from one project-controlled HTTP
service**. They do not independently read chain state. The substantive
checks — bounds, evidence commitments, account and facility reads — are
performed by the Rust engine against a gateway-built snapshot, upstream of
anything here.

Two further limits, both structural:

- **Monitoring only.** A monitoring report carries no grant by
  construction, so a CRE-pinned facility can monitor but cannot fund.
  There is no CRE origination path in v0.
- **No DON-signed report has reached the live receiver.** Deploy access
  requires a commercial agreement with Chainlink. `Written: 1` from the
  simulator means the write path executed and was accepted in simulation;
  it is not a transaction.

`docs/08` states the ceiling on the CRE claim and `docs/11` records the
evidence rows.

## Configuration

| Field | Meaning |
|---|---|
| `schedule` | Reassessment cadence. `docs/11` requires an unprompted monitoring update within a declared interval; this is the declaration |
| `riskServiceUrl` | `credence-assessd`, serving `POST /v1/assess` with `{"loan_id": N}`. **Not the engine directly** — the engine is pure and cannot look a loan up by id |
| `chainSelector` | The chain the receiver lives on |
| `receiverAddress` | The deployed `CREReceiver`. Must be a real address: the zero placeholder and unparseable input are both refused at init, because `common.HexToAddress` turns either into the zero address and the workflow would then write into nothing, succeeding every cycle while no permission reached the oracle |
| `gasLimit` | Bounded, so a pathological report cannot spend without limit |
| `validitySeconds` | How long a monitoring permission stands. Comfortably longer than the schedule interval, or permission lapses between runs |

Addresses are configuration rather than code: the same binary underwrites
a scenario deployment and a production one, and a hardcoded address would
make those the same deployment by accident. The deployed receiver is
recorded in `contracts/deployments/10143.json`, which is the single source
of truth for addresses.

## Running it

Simulation needs no approval; only `cre workflow deploy` needs deploy
access.

```sh
credence-assessd &   # the assessment endpoint the workflow asks

cre workflow simulate ./underwrite --target=staging-settings
```

Tests:

```sh
cd cre && go test ./underwrite/
```
