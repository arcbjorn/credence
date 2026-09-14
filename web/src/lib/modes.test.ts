import { describe, expect, it } from "vitest";
import { CLASS_MODES, EVIDENCE_MODES, ROUTE_MODES, classMode, evidenceMode } from "./modes";

/**
 * `docs/09`: "Never collapse these into an ambiguous green indicator."
 *
 * The rule is easy to state and easy to violate later, so it is pinned
 * here rather than left to review.
 */
describe("mode labels", () => {
  it("gives scenario and observed evidence distinct tokens", () => {
    // The failure this prevents: a refactor that maps both to a shared
    // "ok" token, at which point synthetic history renders identically to
    // observed chain state.
    expect(EVIDENCE_MODES.Synthetic.token).not.toBe(EVIDENCE_MODES.Observed.token);
  });

  it("gives the two authorization routes distinct tokens", () => {
    expect(ROUTE_MODES.Development.token).not.toBe(ROUTE_MODES.CRE.token);
  });

  it("has no generic success token any mode could fall back to", () => {
    const tokens = [
      ...Object.values(EVIDENCE_MODES),
      ...Object.values(ROUTE_MODES),
      ...Object.values(CLASS_MODES),
    ].map((m) => m.token);
    for (const banned of ["ok", "success", "green", "healthy", "valid"]) {
      expect(tokens).not.toContain(banned);
    }
  });

  it("carries a plain label for every mode, never an empty string", () => {
    for (const m of [...Object.values(EVIDENCE_MODES), ...Object.values(ROUTE_MODES)]) {
      expect(m.label.trim().length).toBeGreaterThan(0);
      expect(m.meaning.trim().length).toBeGreaterThan(0);
    }
  });

  it("shows an unknown origin verbatim rather than guessing", () => {
    // An unrecognized origin must not silently render as observed, which
    // is the one outcome that would overstate the evidence.
    const m = evidenceMode("SomethingNew");
    expect(m.token).toBe("unknown");
    expect(m.label).toContain("SomethingNew");
    expect(m.token).not.toBe(EVIDENCE_MODES.Observed.token);
  });

  it("treats ScenarioOnly as a mode, not a warning", () => {
    // It is the correct, expected class for a scenario facility. Rendering
    // it as a failure would train a reader to misread the demo's normal
    // state.
    expect(classMode("ScenarioOnly").token).toBe("scenario");
  });
});
