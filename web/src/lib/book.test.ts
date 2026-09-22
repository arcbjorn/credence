import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { atmosphere, core, readBook, scar } from "./book";
import type { Run } from "./data";

/**
 * The book's physical state.
 *
 * `docs/brand/physics.md` binds the core, the atmosphere and the scar to
 * loaded figures. The property worth pinning hardest is the difference
 * between ABSENT and ZERO: a vault with no idle cash and a vault whose
 * idle cash was never recorded look the same if you collapse them, and
 * the first is a cold market while the second is a missing measurement.
 * Collapsing them would draw a confident atmosphere around nothing.
 */

const run = (checks: { name: string; detail: string }[]): Run =>
  ({ assessments: [], checks }) as unknown as Run;

describe("reading the book", () => {
  it("reads idle, outstanding and loss from the checks", () => {
    const b = readBook(
      run([
        { name: "vault idle cash", detail: "46012860290 base units" },
        { name: "vault principal outstanding", detail: "4000000000 base units" },
        { name: "vault realized loss", detail: "0 base units" },
      ]),
    );
    expect(b.idle).toBe(46012860290n);
    expect(b.outstanding).toBe(4000000000n);
    expect(b.loss).toBe(0n);
  });

  it("computes utilization as outstanding over the whole book", () => {
    const b = readBook(
      run([
        { name: "vault idle cash", detail: "46000000000 base units" },
        { name: "vault principal outstanding", detail: "4000000000 base units" },
      ]),
    );
    expect(b.utilization).toBeCloseTo(0.08, 4);
  });

  it("returns null rather than zero when a check is missing", () => {
    const b = readBook(run([]));
    expect(b.idle).toBeNull();
    expect(b.outstanding).toBeNull();
    expect(b.utilization).toBeNull();
    // And the derived properties refuse to invent a value.
    expect(atmosphere(b)).toBeNull();
    expect(core(b)).toBeNull();
    expect(scar(b)).toBeNull();
  });

  it("returns null for an empty book rather than full health", () => {
    // Nothing lent and nothing idle is not a safe book, it is no book.
    const b = readBook(
      run([
        { name: "vault idle cash", detail: "0 base units" },
        { name: "vault principal outstanding", detail: "0 base units" },
      ]),
    );
    expect(b.utilization).toBeNull();
    expect(atmosphere(b)).toBeNull();
    expect(core(b)).toBeNull();
  });

  it("ignores a detail that is not a base-unit figure", () => {
    const b = readBook(
      run([{ name: "vault idle cash", detail: "debtOutstanding=true" }]),
    );
    expect(b.idle).toBeNull();
  });
});

describe("atmosphere", () => {
  it("is thick when little is lent and thin when much is", () => {
    const light = readBook(
      run([
        { name: "vault idle cash", detail: "90 base units" },
        { name: "vault principal outstanding", detail: "10 base units" },
      ]),
    );
    const heavy = readBook(
      run([
        { name: "vault idle cash", detail: "9 base units" },
        { name: "vault principal outstanding", detail: "91 base units" },
      ]),
    );
    expect(atmosphere(light)!).toBeGreaterThan(0.85);
    expect(atmosphere(heavy)!).toBeLessThan(0.15);
    expect(atmosphere(light)!).toBeGreaterThan(atmosphere(heavy)!);
  });
});

describe("scar", () => {
  it("is absent when nothing was written off", () => {
    const b = readBook(
      run([
        { name: "vault idle cash", detail: "100 base units" },
        { name: "vault realized loss", detail: "0 base units" },
      ]),
    );
    expect(scar(b)).toBeNull();
  });

  it("is present and proportionate when a loan defaulted", () => {
    const b = readBook(
      run([
        { name: "vault idle cash", detail: "47400 base units" },
        { name: "vault principal outstanding", detail: "0 base units" },
        { name: "vault realized loss", detail: "2600 base units" },
      ]),
    );
    const s = scar(b)!;
    expect(s).toBeGreaterThan(0);
    expect(s).toBeCloseTo(2600 / 50000, 3);
  });
});

/**
 * The figures the physics claims must exist in the shipped bundle. If a
 * capture stops recording one, the body silently loses a law -- and the
 * page would keep rendering, minus the meaning.
 */
const REPAY = resolve(process.cwd(), "public/data/repay.json");
const DEFAULT_RUN = resolve(process.cwd(), "public/data/default.json");

describe.skipIf(!existsSync(REPAY))("the shipped repay bundle", () => {
  it("carries the figures the atmosphere needs", () => {
    const b = readBook(JSON.parse(readFileSync(REPAY, "utf8")) as Run);
    expect(b.idle).not.toBeNull();
    expect(b.outstanding).not.toBeNull();
    expect(b.utilization).not.toBeNull();
    expect(atmosphere(b)).not.toBeNull();
  });
});

describe.skipIf(!existsSync(DEFAULT_RUN))("the shipped default bundle", () => {
  it("carries a real scar", () => {
    // Beat 7: the loss is on chain, and the body wears it.
    const b = readBook(JSON.parse(readFileSync(DEFAULT_RUN, "utf8")) as Run);
    expect(b.loss).not.toBeNull();
    expect(b.loss! > 0n).toBe(true);
    expect(scar(b)).not.toBeNull();
  });
});
