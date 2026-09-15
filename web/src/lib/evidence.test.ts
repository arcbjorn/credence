import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { fixtureMissing } from "./fixtures";
import { describe, expect, it } from "vitest";
import type { Run } from "./data";
import { evidenceMode } from "./modes";

/**
 * `docs/09`: where did this come from, how fresh, how complete, is it
 * synthetic?
 *
 * The inspector shows each item separately, and these pin the properties
 * that make that worth doing: every item carries its own origin and its
 * own content hash, and nothing was observed AFTER the moment it was
 * assessed at.
 */
const ROOT = resolve(process.cwd(), "..");
const MONITOR = `${ROOT}/web/public/data/monitor.json`;
const ITEMS = ["account", "market", "history", "forecasts", "portfolio"] as const;

describe.skipIf(fixtureMissing(MONITOR))("the evidence a decision rested on", () => {
  const run = (): Run => JSON.parse(readFileSync(MONITOR, "utf8"));
  const snapshots = () => Object.values(run().snapshots);

  it("labels every item with an origin the interface knows", () => {
    for (const s of snapshots()) {
      for (const key of ITEMS) {
        const e = s[key];
        if (!e) continue;
        // An unrecognized origin renders verbatim rather than defaulting
        // to Observed, which would overstate the evidence.
        expect(evidenceMode(e.origin).token).not.toBe("unknown");
      }
    }
  });

  it("never shows evidence observed after the assessment", () => {
    // Evidence from the future is not evidence. This is the wall the
    // simulator enforces one layer down; here it is checked on what the
    // live gateway actually retained.
    for (const s of snapshots()) {
      for (const key of ITEMS) {
        const e = s[key];
        if (!e || !e.observed_at) continue;
        expect(
          e.observed_at,
          `${key} observed after assessment_time`,
        ).toBeLessThanOrEqual(s.assessment_time);
      }
    }
  });

  it("commits to each item separately", () => {
    for (const s of snapshots()) {
      const hashes = ITEMS.map((k) => s[k]?.content_hash).filter(Boolean);
      expect(hashes.length).toBeGreaterThan(0);
      for (const h of hashes) {
        expect(h).toMatch(/^[0-9a-f]{64}$/);
      }
    }
  });
});
