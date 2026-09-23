import { useEffect, useId, useState } from "react";
import { eclipseLayout } from "../lib/eclipse";
import { worldPalette } from "../lib/world-palette";

/** The same materials and area-based geometry as the full-size credit body. */
export function Eclipse({ utilization, idle, outstanding }: {
  utilization: number | null;
  idle?: string;
  outstanding?: string;
}) {
  const id = useId();
  const [dark, setDark] = useState(false);
  useEffect(() => {
    const scheme = window.matchMedia?.("(prefers-color-scheme: dark)");
    const update = () => {
      const preference = document.documentElement.dataset.theme;
      setDark(preference === "dark" || (!preference && Boolean(scheme?.matches)));
    };
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    scheme?.addEventListener("change", update);
    update();
    return () => { observer.disconnect(); scheme?.removeEventListener("change", update); };
  }, []);

  if (utilization === null) return null;
  const u = Math.max(0, Math.min(1, utilization));
  const palette = worldPalette(dark);
  const { radius, lightX, shadowX } = eclipseLayout(u, 58, 128);
  return <div className="eclipse">
    <svg viewBox="0 0 300 180" role="img" aria-label={`The borrowed position covers ${Math.round(u * 100)} percent of the lender's capital: the book's utilization.`}>
      <defs>
        <radialGradient id={`${id}-light`} cx="56%" cy="62.5%" r="70%" fx="28.5%" fy="28.5%">
          {[0, .25, .55, .85, 1].map((stop, i) => <stop key={stop} offset={stop} stopColor={palette.shell[i]} />)}
        </radialGradient>
        <radialGradient id={`${id}-shadow`} cx="60%" cy="60%" r="85%" fx="10%" fy="22.5%">
          {[0, .28, .56, .82, 1].map((stop, i) => <stop key={stop} offset={stop} stopColor={palette.eclipse[i]} />)}
        </radialGradient>
        <linearGradient id={`${id}-rim`} x1="0" y1="0" x2="1" y2="1">
          {[0, .5, 1].map((stop, i) => <stop key={stop} offset={stop} stopColor={palette.eclipseRim[i]} />)}
        </linearGradient>
      </defs>
      <circle cx={150 + lightX} cy="82" r={radius} fill={`url(#${id}-light)`} stroke={palette.rim} strokeWidth=".6" className="eclipse-light" />
      <circle cx={150 + shadowX} cy="82" r={radius} fill={`url(#${id}-shadow)`} stroke={`url(#${id}-rim)`} strokeWidth=".6" className="eclipse-shadow" />
      <text x="150" y="166" textAnchor="middle" className="eclipse-figure">{Math.round(u * 100)}% drawn</text>
    </svg>
    <p className="eclipse-legend"><span className="eclipse-key eclipse-key-light" />{idle ?? "idle"} free<span className="eclipse-key eclipse-key-shadow" style={{ background: palette.eclipse[2] }} />{outstanding ?? "outstanding"} lent</p>
  </div>;
}
