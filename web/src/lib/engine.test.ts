import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import init, { evaluate, replay, model_hash } from "../engine/risk_wasm.js";

/**
 * The browser engine must agree with the native one.
 *
 * This is the claim the replay screen rests on: `docs/09` beat 8 is only
 * evidence if the engine in the reader's tab is the same computation the
 * gateway performs. A viewer that agreed for a different reason would be
 * worse than no viewer.
 *
 * The figures asserted below are the ones `docs/11` publishes for the
 * demo's third loan, and `risk-core`'s own
 * `demo_third_loan_binds_on_stress_capacity` pins them natively. If these
 * ever disagree, one of the two engines has drifted.
 */
const ROOT = "/Users/arc/projects/credence";
const read = (p: string) => readFileSync(`${ROOT}/${p}`, "utf8");

async function engine() {
  await init({ module_or_path: readFileSync(`${ROOT}/web/src/engine/risk_wasm_bg.wasm`) });
  return { evaluate, replay, model_hash };
}

describe("the engine, compiled for the browser", () => {
  it("reports the same model hash the native engine does", async () => {
    const e = await engine();
    expect(e.model_hash()).toBe(
      "eb1f28871b21718a9db5a3e65fcdca22fc1680e9f369d8adb25869966461a1a3",
    );
  });

  it("reproduces the figures docs/11 publishes for the demo's loan 3", async () => {
    const e = await engine();
    const input = read("fixtures/demo/repay-loan3-origination.json");
    const policy = read("config/engine-policy.json");

    const out = JSON.parse(e.evaluate(input, policy));
    expect(out.outcome.capacity.agent_binding).toBe("StressCapacity");
    expect(out.outcome.terms.apr_bps).toBe(1094);
    expect(out.outcome.capacity.agent_capacity).toBe("8117138556");
  });

  it("matches when an assessment is replayed against its own input", async () => {
    const e = await engine();
    const input = read("fixtures/demo/repay-loan3-origination.json");
    const policy = read("config/engine-policy.json");
    const assessment = e.evaluate(input, policy);

    const report = e.replay(assessment, input, policy);
    expect(report.matched).toBe(true);
    expect(report.differences).toHaveLength(0);
  });

  it("reports a DIFFERENCE when one digit of the mark is altered", async () => {
    // `docs/11`: a MATCH is only evidence if a tampered run can produce a
    // DIFFERENCE. A replay that cannot fail proves nothing.
    const e = await engine();
    const input = read("fixtures/demo/repay-loan3-origination.json");
    const policy = read("config/engine-policy.json");
    const assessment = e.evaluate(input, policy);

    const tampered = JSON.parse(input);
    tampered.market.value.realized_volatility_bps = 4000;

    const report = e.replay(assessment, JSON.stringify(tampered), policy);
    expect(report.matched).toBe(false);
    expect(report.differences.length).toBeGreaterThan(0);
  });

  it("refuses a replay under a different model rather than calling it a mismatch", async () => {
    // A replay under a different model is not a replay that disagreed, it
    // is a replay that never happened -- and reporting it as a difference
    // would send a reader looking for drift that is not there.
    const e = await engine();
    const input = read("fixtures/demo/repay-loan3-origination.json");
    const policy = read("config/engine-policy.json");
    const assessment = JSON.parse(e.evaluate(input, policy));
    assessment.model_hash = "0".repeat(64);

    expect(() => e.replay(JSON.stringify(assessment), input, policy)).toThrow(
      /model mismatch/,
    );
  });
});
