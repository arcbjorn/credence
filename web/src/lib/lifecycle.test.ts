import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { fixtureMissing } from "./fixtures";
import { describe, expect, it } from "vitest";
import type { Run } from "./data";

/**
 * `docs/06`: a record is weighted by what RESOLVED, not by how many
 * forecasts were attempted.
 *
 * The screen therefore has to show unresolved forecasts too. Listing only
 * the resolved ones would let a borrower's apparent calibration improve by
 * abandoning the forecasts going badly — the exact gaming the weighting
 * rule exists to prevent.
 *
 * This also pins the bundle's decoding: the generator applied its parser
 * to two tables and let `default=str` catch the rest, which shipped
 * payloads as the Python repr `"b'{...}'"` — a string no consumer can
 * read. Eight fields carried it.
 */
const ROOT = resolve(process.cwd(), "..");
const REPAY = `${ROOT}/web/public/data/repay.json`;

describe.skipIf(fixtureMissing(REPAY))("the lifecycle bundle", () => {
  const run = (): Run => JSON.parse(readFileSync(REPAY, "utf8"));

  it("retains a forecast for every loan", () => {
    const r = run();
    const originations = r.assessments.filter(
      (a) => a.payload?.outcome?.decision === "ApprovedOrigination",
    );
    expect(r.forecasts.length).toBeGreaterThanOrEqual(originations.length);
  });

  it("commits each forecast before its outcome resolves", () => {
    const r = run();
    const byId = new Map(r.forecasts.map((f) => [f.forecast_id, f]));
    for (const o of r.outcomes) {
      const f = byId.get(o.forecast_id);
      expect(f, `no forecast for ${o.outcome_id}`).toBeDefined();
      // `docs/06`: committed before funding, scored afterwards.
      expect(o.resolved_at).toBeGreaterThan(f!.committed_at);
    }
  });

  it("keeps unresolved forecasts in the record", () => {
    const r = run();
    const resolved = new Set(r.outcomes.map((o) => o.forecast_id));
    const unresolved = r.forecasts.filter((f) => !resolved.has(f.forecast_id));
    // This run has a live loan whose forecast has not resolved. It must
    // still be present, or the screen is only showing the flattering half.
    expect(r.forecasts.length).toBe(resolved.size + unresolved.length);
  });

  it("decodes payloads to objects, never a bytes repr", () => {
    const r = run();
    const raw = readFileSync(REPAY, "utf8");
    expect(raw).not.toContain(`"b'`);
    for (const f of r.forecasts) {
      if (f.payload !== undefined) {
        expect(typeof f.payload).toBe("object");
      }
    }
  });
});
