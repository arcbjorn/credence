import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { fixtureMissing } from "./fixtures";
import type { Run } from "./data";

/**
 * The landing screen states the thesis in three figures, and every one
 * is LOADED. A landing page is exactly where a hardcoded headline is
 * most tempting and least visible -- nobody scrolls past the hero to
 * check whether the number under it came from anywhere.
 */
const REPAY = "public/data/repay.json";
const DEFAULT_RUN = "public/data/default.json";

const load = (p: string): Run => JSON.parse(readFileSync(p, "utf8")) as Run;

const originations = (run: Run) =>
  run.assessments.filter(
    (a) => a.payload?.outcome?.decision === "ApprovedOrigination",
  );

describe.skipIf(fixtureMissing(REPAY))("the figures the landing screen states", () => {
  it("has more than one loan, or the rate fall is not a fall", () => {
    // `−1.06%` is the difference between the first and last APR. With a
    // single loan there is no difference to show, and the screen says so
    // rather than rendering a zero that looks like a measurement.
    expect(originations(load(REPAY)).length).toBeGreaterThan(1);
  });

  it("the rate falls as history accumulates", () => {
    const loans = originations(load(REPAY));
    const first = loans[0]!.payload.outcome.terms!.apr_bps;
    const last = loans[loans.length - 1]!.payload.outcome.terms!.apr_bps;
    expect(last).toBeLessThan(first);
  });

  it("the capacity rises with it", () => {
    const loans = originations(load(REPAY));
    const first = BigInt(loans[0]!.payload.outcome.capacity!.agent_capacity);
    const last = BigInt(
      loans[loans.length - 1]!.payload.outcome.capacity!.agent_capacity,
    );
    expect(last).toBeGreaterThan(first);
  });

  // The screen claims the binding constraint "switches from history to
  // portfolio stress" ONLY when the data shows it. A two-loan bundle is
  // still history-bound, and saying otherwise would describe a run that
  // did not happen -- so the claim is conditional in the component and
  // this test pins the condition it depends on.
  it("names a binding constraint on every loan", () => {
    for (const a of originations(load(REPAY))) {
      expect(a.payload.outcome.capacity?.agent_binding).toBeTruthy();
    }
  });
});

describe.skipIf(fixtureMissing(DEFAULT_RUN))("the loss the landing screen states", () => {
  it("comes from a check, in base units", () => {
    const run = load(DEFAULT_RUN);
    const loss = run.checks?.find((c) => c.name.includes("vault realized loss"));
    expect(loss).toBeDefined();
    // Base units, scaled by `units()` for display. A figure already
    // scaled would be understated by 10^6 on screen.
    expect(loss!.detail).toMatch(/^\d+ base units$/);
    expect(BigInt(loss!.detail.split(" ")[0]!)).toBeGreaterThan(0n);
  });
});
