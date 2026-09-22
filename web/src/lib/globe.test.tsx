import { act, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CreditGlobe, type GlobePoint } from "../components/CreditGlobe";

/**
 * The globe's grammar.
 *
 * The premise is that geometry IS the data: if the shape can drift from
 * the figures it claims to draw, it is decoration with extra steps. So
 * these tests check the mapping rather than the markup.
 *
 * The area rule is the one worth pinning. A disc is read by how much ink
 * it covers, and area grows as the square of the radius, so mapping a
 * quantity straight to `r` shows a 31% rise as a ~70% bigger object.
 * That is the flattering version, and it is the easy mistake to make
 * twice.
 */

const POINTS: GlobePoint[] = [
  { capacity: "6200000000", apr: 1200, binding: "HistoryGrowthCap", score: 610 },
  { capacity: "7533330000", apr: 1147, binding: "HistoryGrowthCap", score: 676 },
  { capacity: "8117138556", apr: 1094, binding: "StressCapacity", score: 705 },
];

const discR = () =>
  Number(document.querySelector(".globe-edge")?.getAttribute("r"));

const limit = () => document.querySelector(".globe-limit");

/** Render showing one specific loan, with motion off so it does not walk. */
const at = (i: number) => {
  document.body.innerHTML = "";
  const view = render(<CreditGlobe points={POINTS} />);
  const steps = view.container.querySelectorAll<HTMLButtonElement>(".globe-step");
  // Wrapped: the click sets state, and without `act` the assertions
  // below read the pre-click render and every ratio comes out as 1.
  act(() => steps[i]!.click());
  return view;
};

describe("the object's geometry", () => {
  it("scales the body by area, not by radius", () => {
    at(0);
    const first = discR();
    at(2);
    const third = discR();

    const capRatio =
      Number(BigInt(POINTS[2]!.capacity) / 1_000_000n) /
      Number(BigInt(POINTS[0]!.capacity) / 1_000_000n);

    // Covered area must rise in step with capacity. Radius alone would
    // make this ratio the square of the real one.
    const areaRatio = (third * third) / (first * first);
    expect(areaRatio).toBeCloseTo(capRatio, 2);
  });

  it("never draws the body past the limit ring", () => {
    // Capacity is what the constraint allows, so a disc spilling over
    // its own limit would be drawing an impossible state.
    for (let i = 0; i < POINTS.length; i++) {
      at(i);
      const r = discR();
      const limitR = Number(limit()?.getAttribute("r"));
      expect(r, `loan ${i + 1}`).toBeLessThanOrEqual(limitR);
    }
  });

  it("hardens the limit ring only when stress binds", () => {
    at(0);
    // History-bound: a soft edge, because more history moves it.
    expect(limit()?.getAttribute("stroke-dasharray")).not.toBe("none");

    at(2);
    // Stress-bound: a hard line that behaving well does not move.
    expect(limit()?.getAttribute("stroke-dasharray")).toBe("none");
  });

  it("grows the body as capacity grows", () => {
    at(0);
    const first = discR();
    at(1);
    const second = discR();
    at(2);
    const third = discR();
    expect(second).toBeGreaterThan(first);
    expect(third).toBeGreaterThan(second);
  });

  it("thins the rate ring as the rate falls", () => {
    at(0);
    const dear = Number(
      document.querySelector(".globe-edge")?.getAttribute("stroke-width"),
    );
    at(2);
    const cheap = Number(
      document.querySelector(".globe-edge")?.getAttribute("stroke-width"),
    );
    expect(cheap).toBeLessThan(dear);
  });

  it("draws a refusal as an empty ring in the alarm register", () => {
    // Zero capacity is not a small loan. The agent reaches none of its
    // limit, so there is a ring and nothing inside it -- the refusal
    // stated as geometry rather than as a red word beside a number.
    document.body.innerHTML = "";
    const { container } = render(
      <CreditGlobe
        still
        peakOverride={12000}
        points={[
          { capacity: "0", apr: 0, binding: "StressCapacity", score: 300 },
        ]}
      />,
    );
    expect(Number(container.querySelector(".globe-edge")?.getAttribute("r"))).toBe(0);
    expect(container.querySelector(".globe-limit")?.getAttribute("stroke")).toBe(
      "var(--alarm)",
    );
    // And it says so, rather than printing a 0.00% rate nobody is offered.
    expect(container.textContent).toContain("denied");
    expect(container.textContent).not.toContain("0.00%");
  });

  it("scales against an outside peak so bodies are comparable", () => {
    // Without this each object normalises to its own capacity and a
    // small agent is drawn the same size as a large one.
    document.body.innerHTML = "";
    const { container } = render(
      <CreditGlobe
        still
        peakOverride={12000}
        points={[
          { capacity: "6000000000", apr: 1200, binding: "HistoryGrowthCap" },
        ]}
      />,
    );
    const r = Number(container.querySelector(".globe-edge")?.getAttribute("r"));
    // Half the peak by area, so sqrt(0.5) of the body's maximum. 128 is
    // BODY_MAX_R; the limit ring sits further out at 150, so a full
    // facility still leaves daylight between shell and bound.
    expect(r).toBeCloseTo(Math.sqrt(0.5) * 128, 1);
  });

  it("prints only the instrument figures it was given", () => {
    // The readout is a measurement, not decoration. With no book state
    // there is nothing to report, and a globe that printed UTIL 0% or
    // SCAR 0.0% would be asserting a healthy book it never measured.
    document.body.innerHTML = "";
    const { container } = render(
      <CreditGlobe
        still
        points={[
          { capacity: "6000000000", apr: 1200, binding: "HistoryGrowthCap" },
        ]}
      />,
    );
    expect(container.textContent).not.toContain("UTIL");
    expect(container.textContent).not.toContain("SCAR");
    expect(container.textContent).not.toContain("SCORE");
  });

  it("reports utilization as the inverse of the atmosphere", () => {
    document.body.innerHTML = "";
    const { container } = render(
      <CreditGlobe
        still
        atmosphere={0.92}
        points={[
          { capacity: "6000000000", apr: 1200, binding: "HistoryGrowthCap" },
        ]}
      />,
    );
    // A thick atmosphere is a lightly used book.
    expect(container.textContent).toContain("8%");
  });

  it("renders nothing without points", () => {
    const { container } = render(<CreditGlobe points={[]} />);
    expect(container.querySelector(".globe")).toBeNull();
  });
});
