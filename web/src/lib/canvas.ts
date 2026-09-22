/**
 * Bound the backing bitmap independently of the CSS layout. In particular,
 * never multiply a canvas's own intrinsic width and feed it back into its
 * layout: without its stylesheet that grows on every ResizeObserver event.
 */
export function canvasResolution(layoutWidth: number, deviceRatio: number): number {
  const width = Number.isFinite(layoutWidth) && layoutWidth > 0 ? layoutWidth : 800;
  const ratio = Number.isFinite(deviceRatio) && deviceRatio > 0 ? Math.min(deviceRatio, 2) : 1;
  return Math.max(1, Math.min(2048, Math.round(width * ratio)));
}
