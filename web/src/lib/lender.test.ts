import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { fixtureMissing } from "./fixtures";
import type { Check } from "./data";

/**
 * The lender screen must be able to show a loss that is not zero.
 *
 * It could not, until 18 September. The screen read `demo-repay`, where
 * nothing ever defaults, so its "realized loss" figure was structurally
 * always `0` -- the one screen whose job is showing a lender loss could
 * never show one, and `docs/09` beat 7 asks for a loss that is realised
 * rather than asserted.
 *
 * The cause was upstream: `demo-default.sh` drives the contracts directly
 * and retains no SQLite, and the bundler dropped any run without a
 * database, taking the realised loss with it.
 *
 * These assert the shape the screen depends on, at the data layer, so a
 * regression fails here rather than as a zero nobody notices.
 */
const ROOT = resolve(process.cwd(), "..");
const REPAY = `${ROOT}/web/public/data/repay.json`;
const DEFAULT = `${ROOT}/web/public/data/default.json`;

const ledgers = (path: string): Check[] =>
  (JSON.parse(readFileSync(path, "utf8")).checks ?? []).filter(
    (c: Check) => c.beat === "lender",
  );

const value = (checks: Check[], needle: string): bigint => {
  const hit = checks.find((c) => c.name.includes(needle));
  if (hit === undefined) throw new Error(`no ledger matching "${needle}"`);
  return BigInt(hit.detail.split(" ")[0] ?? "0");
};

describe.skipIf(fixtureMissing(REPAY))("the clean epoch", () => {
  it("reports no realized loss", () => {
    // The control. If a clean run showed a loss, the contrast below would
    // prove nothing about loss accounting.
    expect(value(ledgers(REPAY), "realized loss")).toBe(0n);
  });

  it("reports principal still outstanding", () => {
    expect(value(ledgers(REPAY), "principal outstanding")).toBeGreaterThan(0n);
  });
});

// `fixtureMissing`, like every other bundle file: CI runs
// `demo-default.sh`, so an absent `default.json` there is a broken
// pipeline rather than a developer who has not run that demo. Locally it
// stays a skip. Guarding it with a bare `existsSync` would let CI report
// green while the only non-zero loss in the interface went unchecked --
// the failure mode `fixtures.ts` exists to prevent.
describe.skipIf(fixtureMissing(DEFAULT))("the epoch that lost money", () => {
  it("reports a realized loss that is not zero", () => {
    // The whole point. A screen that can only ever render zero here is
    // not demonstrating loss accounting.
    expect(value(ledgers(DEFAULT), "realized loss")).toBeGreaterThan(0n);
  });

  it("leaves no principal outstanding", () => {
    // `docs/05`: the write-off records the loss AS loss. Principal left
    // outstanding after a default would be exposure the vault no longer
    // has, counted twice.
    expect(value(ledgers(DEFAULT), "principal outstanding")).toBe(0n);
  });

  it("recovered something rather than nothing", () => {
    // A total loss would also satisfy "loss > 0", and would not show the
    // waterfall working. The demo crashes the asset far enough to break
    // principal while leaving real proceeds to collect.
    expect(value(ledgers(DEFAULT), "idle cash")).toBeGreaterThan(0n);
  });
});
