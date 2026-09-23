import { render, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CutModel, type Layer } from "../components/CutModel";
import { Eclipse } from "../components/Eclipse";

/**
 * The two big shapes, checked for the property that makes them
 * admissible: their geometry is derived from a figure, so neither can
 * flatter the book.
 *
 * An eclipse drawn at a guessed offset would be the most confident lie
 * on the page -- it reads instantly and nobody checks it against a
 * number. So with no utilization it renders nothing at all.
 */

const LAYERS: Layer[] = [
  { key: "a", label: "The agent", note: "identity", amount: null, register: "machine" },
  { key: "b", label: "Its record", note: "history", amount: null, register: "growth" },
  { key: "c", label: "Held", note: "collateral", amount: 7000n, register: "machine" },
  { key: "d", label: "Lender cash", note: "free", amount: 46000n, register: "growth" },
];

const ring = (n: number) =>
  document.querySelectorAll(".cut-ring")[n]!;

describe("the eclipse", () => {
  it("renders nothing without a utilization figure", () => {
    const { container } = render(<Eclipse utilization={null} />);
    expect(container.querySelector(".eclipse")).toBeNull();
  });

  it("separates the bodies when little is drawn", () => {
    document.body.innerHTML = "";
    const { container } = render(<Eclipse utilization={0.08} />);
    const [light, shadow] = [
      container.querySelector(".eclipse-light")!,
      container.querySelector(".eclipse-shadow")!,
    ];
    const gap = Math.abs(
      Number(shadow.getAttribute("cx")) - Number(light.getAttribute("cx")),
    );
    // An 8% covered area leaves most of the diameter exposed.
    expect(gap).toBeGreaterThan(90);
  });

  it("closes the bodies as the book is drawn down", () => {
    document.body.innerHTML = "";
    const light = render(<Eclipse utilization={0.1} />);
    const lightGap = Math.abs(
      Number(light.container.querySelector(".eclipse-shadow")!.getAttribute("cx")) -
        Number(light.container.querySelector(".eclipse-light")!.getAttribute("cx")),
    );

    document.body.innerHTML = "";
    const heavy = render(<Eclipse utilization={0.9} />);
    const heavyGap = Math.abs(
      Number(heavy.container.querySelector(".eclipse-shadow")!.getAttribute("cx")) -
        Number(heavy.container.querySelector(".eclipse-light")!.getAttribute("cx")),
    );

    // More drawn means more overlap, which means less distance.
    expect(heavyGap).toBeLessThan(lightGap);
  });

  it("changes both surface materials when the theme changes", async () => {
    document.documentElement.dataset.theme = "light";
    const { container } = render(<Eclipse utilization={.61} />);
    const colors = () => Array.from(container.querySelectorAll("stop")).map(stop => stop.getAttribute("stop-color"));
    const light = colors();
    document.documentElement.dataset.theme = "dark";
    await waitFor(() => expect(colors()).not.toEqual(light));
    document.documentElement.dataset.theme = "light";
    await waitFor(() => expect(colors()).toEqual(light));
  });

  it("states the figure it drew", () => {
    document.body.innerHTML = "";
    const { container } = render(<Eclipse utilization={0.61} />);
    expect(container.textContent).toContain("61% drawn");
  });
});

describe("the cut model", () => {
  it("renders one ring per layer", () => {
    document.body.innerHTML = "";
    render(<CutModel layers={LAYERS} />);
    expect(document.querySelectorAll(".cut-ring").length).toBe(LAYERS.length);
  });

  it("dashes a layer nothing measured", () => {
    document.body.innerHTML = "";
    render(<CutModel layers={LAYERS} />);
    // Layers a and b carry no amount, so they claim no width.
    const edge = ring(0).querySelectorAll("circle")[1]!;
    expect(edge.getAttribute("stroke-dasharray")).not.toBe("none");
  });

  it("draws a measured layer solid", () => {
    document.body.innerHTML = "";
    render(<CutModel layers={LAYERS} />);
    const edge = ring(3).querySelectorAll("circle")[1]!;
    expect(edge.getAttribute("stroke-dasharray")).toBe("none");
  });

  it("gives the larger figure the wider band", () => {
    document.body.innerHTML = "";
    render(<CutModel layers={LAYERS} />);
    const width = (n: number) =>
      Number(ring(n).querySelectorAll("circle")[0]!.getAttribute("stroke-width"));
    // Lender cash (46000) dwarfs the held collateral (7000).
    expect(width(3)).toBeGreaterThan(width(2));
  });

  it("renders nothing without layers", () => {
    const { container } = render(<CutModel layers={[]} />);
    expect(container.querySelector(".cut")).toBeNull();
  });
});
