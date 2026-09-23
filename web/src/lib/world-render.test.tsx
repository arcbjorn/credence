import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { World } from "../components/World";
import type { BookState } from "./book";
import { agentColor } from "./world-palette";
import type { LoanArc, WorldNode } from "./observatory";

/**
 * The world actually puts ink on its canvas.
 *
 * Every other test around the world checks geometry helpers in isolation:
 * given a latitude, where does the point land. None of them check the one
 * thing a reader sees, which is whether the draw loop runs at all and
 * reaches the canvas. A blank 800x800 plate passes every geometry test in
 * the suite.
 *
 * That gap is not hypothetical. The draw loop is gated three ways --
 * an IntersectionObserver `visible` latch, `document.hidden`, and a
 * delta-time floor -- and each gate has already been wrong once. The
 * comment in `World.tsx` records an IntersectionObserver that latched
 * `false` during first layout and stopped the loop for good.
 *
 * jsdom has no canvas backend, so these drive a recording stub: the
 * question is whether the component issues draw calls, not whether a
 * rasterizer honours them.
 */

const BOOK: BookState = {
  idle: 46_012_860_290_000_000_000_000n,
  outstanding: 4_000_000_000_000_000_000_000n,
  loss: 0n,
  utilization: 0.08,
};

interface Recorder {
  calls: string[];
  ctx: Record<string, unknown>;
  /** `strokeStyle` as it stood at each `stroke()`. */
  strokes: string[];
  /** `fillStyle` as it stood at each `fill()`. */
  fills: string[];
}

/**
 * A 2D context that records the method names it is asked for.
 *
 * Returning a real object for every canvas method matters: the draw loop
 * calls dozens of them per frame and a missing one throws inside a
 * requestAnimationFrame callback, where the failure is swallowed and
 * looks exactly like the blank plate this test is meant to catch.
 */
function recordingContext(): Recorder {
  const calls: string[] = [];
  const strokes: string[] = [];
  const fills: string[] = [];
  const noop = (name: string) => (...args: unknown[]) => {
    calls.push(name);
    void args;
  };
  // The colour a stroke or fill USED, sampled when it happens. Recording
  // only method names cannot tell a globe whose arcs are all one jade
  // from one where each agent has its own hue, which is the distinction
  // these tests exist to hold.
  const styled = (name: string, sink: string[], prop: string) => (...args: unknown[]) => {
    calls.push(name);
    sink.push(String((ctx as Record<string, unknown>)[prop] ?? ""));
    void args;
  };
  const ctx: Record<string, unknown> = {
    canvas: null,
    setTransform: noop("setTransform"),
    clearRect: noop("clearRect"),
    beginPath: noop("beginPath"),
    closePath: noop("closePath"),
    moveTo: noop("moveTo"),
    lineTo: noop("lineTo"),
    arc: noop("arc"),
    ellipse: noop("ellipse"),
    fill: styled("fill", fills, "fillStyle"),
    stroke: styled("stroke", strokes, "strokeStyle"),
    save: noop("save"),
    restore: noop("restore"),
    translate: noop("translate"),
    scale: noop("scale"),
    rotate: noop("rotate"),
    clip: noop("clip"),
    fillRect: noop("fillRect"),
    strokeRect: noop("strokeRect"),
    fillText: noop("fillText"),
    measureText: () => ({ width: 10 }),
    bezierCurveTo: noop("bezierCurveTo"),
    quadraticCurveTo: noop("quadraticCurveTo"),
    createLinearGradient: () => ({ addColorStop: () => {} }),
    createRadialGradient: () => ({ addColorStop: () => {} }),
    putImageData: noop("putImageData"),
    getImageData: () => ({ data: new Uint8ClampedArray(4) }),
    createImageData: () => ({ data: new Uint8ClampedArray(4) }),
    drawImage: noop("drawImage"),
    setLineDash: noop("setLineDash"),
    fillStyle: "",
    strokeStyle: "",
    lineWidth: 1,
    globalAlpha: 1,
    font: "",
    textAlign: "",
    textBaseline: "",
    lineCap: "",
    lineJoin: "",
    globalCompositeOperation: "",
    filter: "",
  };
  return { calls, ctx, strokes, fills };
}

let recorder: Recorder;
let frameCallbacks: FrameRequestCallback[];

beforeEach(() => {
  recorder = recordingContext();
  frameCallbacks = [];

  // jsdom defines none of these. `World` bails out entirely when
  // `CanvasRenderingContext2D` is missing, so without the constructor the
  // draw loop never starts and every assertion below would pass or fail
  // for reasons that have nothing to do with the component.
  vi.stubGlobal("CanvasRenderingContext2D", function () {});
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  // Report the world as on screen. The real gate is an intersection
  // callback; a stub that never fires would leave `visible` at its
  // initial value and silently decide the outcome of these tests.
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(private cb: (entries: { isIntersecting: boolean }[]) => void) {
        queueMicrotask(() => this.cb([{ isIntersecting: true }]));
      }
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );

  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation(
    () => recorder.ctx as unknown as CanvasRenderingContext2D,
  );

  // The component owns its own rAF loop. Capturing the callbacks lets a
  // test advance exactly as many frames as it wants, with a timestamp it
  // controls, instead of racing a real scheduler.
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    frameCallbacks.push(cb);
    return frameCallbacks.length;
  });
  vi.stubGlobal("cancelAnimationFrame", () => {});

  // jsdom lays everything out at zero. The wrapper's width is what sizes
  // the bitmap, so without this the canvas is legitimately empty and the
  // test would pass for the wrong reason.
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(800);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/** Run the pending frame callbacks once, at `now`. */
function advance(now: number) {
  const pending = frameCallbacks;
  frameCallbacks = [];
  act(() => {
    for (const cb of pending) cb(now);
  });
}

