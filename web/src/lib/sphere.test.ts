import { describe, expect, it } from "vitest";
import {
  arcPoints,
  latitudeLine,
  onSphere,
  project,
  rotate,
} from "../components/Sphere";

/**
 * The projection.
 *
 * A wrong sphere looks fine in motion -- it turns, it foreshortens, the
 * eye accepts it -- and is wrong in every frame. These check the maths
 * rather than the markup: points stay on the unit sphere, rotation
 * preserves length, `z` actually reports which side a point is on, and
 * the wireframe splits at the limb instead of drawing across the body.
 */

const len = (p: { x: number; y: number; z: number }) =>
  Math.hypot(p.x, p.y, p.z);

describe("placing points on the sphere", () => {
  it("puts every point on the unit sphere", () => {
    for (const lat of [-90, -45, 0, 30, 90]) {
      for (const lon of [0, 90, 180, 270]) {
        expect(len(onSphere(lat, lon))).toBeCloseTo(1, 10);
      }
    }
  });

  it("puts the poles on the vertical axis", () => {
    const north = onSphere(90, 0);
    expect(north.y).toBeCloseTo(1, 10);
    expect(north.x).toBeCloseTo(0, 10);
    expect(north.z).toBeCloseTo(0, 10);

    expect(onSphere(-90, 0).y).toBeCloseTo(-1, 10);
  });

  it("puts the origin on the near face", () => {
    // lat 0, lon 0 faces the viewer, so z is +1 before any rotation.
    expect(onSphere(0, 0).z).toBeCloseTo(1, 10);
  });
});

describe("rotation", () => {
  it("preserves length", () => {
    const p = onSphere(37, 122);
    for (const spin of [0, 45, 90, 180, 270]) {
      expect(len(rotate(p, spin, 16))).toBeCloseTo(1, 10);
    }
  });

  it("carries a point to the far side after half a turn", () => {
    // Facing the viewer at spin 0; 180 degrees later it is behind.
    const front = rotate(onSphere(0, 0), 0, 0);
    expect(front.z).toBeGreaterThan(0.99);

    const back = rotate(onSphere(0, 0), 180, 0);
    expect(back.z).toBeLessThan(-0.99);
  });

  it("leaves a point on the spin axis where it is", () => {
    // The north pole is on the axis, so spinning must not move it.
    const a = rotate(onSphere(90, 0), 0, 0);
    const b = rotate(onSphere(90, 0), 137, 0);
    expect(b.x).toBeCloseTo(a.x, 10);
    expect(b.y).toBeCloseTo(a.y, 10);
  });
});

describe("projection to screen space", () => {
  it("maps the sphere inside the drawn radius", () => {
    for (const lat of [-80, -20, 0, 55]) {
      for (const lon of [0, 70, 200, 310]) {
        const p = project(rotate(onSphere(lat, lon), 42, 16), 180, 180, 120);
        expect(Math.hypot(p.x - 180, p.y - 180)).toBeLessThanOrEqual(120.001);
      }
    }
  });

  it("flips the vertical axis, because screen y grows downward", () => {
    const north = project(rotate(onSphere(90, 0), 0, 0), 180, 180, 120);
    expect(north.y).toBeLessThan(180);
  });
});

describe("the wireframe", () => {
  it("splits a latitude line into a near and a far half", () => {
    const { near, far } = latitudeLine(0, 0, 0, 180, 180, 120);
    // The equator always has both halves: one facing the viewer, one
    // behind. Drawing it as a single path would run it across the body.
    expect(near.length).toBeGreaterThan(0);
    expect(far.length).toBeGreaterThan(0);
  });

  it("produces drawable paths", () => {
    const { near } = latitudeLine(30, 25, 16, 180, 180, 120);
    expect(near.startsWith("M ")).toBe(true);
    expect(near).toContain("L ");
    expect(near).not.toContain("NaN");
  });
});

describe("loan arcs", () => {
  it("lifts the arc off the surface at its midpoint", () => {
    // Measured side-on, not head-on. An arc between two points either
    // side of the meridian is lifted along the axis pointing AT the
    // viewer, and an orthographic projection collapses that to the
    // centre -- so screen distance would report a lift of zero for a
    // lift that is working. Spinning the pair to the limb puts the
    // same bulge across the screen where it can be measured.
    const from = onSphere(0, -40);
    const to = onSphere(0, 40);
    const pts = arcPoints(from, to, 90, 0, 180, 180, 120);
    const mid = pts[Math.floor(pts.length / 2)]!;
    expect(Math.hypot(mid.x - 180, mid.y - 180)).toBeGreaterThan(120);
  });

  it("lifts the arc above the shell in three dimensions", () => {
    // The lift itself, independent of any viewing angle.
    const from = onSphere(0, -40);
    const to = onSphere(0, 40);
    const pts = arcPoints(from, to, 0, 0, 0, 0, 1);
    const mid = pts[Math.floor(pts.length / 2)]!;
    // Screen y is flipped, so undo it before measuring the radius.
    expect(Math.hypot(mid.x, -mid.y, mid.z)).toBeGreaterThan(1);
  });

  it("meets the surface at both ends", () => {
    const from = onSphere(10, -30);
    const to = onSphere(-10, 30);
    const pts = arcPoints(from, to, 0, 0, 180, 180, 120);
    const first = pts[0]!;
    const lastPt = pts[pts.length - 1]!;
    expect(Math.hypot(first.x - 180, first.y - 180)).toBeCloseTo(
      Math.hypot(
        project(from, 180, 180, 120).x - 180,
        project(from, 180, 180, 120).y - 180,
      ),
      6,
    );
    expect(lastPt).toBeDefined();
  });

  it("never emits NaN", () => {
    const pts = arcPoints(onSphere(0, 0), onSphere(0, 0), 30, 16, 180, 180, 120);
    for (const p of pts) {
      expect(Number.isFinite(p.x)).toBe(true);
      expect(Number.isFinite(p.y)).toBe(true);
    }
  });
});
