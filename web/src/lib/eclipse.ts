/** Distance between equal circles whose covered area is utilization.
 * A linear distance is misleading: half a diameter covers only 39%.
 * Return a distance in radii, with zero meaning a total eclipse.
 */
export function eclipseDistance(utilization: number): number {
  const u = Math.max(0, Math.min(1, utilization));
  if (u === 0) return 2;
  if (u === 1) return 0;
  let low = 0, high = 2;
  for (let i = 0; i < 28; i++) {
    const d = (low + high) / 2;
    const covered = (2 * Math.acos(d / 2) - d * Math.sqrt(4 - d * d) / 2) / Math.PI;
    if (covered > u) low = d;
    else high = d;
  }
  return (low + high) / 2;
}

/** Fit both bodies within an instrument without clipping either silhouette. */
export function eclipseLayout(utilization: number, maximumRadius = 258, halfWidth = 320) {
  const distance = eclipseDistance(utilization);
  const radius = Math.min(maximumRadius, halfWidth / (1 + distance / 2));
  const offset = distance * radius;
  return { radius, lightX: -offset / 2, shadowX: offset / 2 };
}

export function eclipseCovers(x: number, y: number, shadowX: number, shadowY: number, radius: number) {
  return (x - shadowX) ** 2 + (y - shadowY) ** 2 < radius ** 2;
}
