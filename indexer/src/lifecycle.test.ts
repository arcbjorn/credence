/**
 * The lifecycle fold.
 *
 * These assert the properties `docs/05` states about loan accounting,
 * against the fold rather than against a chain. A test that replayed
 * events through a live indexer and compared the result to an eth_call
 * would prove the chain agrees with itself; these prove the fold is the
 * arithmetic the documents specify.
 */
import { describe, expect, it } from "vitest";
import {
  Status,
  collected,
  defaulted,
  originated,
  recovered,
  recoveryOpened,
  repaid,
} from "./lifecycle.js";

const PRINCIPAL = 4_000_000_000n;
const ORIGINATED_AT = 1_000n;

const openLoan = () => originated(PRINCIPAL, ORIGINATED_AT);

describe("origination", () => {
  it("starts active with nothing repaid and nothing lost", () => {
    const loan = openLoan();
    expect(loan.status).toBe(Status.Active);
    expect(loan.principal).toBe(PRINCIPAL);
    expect(loan.principalRepaid).toBe(0n);
    expect(loan.interestCollected).toBe(0n);
    expect(loan.principalLoss).toBe(0n);
  });

  it("leaves every terminal field unset", () => {
    const loan = openLoan();
    expect(loan.resolvedAt).toBeUndefined();
    expect(loan.heldSeconds).toBeUndefined();
    expect(loan.recoveryOpenedAt).toBeUndefined();
  });
});

describe("repayment", () => {
  it("records principal, interest and the held span", () => {
    const loan = repaid(openLoan(), PRINCIPAL, 12_860_305n, 433_000n);
    expect(loan.status).toBe(Status.Repaid);
    expect(loan.principalRepaid).toBe(PRINCIPAL);
    expect(loan.interestCollected).toBe(12_860_305n);
    // Origination to resolution, which is the number `docs/06` publishes
    // and the one a struct read cannot reconstruct afterwards.
    expect(loan.heldSeconds).toBe(433_000n - ORIGINATED_AT);
  });
});

describe("recovery", () => {
  it("takes the contract's own timestamp, not the block's", () => {
    // They are the same value today. Reading the contract's keeps the row
    // correct if recovery is ever opened for a time other than now.
    const loan = recoveryOpened(openLoan(), 2_500n);
    expect(loan.status).toBe(Status.RecoveryPending);
    expect(loan.recoveryOpenedAt).toBe(2_500n);
  });

  it("accumulates repeated collections rather than overwriting", () => {
    // The sharp one. `docs/05` forbids double-counting collections, and
    // assigning instead of adding would silently keep only the last --
    // reading as a smaller recovery and a larger loss. Three partial
    // collections must sum.
    let loan = recoveryOpened(openLoan(), 2_000n);
    loan = collected(loan, 1_000_000_000n, 500n);
    loan = collected(loan, 1_500_000_000n, 250n);
    loan = collected(loan, 500_000_000n, 125n);

    expect(loan.principalRepaid).toBe(3_000_000_000n);
    expect(loan.interestCollected).toBe(875n);
  });

  it("stays pending while collecting", () => {
    // A collection is not a terminal event: recovery closes on Recovered
    // or Defaulted, never on having collected something.
    const loan = collected(recoveryOpened(openLoan(), 2_000n), 1n, 1n);
    expect(loan.status).toBe(Status.RecoveryPending);
    expect(loan.resolvedAt).toBeUndefined();
  });

  it("closes on Recovered, keeping what was collected", () => {
    let loan = recoveryOpened(openLoan(), 2_000n);
    loan = collected(loan, 3_000_000_000n, 800n);
    loan = recovered(loan, 3_000n);

    expect(loan.status).toBe(Status.Recovered);
    expect(loan.principalRepaid).toBe(3_000_000_000n);
    expect(loan.resolvedAt).toBe(3_000n);
    expect(loan.heldSeconds).toBe(3_000n - ORIGINATED_AT);
  });
});

describe("default", () => {
  it("records the write-off without discarding what was collected", () => {
    // `docs/05`: realized proceeds, principal loss and unpaid interest
    // must reconcile. A partial collection followed by a default has to
    // keep both numbers -- the loss is what remained, not the whole
    // principal.
    let loan = recoveryOpened(openLoan(), 2_000n);
    loan = collected(loan, 1_000_000_000n, 200n);
    loan = defaulted(loan, 3_000_000_000n, 5_000n, 4_000n);

    expect(loan.status).toBe(Status.Defaulted);
    expect(loan.principalRepaid).toBe(1_000_000_000n);
    expect(loan.principalLoss).toBe(3_000_000_000n);
    expect(loan.unpaidInterest).toBe(5_000n);
    // Collected plus written off accounts for the whole principal.
    expect(loan.principalRepaid + loan.principalLoss).toBe(PRINCIPAL);
  });

  it("is terminal and timed", () => {
    const loan = defaulted(openLoan(), PRINCIPAL, 0n, 5_000n);
    expect(loan.resolvedAt).toBe(5_000n);
    expect(loan.heldSeconds).toBe(5_000n - ORIGINATED_AT);
  });
});

describe("a final collection and recovery in one transaction", () => {
  it("keeps both, which is why LoanEvent keys on logIndex", () => {
    // `collect` emits `Collected` and then, when the debt reaches zero,
    // `Recovered` -- both in the same transaction. Keying the event rows
    // on a transaction hash would collide and drop one; they key on
    // `<chain>-<block>-<logIndex>` instead. The fold has to survive the
    // pair arriving back to back.
    let loan = recoveryOpened(openLoan(), 2_000n);
    loan = collected(loan, PRINCIPAL, 900n);
    loan = recovered(loan, 2_400n);

    expect(loan.status).toBe(Status.Recovered);
    expect(loan.principalRepaid).toBe(PRINCIPAL);
    expect(loan.interestCollected).toBe(900n);
    expect(loan.resolvedAt).toBe(2_400n);
  });
});

describe("the fold is a fold", () => {
  it("never mutates the row it is given", () => {
    // Every handler reads a row, applies a transition and writes it back.
    // A transition that mutated in place would corrupt the row Envio
    // holds for rollback, which is exactly what the reorg row depends on.
    const before = openLoan();
    const snapshot = { ...before };
    repaid(before, PRINCIPAL, 1n, 2_000n);
    expect(before).toEqual(snapshot);
  });
});
