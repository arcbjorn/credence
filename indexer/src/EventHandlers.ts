/**
 * Loan lifecycle handlers.
 *
 * Two rows are written for every event: an immutable `LoanEvent` holding
 * exactly what the log carried, and an updated `Loan` summarising the
 * lifecycle so far. The summary is always a fold over the events, never
 * a read of contract state -- an eth_call here would make the indexer
 * agree with the chain because it asked the chain, which is the same
 * circularity `tools/reader` avoids by not importing the gateway's
 * encoder.
 *
 * The fold itself lives in `lifecycle.ts`, which is pure and tested
 * without a chain. What remains here is I/O: read the row, apply one
 * transition, write it back.
 */
import { indexer, type Loan, type LoanEvent } from "envio";
import * as lifecycle from "./lifecycle.js";

/** `<chainId>-<loanId>`: loan ids restart at 1 per deployment. */
const loanKey = (chainId: number, loanId: bigint): string =>
  `${chainId}-${loanId}`;

/**
 * `<chainId>-<block>-<logIndex>`, unique and block-ordered.
 *
 * A transaction hash alone would collide: `collect` emits `Collected`
 * and can emit `Recovered` in the same transaction.
 */
const eventKey = (
  chainId: number,
  blockNumber: number,
  logIndex: number,
): string => `${chainId}-${blockNumber}-${logIndex}`;

/** The event fields every handler records identically. */
type LifecycleEvent = {
  chainId: number;
  block: { number: number; timestamp: number };
  logIndex: number;
  transaction: { hash: string };
};

type EventPayload = {
  principal?: bigint;
  interest?: bigint;
  principalLoss?: bigint;
  unpaidInterest?: bigint;
};

/**
 * Record the raw log row.
 *
 * Written before the summary is touched, and never conditional on the
 * `Loan` existing: `docs/11` requires replay after a cursor rollback, and
 * a summary that cannot be rebuilt from its inputs is a claim rather than
 * evidence. These rows are the inputs.
 */
const recordEvent = (
  context: { LoanEvent: { set: (entity: LoanEvent) => void } },
  event: LifecycleEvent,
  loanId: bigint,
  kind: string,
  payload: EventPayload = {},
): void => {
  context.LoanEvent.set({
    id: eventKey(event.chainId, event.block.number, event.logIndex),
    loan_id: loanKey(event.chainId, loanId),
    chainId: event.chainId,
    kind,
    blockNumber: BigInt(event.block.number),
    blockTimestamp: BigInt(event.block.timestamp),
    logIndex: event.logIndex,
    txHash: event.transaction.hash,
    principal: payload.principal,
    interest: payload.interest,
    principalLoss: payload.principalLoss,
    unpaidInterest: payload.unpaidInterest,
  });
};

indexer.onEvent(
  { contract: "LoanManager", event: "Originated" },
  async ({ event, context }) => {
    const { loanId, account, principal } = event.params;
    recordEvent(context, event, loanId, "Originated", { principal });

    context.Loan.set({
      id: loanKey(event.chainId, loanId),
      chainId: event.chainId,
      loanId,
      account,
      ...lifecycle.originated(principal, BigInt(event.block.timestamp)),
    });
  },
);

indexer.onEvent(
  { contract: "LoanManager", event: "Repaid" },
  async ({ event, context }) => {
    const { loanId, principal, interest } = event.params;
    recordEvent(context, event, loanId, "Repaid", { principal, interest });

    const loan = await context.Loan.get(loanKey(event.chainId, loanId));
    // An event for a loan that was never originated means the indexer
    // started after origination. Dropping the summary update is right:
    // inventing a Loan row here would report a loan with no principal and
    // no origination time as though it were complete. The LoanEvent row
    // above is still written, so nothing from the log is lost.
    if (loan === undefined) return;

    context.Loan.set({
      ...loan,
      ...lifecycle.repaid(
        loan,
        principal,
        interest,
        BigInt(event.block.timestamp),
      ),
    } satisfies Loan);
  },
);

indexer.onEvent(
  { contract: "LoanManager", event: "RecoveryOpened" },
  async ({ event, context }) => {
    const { loanId, at } = event.params;
    recordEvent(context, event, loanId, "RecoveryOpened");

    const loan = await context.Loan.get(loanKey(event.chainId, loanId));
    if (loan === undefined) return;

    // The contract's own `at`, not the block timestamp: they are the same
    // value today, and taking the contract's keeps this row correct if
    // recovery is ever opened for a time other than now.
    context.Loan.set({
      ...loan,
      ...lifecycle.recoveryOpened(loan, at),
    } satisfies Loan);
  },
);

indexer.onEvent(
  { contract: "LoanManager", event: "Collected" },
  async ({ event, context }) => {
    const { loanId, principal, interest } = event.params;
    recordEvent(context, event, loanId, "Collected", { principal, interest });

    const loan = await context.Loan.get(loanKey(event.chainId, loanId));
    if (loan === undefined) return;

    context.Loan.set({
      ...loan,
      ...lifecycle.collected(loan, principal, interest),
    } satisfies Loan);
  },
);

indexer.onEvent(
  { contract: "LoanManager", event: "Recovered" },
  async ({ event, context }) => {
    const { loanId } = event.params;
    recordEvent(context, event, loanId, "Recovered");

    const loan = await context.Loan.get(loanKey(event.chainId, loanId));
    if (loan === undefined) return;

    context.Loan.set({
      ...loan,
      ...lifecycle.recovered(loan, BigInt(event.block.timestamp)),
    } satisfies Loan);
  },
);

indexer.onEvent(
  { contract: "LoanManager", event: "Defaulted" },
  async ({ event, context }) => {
    const { loanId, principalLoss, unpaidInterest } = event.params;
    recordEvent(context, event, loanId, "Defaulted", {
      principalLoss,
      unpaidInterest,
    });

    const loan = await context.Loan.get(loanKey(event.chainId, loanId));
    if (loan === undefined) return;

    context.Loan.set({
      ...loan,
      ...lifecycle.defaulted(
        loan,
        principalLoss,
        unpaidInterest,
        BigInt(event.block.timestamp),
      ),
    } satisfies Loan);
  },
);
