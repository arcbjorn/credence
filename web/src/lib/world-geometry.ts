export interface Point3 { x: number; y: number; z: number }
export interface PathPoint { x: number; y: number; visible: boolean }
const RAD = Math.PI / 180;

export function spherePoint(latitude: number, longitude: number): Point3 {
  const lat = latitude * RAD, lon = longitude * RAD;
  return { x: Math.cos(lat) * Math.sin(lon), y: Math.sin(lat), z: Math.cos(lat) * Math.cos(lon) };
}

/** The shell, arcs and agents use the same camera transform. */
export function rotatePoint(p: Point3, yaw: number, tilt = -.21): Point3 {
  return viewTransform(yaw, tilt)(p);
}

export function viewTransform(yaw: number, tilt = -.21) {
  const cos = Math.cos(yaw), sin = Math.sin(yaw), ct = Math.cos(tilt), st = Math.sin(tilt);
  return (p: Point3): Point3 => {
    const x = p.x * cos + p.z * sin, z = p.z * cos - p.x * sin;
    return { x, y: p.y * ct - z * st, z: p.y * st + z * ct };
  };
}

/** A raised great-circle path anchored to the shell at both ends. */
export function loanArc(from: Point3, to: Point3, lift: number, samples = 96): Point3[] {
  const dot = Math.max(-1, Math.min(1, from.x * to.x + from.y * to.y + from.z * to.z));
  const angle = Math.acos(dot);
  let tangent = { x: to.x - dot * from.x, y: to.y - dot * from.y, z: to.z - dot * from.z };
  let length = Math.hypot(tangent.x, tangent.y, tangent.z);
  if (length < 1e-8) {
    // Coincident and antipodal coordinates still need a finite path.
    tangent = Math.abs(from.y) < .9 ? { x: -from.z, y: 0, z: from.x } : { x: from.y, y: -from.x, z: 0 };
    length = Math.hypot(tangent.x, tangent.y, tangent.z);
  }
  tangent = { x: tangent.x / length, y: tangent.y / length, z: tangent.z / length };
  const count = Math.max(2, Math.floor(samples));
  return Array.from({ length: count + 1 }, (_, i) => {
    if (i === 0) return { ...from };
    if (i === count) return { ...to };
    const t = i / count, r = 1 + Math.max(0, lift) * Math.sin(Math.PI * t);
    const a = Math.cos(angle * t), b = Math.sin(angle * t);
    return { x: (from.x * a + tangent.x * b) * r, y: (from.y * a + tangent.y * b) * r, z: (from.z * a + tangent.z * b) * r };
  });
}

/** Elevated arcs beyond the silhouette remain visible, even on the far side. */
export function sphereOccludes(p: Point3): boolean {
  const radial = p.x * p.x + p.y * p.y;
  return radial < 1 && p.z < Math.sqrt(1 - radial) - 1e-6;
}

/** Never connect two visible runs through an occluded stretch. */
export function traceVisiblePath(path: Pick<CanvasRenderingContext2D, "moveTo" | "lineTo">, points: PathPoint[]) {
  let drawing = false;
  for (const p of points) {
    if (!p.visible) { drawing = false; continue; }
    if (drawing) path.lineTo(p.x, p.y);
    else path.moveTo(p.x, p.y);
    drawing = true;
  }
}
