/**
 * The state of the book, as the physics needs it.
 *
 * `docs/brand/physics.md` gives the core, the atmosphere and the scar a
 * physical form on the credit body. Each one has to come from a loaded
 * figure, because a shader driven by an invented number is a claim the
 * bundle never made -- and this project spends its credibility on not
 * doing that.
 *
 * All three come from the same retained checks the lender screen renders
 * as a table:
 *
 *   core         vault idle cash
 *   atmosphere   1 - utilization, from outstanding over total
 *   scar         vault realized loss
 *
 * Absent checks produce `null`, never zero. A vault with no idle cash and
 * a vault whose idle cash was not recorded look identical if you collapse
 * them, and the first is a market with a cold core while the second is a
 * missing measurement.
 */

import type { Check, Run } from "./data";

export interface BookState {
  /** Idle cash in base units, as the check recorded it. */
  idle: bigint | null;
  /** Principal outstanding in base units. */
  outstanding: bigint | null;
  /** Realized loss in base units. */
  loss: bigint | null;
  /**
   * Outstanding over total, in [0, 1], or `null` when either side is
   * missing. High utilization is a thin atmosphere.
   */
  utilization: number | null;
}

/** The leading integer of a `"<n> base units"` detail. */
function baseUnits(check: Check | undefined): bigint | null {
  if (!check) return null;
  const head = check.detail.split(" ")[0];
  if (head === undefined || !/^\d+$/.test(head)) return null;
  try {
    return BigInt(head);
  } catch {
    return null;
  }
}

function find(run: Run | null, needle: string): Check | undefined {
  return run?.checks?.find((c) => c.name.includes(needle));
}

export function readBook(run: Run | null): BookState {
  const idle = baseUnits(find(run, "vault idle cash"));
  const outstanding = baseUnits(find(run, "vault principal outstanding"));
  const loss = baseUnits(find(run, "vault realized loss"));

  let utilization: number | null = null;
  if (idle !== null && outstanding !== null) {
    const total = idle + outstanding;
    // A book with nothing in it has no utilization to report. Returning
    // zero would draw a thick, healthy atmosphere around an empty vault.
    utilization =
      total > 0n ? Number((outstanding * 10_000n) / total) / 10_000 : null;
  }

  return { idle, outstanding, loss, utilization };
}

/**
 * Atmosphere thickness in [0, 1], from utilization.
 *
 * `1 - utilization`, so a lightly used book looks padded and a heavily
 * used one looks vacuum-thin. Missing data gives `null`, and the caller
 * draws no atmosphere rather than a reassuring default.
 */
export function atmosphere(book: BookState): number | null {
  if (book.utilization === null) return null;
  return Math.max(0, Math.min(1, 1 - book.utilization));
}

/**
 * Core brightness in [0, 1].
 *
 * Idle cash against the whole book: a core that dims as the vault lends
 * its cash out and brightens as it is repaid. This is TVL felt rather
 * than tabulated, and it is the same two figures utilization uses, so
 * the two can never disagree.
 */
export function core(book: BookState): number | null {
  if (book.idle === null || book.outstanding === null) return null;
  const total = book.idle + book.outstanding;
  if (total <= 0n) return null;
  return Number((book.idle * 10_000n) / total) / 10_000;
}

/**
 * Whether the book carries a scar, and how deep.
 *
 * Returned as loss against the book's own size, so the crater is
 * proportionate rather than absolute. A default is a permanent mark:
 * `docs/brand/physics.md` says it does not heal, and nothing here fades
 * it with time.
 */
export function scar(book: BookState): number | null {
  if (book.loss === null || book.loss === 0n) return null;
  if (book.idle === null) return null;
  const total = book.idle + (book.outstanding ?? 0n) + book.loss;
  if (total <= 0n) return null;
  return Number((book.loss * 10_000n) / total) / 10_000;
}
