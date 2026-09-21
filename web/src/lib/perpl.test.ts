import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { fixtureMissing } from "./fixtures";
import type { Perpl } from "./data";

/**
 * The Perpl capture, checked as a contract rather than as a snapshot.
 *
 * These assert properties that must hold of ANY capture -- a venue that
 * moved would break them, and a hand-edited file would too. Pinning exact
 * figures would fail on every refresh instead, which trains a reader to
 * regenerate rather than to look.
 */
const PATH = "public/data/perpl.json";
const missing = fixtureMissing(PATH);

const load = (): Perpl => JSON.parse(readFileSync(PATH, "utf8")) as Perpl;

describe.skipIf(missing)("the Perpl capture", () => {
  it("observes both engine inputs", () => {
    const p = load();
    // Volatility may legitimately be zero in a calm window -- the stress
    // floor exists for that case -- so only depth is required positive.
    expect(BigInt(p.market.liquidity_depth)).toBeGreaterThan(0n);
    expect(p.market.volatility_window_seconds).toBeGreaterThan(0);
    expect(p.market.realized_volatility_bps).toBeGreaterThanOrEqual(0);
  });

  it("summed depth from the bid side it displays", () => {
    const p = load();
    expect(p.book.bids.length).toBeGreaterThan(0);
    // The displayed rows are a WINDOW onto the book, never the whole of
    // it, or a reader would conclude depth was summed from eight levels.
    expect(p.book.bid_levels).toBeGreaterThanOrEqual(p.book.bids.length);
  });

  it("keeps the two snapshots identical apart from market evidence", () => {
    const p = load();
    const obs = p.snapshot as Record<string, unknown>;
    const base = p.baseline as Record<string, unknown>;

    // This is what makes the comparison mean anything. If the account or
    // the clock differed too, the capacity difference would no longer be
    // attributable to the market.
    for (const key of Object.keys(obs)) {
      if (key === "market") continue;
      expect(JSON.stringify(base[key]), `${key} differs`).toBe(
        JSON.stringify(obs[key]),
      );
    }
  });

  it("marks the observed side observed and the baseline declared", () => {
    const p = load();
    const market = (p.snapshot as { market: { origin: string; source: string } })
      .market;
    const baseline = (p.baseline as { market: { origin: string } }).market;

    expect(market.origin).toBe("Observed");
    expect(market.source).toBe("perpl");
    // The baseline keeps its declared provenance: it is the counterfactual,
    // not a second observation.
    expect(baseline.origin).toBe("Synthetic");
  });

  it("gives the baseline the observed clock", () => {
    const p = load();
    const obs = p.snapshot as { assessment_time: number };
    const base = p.baseline as { assessment_time: number };
    // Evaluating the two at different times would compare two different
    // questions, and the stale-evidence blocker would answer the second.
    expect(base.assessment_time).toBe(obs.assessment_time);
  });

  it("reports funding as a bounded, signed premium", () => {
    const p = load();
    // Funding is a premium, not a cost: it legitimately goes negative when
    // shorts pay longs, so an unsigned reading would be wrong.
    expect(p.funding.samples).toBeGreaterThan(0);
    expect(p.funding.max_abs_bps).toBeGreaterThanOrEqual(0);
    expect(p.funding.one_sided_pct).toBeGreaterThanOrEqual(0);
    expect(p.funding.one_sided_pct).toBeLessThanOrEqual(100);
    expect(Math.abs(p.funding.mean_bps)).toBeLessThanOrEqual(
      p.funding.max_abs_bps,
    );
  });

  it("uses a snapshot the market can actually move", () => {
    const p = load();
    const snap = p.snapshot as {
      context?: { kind?: string };
    };
    // An origination snapshot: the stress scenarios size a principal
    // there. A monitoring one would stress an existing book instead.
    expect(snap.context?.kind).toBe("Origination");

    // Capacity is a min() over several constraints and only the stress
    // term reads market evidence. On a history-bound snapshot the panel
    // shows two identical capacities and proves nothing about the
    // market -- which is exactly what a build produced on 21 September,
    // because `snapshots` is a dict with no meaningful order and loan 1
    // is history-bound. The capture now prefers a stress-bound one.
    //
    // Not asserted as REQUIRED: a run with no stress-bound origination
    // is legitimate, and the page then names the binding constraint
    // instead of claiming the market had no effect. What must hold is
    // that the file records which snapshot was chosen.
    expect(p.snapshot_source).toBeTruthy();
  });

  it("names the run rather than a scratch path", () => {
    const p = load();
    // A mktemp path rendered as "tmp.nbcHaDQ3zO" on the page and told a
    // reader nothing. Caught by looking at the page, not by a test.
    expect(p.snapshot_source).not.toMatch(/^\/|tmp\./);
  });
});
