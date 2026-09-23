import { existsSync, readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { Run } from "./data";
import { curve, earned, plot } from "./earned";

/**
 * The growth run, checked as the claim it is meant to support.
 *
 * `demo-growth.sh` holds the principal, term, market and policy constant
 * and varies only the agent's record. If these properties do not hold,
 * the run demonstrates nothing about earned credit and the screen built
 * on it would be asserting something its own data does not show.
 */
const PATH = "public/data/growth.json";
const missing = !existsSync(PATH);
const load = (): Run => JSON.parse(readFileSync(PATH, "utf8")) as Run;

describe.skipIf(missing)("the growth run", () => {
  it("priced every origination in the sequence", () => {
    const points = curve(load());
    expect(points.length).toBeGreaterThanOrEqual(4);
  });

  it("held the principal constant, so the delta is attributable", () => {
    const summary = earned(curve(load()))!;
    expect(summary.constant_principal).toBe(true);
  });

  it("lowered the rate as the record grew", () => {
    const summary = earned(curve(load()))!;
    expect(summary.discount_bps).toBeGreaterThan(0);
    expect(summary.last.apr_bps).toBeLessThan(summary.first.apr_bps);
  });

  it("read a record that actually improved", () => {
    // The rate falling is only evidence of EARNED credit if the input
    // that earned it moved. A rate that fell while the record stayed put
    // would mean something else caused it.
    const points = curve(load()).filter((p) => p.repayments_ppm !== null);
    expect(points.length).toBeGreaterThan(1);
    expect(points[points.length - 1]!.repayments_ppm!).toBeGreaterThan(
      points[0]!.repayments_ppm!,
    );
  });

  it("names the constraint that bound each ceiling", () => {
    for (const point of curve(load())) expect(point.binding).toBeTruthy();
  });

  it("hands off from the growth cap to the stress test", () => {
    // The story the run tells: early on the limit is how little it has
    // proven, and later it is what the book survives. If only one
    // constraint ever binds, the curve is flatter than it looks.
    const summary = earned(curve(load()))!;
    expect(summary.bindings).toContain("HistoryGrowthCap");
    expect(summary.bindings).toContain("StressCapacity");
  });
});

describe("the curve summary", () => {
  const point = (sequence: number, apr_bps: number, over = {}) => ({
    sequence,
    apr_bps,
    capacity: "1000",
    binding: "StressCapacity",
    principal: "500",
    repayments_ppm: 0,
    score: null,
    ...over,
  });

  it("refuses to summarise a single point", () => {
    // One origination is not a trend, and a summary through it would be
    // a claim the data cannot support.
    expect(earned([point(1, 1200)])).toBeNull();
    expect(earned([])).toBeNull();
  });

  it("reports a rising rate as a negative discount", () => {
    const summary = earned([point(1, 1000), point(2, 1200)])!;
    expect(summary.discount_bps).toBe(-200);
  });

  it("notices when the principal moved", () => {
    const summary = earned([
      point(1, 1200),
      point(2, 1100, { principal: "900" }),
    ])!;
    expect(summary.constant_principal).toBe(false);
  });

  it("drops assessments that carry no quoted rate", () => {
    // A monitoring assessment has no terms. Including it would put a gap
    // in the middle of the curve, which reads as a rate of zero.
    const run = {
      assessments: [
        { sequence: 1, input_hash: "a", payload: { sequence: 1, outcome: { terms: { apr_bps: 1200 }, capacity: { agent_capacity: "1", agent_binding: "X" } } } },
        { sequence: 2, input_hash: "b", payload: { sequence: 2, outcome: { decision: "Allowed" } } },
      ],
      snapshots: {},
    } as unknown as Run;
    expect(curve(run)).toHaveLength(1);
  });

  it("orders by sequence rather than by storage order", () => {
    const run = {
      assessments: [
        { sequence: 2, input_hash: "b", payload: { sequence: 2, outcome: { terms: { apr_bps: 1100 }, capacity: { agent_capacity: "1", agent_binding: "X" } } } },
        { sequence: 1, input_hash: "a", payload: { sequence: 1, outcome: { terms: { apr_bps: 1200 }, capacity: { agent_capacity: "1", agent_binding: "X" } } } },
      ],
      snapshots: {},
    } as unknown as Run;
    expect(curve(run).map((p) => p.sequence)).toEqual([1, 2]);
  });
});

describe("the plot", () => {
  const points = [
    { sequence: 1, apr_bps: 1200, capacity: "1", binding: "X", principal: "1", repayments_ppm: 0, score: null },
    { sequence: 2, apr_bps: 900, capacity: "1", binding: "X", principal: "1", repayments_ppm: 0, score: null },
  ];

  it("draws a falling series as a falling line", () => {
    // SVG's origin is top-left, so the y axis has to be inverted or a
    // rate discount reads as a rate increase.
    const laid = plot(points, (p) => p.apr_bps, 100, 50);
    expect(laid.points[1]!.y).toBeGreaterThan(laid.points[0]!.y);
  });

  it("survives a flat series without dividing by zero", () => {
    const flat = plot(
      points.map((p) => ({ ...p, apr_bps: 1000 })),
      (p) => p.apr_bps,
      100,
      50,
    );
    for (const p of flat.points) expect(Number.isFinite(p.y)).toBe(true);
  });

  it("returns an empty path for no points", () => {
    expect(plot([], (p) => p.apr_bps, 100, 50).path).toBe("");
  });
});
