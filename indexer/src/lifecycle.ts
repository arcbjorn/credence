/**
 * The lifecycle fold, separated from event registration.
 *
 * These functions are the whole of the indexer's reasoning: what a
 * `Loan` row looks like after each event. They are pure and take no
 * Envio context, so they can be tested without a database, a chain, or
 * the generated runtime -- which matters because the alternative is
 * asserting the summary against the same chain it was derived from.
 *
 * `EventHandlers.ts` does the I/O: read the row, apply one of these,
 * write it back.
 */

/** The subset of a `Loan` row this module reads and writes. */
export type LoanState = {
  status: string;
  principal: bigint;
  principalRepaid: bigint;
  interestCollected: bigint;
  principalLoss: bigint;
  unpaidInterest: bigint;
  originatedAt: bigint;
  recoveryOpenedAt: bigint | undefined;
  resolvedAt: bigint | undefined;
  heldSeconds: bigint | undefined;
};

/** Terminal statuses, matching `docs/05`'s loan states. */
export const Status = {
  Active: "Active",
  RecoveryPending: "RecoveryPending",
  Repaid: "Repaid",
  Recovered: "Recovered",
  Defaulted: "Defaulted",
} as const;

export const originated = (
  principal: bigint,
  blockTimestamp: bigint,
): LoanState => ({
  status: Status.Active,
  principal,
  principalRepaid: 0n,
  interestCollected: 0n,
  principalLoss: 0n,
  unpaidInterest: 0n,
  originatedAt: blockTimestamp,
  recoveryOpenedAt: undefined,
  resolvedAt: undefined,
  heldSeconds: undefined,
});

/**
 * Close a loan.
 *
 * `heldSeconds` is origination to resolution in block time -- the span
 * the agent actually controlled the capital, which `docs/06` publishes
 * as a signal.
 *
 * The contract retains `originatedAt` and `resolvedAt` deliberately, so
 * this particular number IS derivable from a struct read; `credence-
 * publish` derives it that way. Computing it here keeps the row a pure
 * function of the log rather than saving a call.
 */
const resolve = (
  loan: LoanState,
  blockTimestamp: bigint,
  status: string,
): LoanState => ({
  ...loan,
  status,
  resolvedAt: blockTimestamp,
  heldSeconds: blockTimestamp - loan.originatedAt,
});

export const repaid = (
  loan: LoanState,
  principal: bigint,
  interest: bigint,
  blockTimestamp: bigint,
): LoanState =>
  resolve(
    {
      ...loan,
      principalRepaid: loan.principalRepaid + principal,
      interestCollected: loan.interestCollected + interest,
    },
    blockTimestamp,
    Status.Repaid,
  );

export const recoveryOpened = (loan: LoanState, at: bigint): LoanState => ({
  ...loan,
  status: Status.RecoveryPending,
  recoveryOpenedAt: at,
});

/**
 * Accumulate a collection.
 *
 * Recovery can collect repeatedly, and `docs/05` requires collections
 * and losses never to be double-counted. Assigning instead of adding
 * would silently discard every collection but the last -- which reads as
 * a smaller recovery and a larger loss.
 */
export const collected = (
  loan: LoanState,
  principal: bigint,
  interest: bigint,
): LoanState => ({
  ...loan,
  principalRepaid: loan.principalRepaid + principal,
  interestCollected: loan.interestCollected + interest,
});

export const recovered = (loan: LoanState, blockTimestamp: bigint): LoanState =>
  resolve(loan, blockTimestamp, Status.Recovered);

export const defaulted = (
  loan: LoanState,
  principalLoss: bigint,
  unpaidInterest: bigint,
  blockTimestamp: bigint,
): LoanState =>
  resolve(
    { ...loan, principalLoss, unpaidInterest },
    blockTimestamp,
    Status.Defaulted,
  );
