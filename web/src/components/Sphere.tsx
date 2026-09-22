/**
 * The sphere's geometry.
 *
 * A real 3D projection, not a circle with curves drawn on it: points are
 * placed on a unit sphere, rotated, then projected orthographically. That
 * is what makes the wireframe foreshorten correctly near the limb and
 * what lets a point pass behind the body and be occluded rather than
 * drawn over it.
 *
 * Kept separate from the component so the maths is testable on its own.
 * A projection that silently mirrors a hemisphere is the kind of bug that
 * looks fine in motion and is wrong in every frame.
 */

export interface Vec3 {
  x: number;
  y: number;
  z: number;
}

/** A point on the unit sphere from latitude/longitude in degrees. */
export function onSphere(latDeg: number, lonDeg: number): Vec3 {
  const lat = (latDeg * Math.PI) / 180;
  const lon = (lonDeg * Math.PI) / 180;
  return {
    x: Math.cos(lat) * Math.sin(lon),
    y: Math.sin(lat),
    z: Math.cos(lat) * Math.cos(lon),
  };
}

/**
 * Spin about the vertical axis, then tilt toward the viewer.
 *
 * The tilt is what stops the poles sitting exactly on the silhouette,
 * which reads as a flat ring rather than a body.
 */
export function rotate(p: Vec3, spinDeg: number, tiltDeg: number): Vec3 {
  const s = (spinDeg * Math.PI) / 180;
  const t = (tiltDeg * Math.PI) / 180;

  const x1 = p.x * Math.cos(s) + p.z * Math.sin(s);
  const z1 = -p.x * Math.sin(s) + p.z * Math.cos(s);

  const y2 = p.y * Math.cos(t) - z1 * Math.sin(t);
  const z2 = p.y * Math.sin(t) + z1 * Math.cos(t);

  return { x: x1, y: y2, z: z2 };
}

/**
 * Orthographic projection to screen space.
 *
 * `z` is returned so callers can decide what is in front: anything with
 * `z < 0` is on the far side of the body.
 */
export function project(
  p: Vec3,
  cx: number,
  cy: number,
  r: number,
): { x: number; y: number; z: number } {
  return { x: cx + p.x * r, y: cy - p.y * r, z: p.z };
}

/**
 * A great-circle arc between two points on the sphere, as screen-space
 * samples.
 *
 * Interpolating the straight line between two 3D points and normalising
 * keeps the arc ON the surface. Lifting it by `lift` raises it off the
 * shell so a loan reads as an arc over the body rather than a scratch on
 * it.
 *
 * The lift is small on purpose. At 0.18 an arc between sites 80 degrees
 * apart peaked a fifth of a radius outside the body -- roughly 25px at
 * this size -- and hung off the limb like a loose wire. An arc should
 * clear the surface and still belong to the sphere.
 */
export function arcPoints(
  from: Vec3,
  to: Vec3,
  spin: number,
  tilt: number,
  cx: number,
  cy: number,
  r: number,
  lift = 0.06,
  samples = 40,
): { x: number; y: number; z: number }[] {
  const out: { x: number; y: number; z: number }[] = [];
  for (let i = 0; i <= samples; i++) {
    const t = i / samples;
    const x = from.x + (to.x - from.x) * t;
    const y = from.y + (to.y - from.y) * t;
    const z = from.z + (to.z - from.z) * t;
    const len = Math.hypot(x, y, z) || 1;
    // A half-sine bulge: zero at both ends, highest in the middle, so
    // the arc leaves and meets the surface cleanly.
    const h = 1 + lift * Math.sin(Math.PI * t);
    const unit = { x: (x / len) * h, y: (y / len) * h, z: (z / len) * h };
    out.push(project(rotate(unit, spin, tilt), cx, cy, r));
  }
  return out;
}

/** An SVG path through screen-space samples. */
export function toPath(pts: { x: number; y: number }[]): string {
  return pts
    .map((p, i) => `${i === 0 ? "M" : "L"} ${p.x.toFixed(2)} ${p.y.toFixed(2)}`)
    .join(" ");
}

/**
 * One line of latitude, as a screen path.
 *
 * Returned in segments split at the limb, so the half on the far side can
 * be drawn faintly instead of crossing the body.
 */
export function latitudeLine(
  latDeg: number,
  spin: number,
  tilt: number,
  cx: number,
  cy: number,
  r: number,
  steps = 72,
): { near: string; far: string } {
  const near: { x: number; y: number }[][] = [];
  const far: { x: number; y: number }[][] = [];
  let run: { x: number; y: number }[] = [];
  let side: boolean | null = null;

  for (let i = 0; i <= steps; i++) {
    const lon = (i / steps) * 360;
    const p = project(rotate(onSphere(latDeg, lon), spin, tilt), cx, cy, r);
    const isNear = p.z >= 0;
    if (side === null) side = isNear;
    if (isNear !== side) {
      (side ? near : far).push(run);
      run = [];
      side = isNear;
    }
    run.push(p);
  }
  if (run.length) (side ? near : far).push(run);

  return {
    near: near.map(toPath).join(" "),
    far: far.map(toPath).join(" "),
  };
}

/** One meridian, split at the limb the same way. */
export function meridianLine(
  lonDeg: number,
  spin: number,
  tilt: number,
  cx: number,
  cy: number,
  r: number,
  steps = 48,
): { near: string; far: string } {
  const near: { x: number; y: number }[][] = [];
  const far: { x: number; y: number }[][] = [];
  let run: { x: number; y: number }[] = [];
  let side: boolean | null = null;

  for (let i = 0; i <= steps; i++) {
    const lat = -90 + (i / steps) * 180;
    const p = project(rotate(onSphere(lat, lonDeg), spin, tilt), cx, cy, r);
    const isNear = p.z >= 0;
    if (side === null) side = isNear;
    if (isNear !== side) {
      (side ? near : far).push(run);
      run = [];
      side = isNear;
    }
    run.push(p);
  }
  if (run.length) (side ? near : far).push(run);

  return {
    near: near.map(toPath).join(" "),
    far: far.map(toPath).join(" "),
  };
}