describe("the world draws", () => {
  it("issues draw calls on a visible canvas", () => {
    render(<World book={BOOK} projection="globe" />);
    // First frame establishes `last`; the second is the one that draws.
    advance(1000);
    advance(1016);

    expect(recorder.calls).toContain("clearRect");
    expect(recorder.calls.filter(c => c === "arc").length).toBeGreaterThan(0);
    expect(recorder.calls.length).toBeGreaterThan(50);
  });

  it("keeps drawing across every projection the rooms use", () => {
    for (const projection of ["globe", "core", "layers", "moons", "eclipse", "atlas"] as const) {
      recorder = recordingContext();
      frameCallbacks = [];

      const view = render(<World book={BOOK} projection={projection} />);
      advance(1000);
      advance(1016);

      expect(
        recorder.calls.length,
        `projection ${projection} drew nothing`,
      ).toBeGreaterThan(50);
      view.unmount();
    }
  });

  it("still draws a body whose book retained no figures", () => {
    // An empty book is the state a fresh checkout renders. Drawing must
    // not depend on measurements that a run may legitimately lack.
    render(
      <World
        book={{ idle: null, outstanding: null, loss: null, utilization: null }}
        projection="globe"
      />,
    );
    advance(1000);
    advance(1016);

    expect(recorder.calls).toContain("clearRect");
    expect(recorder.calls.length).toBeGreaterThan(50);
  });

  it("does not draw while the document is hidden", () => {
    const hidden = vi
      .spyOn(document, "hidden", "get")
      .mockReturnValue(true);

    render(<World book={BOOK} projection="globe" />);
    advance(1000);
    advance(1016);

    expect(recorder.calls).not.toContain("clearRect");
    hidden.mockRestore();
  });
});

/**
 * Loans reach the agent that took them, in that agent's colour.
 *
 * Every arc used to terminate at `nodes[0]` and stroke in the single
 * `palette.arc` jade, so a whole book converged on one point in one hue
 * and drew over itself. Five repayments looked like one line, which is
 * how the globe came to be described as having "only 1 line".
 */
const node = (id: string, latitude: number, longitude: number): WorldNode => ({
  id, label: id, capacity: "1000000000", score: 700, denied: false, latitude, longitude,
});
const arc = (agent: string, borrowed = "1000000000", loans = 1): LoanArc => ({
  agent, borrowed, loans, term_seconds: 864_000,
});

describe("the world draws one passage per loan", () => {
  const nodes = [node("borrower", 24, -36), node("sable", -30, 80), node("ember", 10, 150)];

  it("strokes a distinct colour for each agent", () => {
    render(
      <World
        book={BOOK}
        projection="globe"
        nodes={nodes}
        loans={[arc("borrower"), arc("sable"), arc("ember")]}
      />,
    );
    advance(1000);
    advance(1016);

    // The three agents' hues, as the light palette assigns them by index.
    const hues = [0, 1, 2].map(i => agentColor(i, false).toLowerCase());
    const used = new Set(recorder.strokes.map(s => s.toLowerCase()));
    for (const [i, hue] of hues.entries()) {
      expect(used.has(hue), `agent ${i} (${hue}) never stroked`).toBe(true);
    }
  });

  it("draws one passage per agent, not one per loan", () => {
    // The bug this replaces: an arc per loan put seventeen curves on
    // screen for five relationships, fanned apart by an invented lift.
    // An agent with six loans and an agent with one now draw the same
    // number of lines -- one each -- and differ in weight instead.
    render(
      <World
        book={BOOK}
        projection="globe"
        nodes={nodes}
        loans={[arc("borrower", "6000000000", 6), arc("sable", "1000000000", 1)]}
      />,
    );
    advance(1000);
    advance(1016);

    const hueA = agentColor(0, false).toLowerCase();
    const hueB = agentColor(1, false).toLowerCase();
    const strokes = recorder.strokes.map(s => s.toLowerCase());
    // Each agent's hue appears for its arc plus its hollow origin dot,
    // and the counts match despite a six-to-one difference in loans.
    expect(strokes.filter(s => s === hueA).length).toBe(
      strokes.filter(s => s === hueB).length,
    );
  });

  it("marks both ends of every passage", () => {
    // A line with no ends has no direction, and on a crowded globe it is
    // not obvious which nearby agent an arc actually reaches.
    const withEnds = render(
      <World book={BOOK} projection="globe" nodes={nodes} loans={[arc("sable")]} />,
    );
    advance(1000);
    advance(1016);
    const hue = agentColor(1, false).toLowerCase();
    // The destination dot is FILLED in the agent's hue; the origin is
    // hollow, so it strokes that hue instead.
    expect(recorder.fills.map(f => f.toLowerCase())).toContain(hue);
    expect(recorder.strokes.map(f => f.toLowerCase())).toContain(hue);
    withEnds.unmount();
  });

  it("draws nothing extra when no loans are supplied", () => {
    render(<World book={BOOK} projection="globe" nodes={nodes} loans={[]} />);
    advance(1000);
    advance(1016);
    const hues = [0, 1, 2].map(i => agentColor(i, false).toLowerCase());
    for (const hue of hues) {
      expect(recorder.strokes.map(s => s.toLowerCase())).not.toContain(hue);
    }
  });

  it("still draws a loan whose agent has no node", () => {
    // A cohort bundle that names an agent the population dropped must not
    // take the globe down; the arc falls back to the first node.
    render(
      <World book={BOOK} projection="globe" nodes={nodes} loans={[arc("ghost")]} />,
    );
    advance(1000);
    advance(1016);
    expect(recorder.calls.length).toBeGreaterThan(50);
  });
});
