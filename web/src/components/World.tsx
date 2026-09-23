import { useEffect, useRef, useState } from "react";
import { useMotionAllowed } from "./Motion";
import type { Assessment } from "../lib/data";
import type { BookState } from "../lib/book";
import type { Projection, WorldNode } from "../lib/observatory";
import { canvasResolution } from "../lib/canvas";
import { loanArc, sphereOccludes, spherePoint as point, traceVisiblePath, viewTransform, type Point3 } from "../lib/world-geometry";
import { worldPalette } from "../lib/world-palette";

const TAU = Math.PI * 2;
const rad = Math.PI / 180;
const dots = Array.from({ length: 2200 }, (_, i) => {
  const y = 1 - (i / 2199) * 2;
  const r = Math.sqrt(1 - y * y);
  const theta = i * Math.PI * (3 - Math.sqrt(5));
  return { x: Math.cos(theta) * r, y, z: Math.sin(theta) * r, grain: (Math.sin(i * 127.1) + 1) / 2 };
});

const latitudeLines = Array.from({ length: 29 }, (_, i) =>
  Array.from({ length: 121 }, (_, j) => ({ ...point(-84 + i * 6, -180 + j * 3), lat: -84 + i * 6, lon: -180 + j * 3 })));
const meridianLines = Array.from({ length: 30 }, (_, i) =>
  Array.from({ length: 61 }, (_, j) => ({ ...point(-90 + j * 3, -180 + i * 12), lat: -90 + j * 3, lon: -180 + i * 12 })));

/** A rendered instrument. Surface stippling is material, never extra agents. */
export function World({ book, nodes = [], loans = [], projection = "globe", selected, onSelect, replay = 0, paused = false, labels = true }: {
  book: BookState;
  nodes?: WorldNode[];
  loans?: Assessment[];
  projection?: Projection;
  selected?: string | undefined;
  onSelect?: ((id: string) => void) | undefined;
  replay?: number;
  paused?: boolean;
  labels?: boolean;
}) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const wrapper = useRef<HTMLDivElement>(null);
  const buttons = useRef(new Map<string, HTMLButtonElement>());
  const motion = useMotionAllowed();
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const latest = useRef({ book, nodes, loans, projection, selected, replay, paused, hovered, focused });
  latest.current = { book, nodes, loans, projection, selected, replay, paused, hovered, focused };

  useEffect(() => {
    const el = canvas.current;
    if (!el || typeof CanvasRenderingContext2D === "undefined") return;
    const ctx = el.getContext("2d");
    if (!ctx) return;
    let frame = 0;
    let resizeFrame = 0;
    let rotation = -0.22;
    let last = 0;
    let visible = true;
    let flatten = 0;
    let layer = 0;
    let xray = 0;
    let moon = 0;
    let atmosphere = 0;
    let core = 0;
    let replayStart = 0;
    let lastReplay = 0;
    let palette = worldPalette(false);
    const arcCache = new Map<string, Point3[]>();
    let theme = { machine: "#00706f", paper: "#f9f5ef", ink: "#1b1d1e", growth: "#4a6b3d", alarm: "#bf3a14" };
    const resize = () => {
      const pixels = canvasResolution(wrapper.current?.clientWidth ?? 800, window.devicePixelRatio);
      if (el.width !== pixels || el.height !== pixels) {
        el.width = pixels;
        el.height = pixels;
        ctx.setTransform(pixels / 800, 0, 0, pixels / 800, 0, 0);
      }
      const style = getComputedStyle(el);
      theme = { machine: style.getPropertyValue("--machine").trim(), paper: style.getPropertyValue("--bg").trim(), ink: style.getPropertyValue("--text").trim(), growth: style.getPropertyValue("--growth").trim(), alarm: style.getPropertyValue("--alarm").trim() };
      const preference = document.documentElement.dataset.theme;
      palette = worldPalette(preference === "dark" || (!preference && Boolean(scheme?.matches)));
    };
    // Defer writes until after the observer's delivery cycle, and observe
    // the CSS wrapper rather than the bitmap whose dimensions we change.
    const scheduleResize = () => {
      if (resizeFrame) return;
      resizeFrame = requestAnimationFrame(() => { resizeFrame = 0; resize(); });
    };
    const observer = typeof ResizeObserver !== "undefined" ? new ResizeObserver(scheduleResize) : null;
    if (wrapper.current) observer?.observe(wrapper.current);
    // Observe the WRAPPER, not the bitmap. The canvas is sized from its
    // container and, at narrow widths, its box can sit off-viewport
    // during first layout -- the observer then latched `false` and the
    // draw loop stopped for good, leaving a blank plate on every room.
    // A margin keeps a body that deliberately bleeds past an edge in
    // view, and `visible` only ever gates work, never correctness.
    const intersection = typeof IntersectionObserver !== "undefined"
      ? new IntersectionObserver(
          entries => { visible = entries.some(e => e.isIntersecting); },
          { rootMargin: "200px" },
        )
      : null;
    intersection?.observe(wrapper.current ?? el);
    const themeObserver = new MutationObserver(scheduleResize);
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    const scheme = window.matchMedia?.("(prefers-color-scheme: dark)");
    scheme?.addEventListener("change", scheduleResize);
    resize();

    const draw = (now: number) => {
      frame = requestAnimationFrame(draw);
      // A 32ms gate pinned this to exactly 30fps on a 60Hz display: rAF
      // delivers every 16.7ms, so the second frame of every pair was
      // always 16.7ms short and got dropped. On a body that rotates
      // continuously that halves the frame rate and reads as judder.
      // rAF already limits itself to the display's refresh, and `dt`
      // below keeps rotation and the projection lerps time-correct at
      // any rate, so the gate only needs to reject the degenerate
      // zero-delta frame.
      if (!visible || document.hidden || now - last < 1) return;
      const dt = Math.min((now - (last || now)) / 1000, 0.1);
      last = now;
      const state = latest.current;
      if (motion && !state.paused && !state.hovered && !state.focused) rotation += dt * TAU / 18;
      const lerp = motion ? Math.min(1, dt * 5) : 1;
      flatten += ((state.projection === "atlas" ? 1 : 0) - flatten) * lerp;
      layer += ((state.projection === "layers" ? 1 : 0) - layer) * lerp;
      xray += ((state.projection === "core" ? 1 : 0) - xray) * lerp;
      moon += ((state.projection === "moons" ? 1 : 0) - moon) * lerp;
      const health = state.book.utilization === null ? 0 : 1 - state.book.utilization;
      atmosphere += (health - atmosphere) * (motion ? Math.min(1, dt / 2) : 1);
      const knownCash = state.book.idle !== null && state.book.outstanding !== null;
      const cash = knownCash && state.book.idle! + state.book.outstanding! > 0n ? Number(state.book.idle! * 10000n / (state.book.idle! + state.book.outstanding!)) / 10000 : 0;
      core += (cash - core) * (motion ? Math.min(1, dt / 2) : 1);
      if (state.replay !== lastReplay) { replayStart = now; lastReplay = state.replay; }
      const replaying = lastReplay > 0 && now - replayStart < 2000;
      const replayProgress = motion ? Math.min(1, (now - replayStart) / 2000) : 1;
      ctx.clearRect(0, 0, 800, 800);
      const cx = 400 - moon * 110;
      const cy = 400;
      const radius = 258 - moon * 98;
      const rotate = viewTransform(rotation);
      const project = (p: { x: number; y: number; z: number }, lat?: number, lon?: number) => {
        const view = rotate(p);
        const longitude = lon ?? Math.atan2(p.x, p.z) / rad;
        const latitude = lat ?? Math.asin(Math.max(-1, Math.min(1, p.y / Math.hypot(p.x, p.y, p.z)))) / rad;
        return { x: (cx + view.x * radius) * (1 - flatten) + (400 + longitude / 180 * 335) * flatten,
          y: (cy - view.y * radius - p.y * layer * 65) * (1 - flatten) + (400 - latitude / 90 * 205) * flatten, z: view.z * (1 - flatten) + flatten, view };
      };
      const circle = (x: number, y: number, r: number) => { ctx.beginPath(); ctx.arc(x, y, Math.max(0, r), 0, TAU); };

      // The survey rings belong to the instrument, not to market counts.
      ctx.save();
      ctx.strokeStyle = theme.ink;
      ctx.globalAlpha = palette.dark ? .16 : .10;
      ctx.lineWidth = 0.7;
      ctx.setLineDash([1, 6]);
      circle(400, 400, 340); ctx.stroke();
      ctx.setLineDash([]);
      for (let i = 0; i < 120; i++) {
        const a = i / 120 * TAU;
        ctx.beginPath(); ctx.moveTo(400 + Math.cos(a) * 343, 400 + Math.sin(a) * 343);
        ctx.lineTo(400 + Math.cos(a) * (i % 10 === 0 ? 354 : 347), 400 + Math.sin(a) * (i % 10 === 0 ? 354 : 347)); ctx.stroke();
      }
      ctx.restore();

      // The facility orbit passes behind the body, rather than cutting across its face.
      if (moon > .001 && state.book.idle !== null) {
        ctx.save(); ctx.globalAlpha = moon * .24; ctx.strokeStyle = theme.ink; ctx.lineWidth = .65;
        ctx.beginPath(); ctx.ellipse(390, 400, 270, 117, -.22, 0, TAU); ctx.stroke(); ctx.restore();
      }

      if (flatten < 0.99) {
        ctx.save();
        ctx.globalAlpha = 1 - flatten;
        const glow = ctx.createRadialGradient(cx, cy, radius * 0.88, cx, cy, radius + 16 + atmosphere * 38);
        glow.addColorStop(0, "transparent"); glow.addColorStop(0.5, `rgba(${palette.atmosphere}, ${atmosphere * palette.haze})`); glow.addColorStop(1, "transparent");
        ctx.fillStyle = glow; circle(cx, cy, radius + 16 + atmosphere * 38); ctx.fill();
        const shell = ctx.createRadialGradient(cx - radius * 0.43, cy - radius * 0.43, 0, cx + radius * 0.12, cy + radius * 0.25, radius * 1.4);
        [0, .25, .55, .85, 1].forEach((stop, i) => shell.addColorStop(stop, palette.shell[i]!));
        ctx.globalAlpha = (1 - flatten) * (1 - xray * 0.77 - layer * 0.45);
        ctx.fillStyle = shell; circle(cx, cy, radius); ctx.fill();
        ctx.strokeStyle = palette.rim; ctx.lineWidth = palette.dark ? 1 : .65; ctx.stroke();
        if (core > 0.001) {
          ctx.globalAlpha = (1 - flatten) * core;
          const heart = ctx.createRadialGradient(cx - 25, cy - 15, 0, cx - 25, cy - 15, radius * 0.86);
          [0, .065, .2, .55].forEach((stop, i) => heart.addColorStop(stop, palette.core[i]!)); heart.addColorStop(1, "transparent");
          ctx.fillStyle = heart; circle(cx - 25, cy - 15, radius * 0.86); ctx.fill();
        }
        ctx.restore();
      }

      // Fine latitude contours and meridians form the shell material.
      ctx.lineWidth = 0.55;
      for (const [lines, color] of [[latitudeLines, palette.contour], [meridianLines, palette.meridian]] as const) {
        for (const line of lines) {
          ctx.beginPath();
          traceVisiblePath(ctx, line.map(v => { const p = project(v, v.lat, v.lon); return { ...p, visible: p.z >= 0 }; }));
          ctx.strokeStyle = flatten > .5 ? theme.machine : color;
          ctx.globalAlpha = ((palette.dark ? .13 : .19) + flatten * .09) * (1 - xray * .3); ctx.stroke();
        }
      }
      for (const dot of dots) {
        const p = project(dot);
        if (p.z < 0) continue;
        const wave = Math.sin(dot.x * 11 + dot.y * 8) * Math.cos(dot.z * 9 + dot.y * 5);
        ctx.globalAlpha = (0.12 + Math.pow(Math.max(0, p.z), 1.4) * (palette.dark ? .42 : .25)) * (0.25 + dot.grain * 0.75) * (1 - xray * 0.6);
        ctx.fillStyle = flatten > 0.5 ? theme.machine : palette.grain[wave > .15 ? 0 : 1];
        circle(p.x, p.y, wave > 0.15 ? 0.95 : 0.5); ctx.fill();
      }
      ctx.globalAlpha = 1;

      if (layer > 0.001) {
        ctx.save();
        ctx.globalAlpha = layer * 0.85;
        for (let i = 0; i < 4; i++) {
          const ringY = cy - 114 + i * 76;
          const ringRadius = radius * (0.72 + (i % 2) * 0.1);
          ctx.beginPath();
          ctx.ellipse(cx, ringY, ringRadius, 44, -0.08, 0, TAU);
          ctx.fillStyle = "rgba(121, 158, 143, 0.08)";
          ctx.fill();
          ctx.strokeStyle = theme.machine;
          ctx.lineWidth = 1.15;
          ctx.stroke();
        }
        ctx.restore();
      }

      // A retained origination draws from the lender pole to this borrower.
      // These are replay paths, not claims of currently live chain activity.
      const shownLoans = state.loans.slice(-12);
      const peak = Math.max(1, ...shownLoans.map(a => Number(a.payload.outcome.approved_principal ?? "0")));
      for (const [i, loan] of shownLoans.entries()) {
        const tenor = loan.payload.outcome.terms?.term_seconds ?? 0;
        const borrower = state.nodes[0];
        const lat = borrower?.latitude ?? 24, lon = borrower?.longitude ?? -36;
        const key = `${tenor}/${lat}/${lon}`;
        let arc = arcCache.get(key);
        if (!arc) {
          arc = loanArc(point(-22, 60), point(lat, lon), .14 + Math.min(.6, Math.max(0, tenor) / 86400 / 90 * .6));
          if (arcCache.size > 24) arcCache.clear();
          arcCache.set(key, arc);
        }
        const path = arc.map(v => { const p = project(v); return { ...p, visible: !sphereOccludes(p.view) }; });
        ctx.strokeStyle = flatten > 0.5 ? theme.machine : palette.arc;
        ctx.lineWidth = 0.5 + Number(loan.payload.outcome.approved_principal ?? "0") / peak * 1.1;
        const opacity = .65 * (1 - moon * .5);
        const transparency = Math.max(flatten, xray * .25, layer * .2);
        if (transparency > .001) {
          ctx.globalAlpha = opacity * transparency;
          ctx.beginPath(); traceVisiblePath(ctx, path.map(p => ({ ...p, visible: !p.visible }))); ctx.stroke();
        }
        ctx.globalAlpha = opacity;
        ctx.beginPath(); traceVisiblePath(ctx, path); ctx.stroke();
        if (replaying && i === shownLoans.length - 1) {
          const p = path[Math.min(path.length - 1, Math.round(replayProgress * (path.length - 1)))]!;
          ctx.globalAlpha = p.visible ? 1 : transparency;
          ctx.fillStyle = palette.pulse; ctx.shadowColor = palette.arc; ctx.shadowBlur = palette.dark ? 14 : 4;
          circle(p.x, p.y, 3); ctx.fill(); ctx.shadowBlur = 0;
        }
      }
      ctx.globalAlpha = 1;

      if (state.projection === "eclipse" && state.book.utilization !== null) {
        const u = state.book.utilization;
        ctx.fillStyle = palette.eclipse; ctx.globalAlpha = 0.96;
        circle(cx + radius * 2 * (1 - u), cy + 8, radius); ctx.fill(); ctx.globalAlpha = 1;
      }
      if (moon > 0.001 && state.book.idle !== null) {
        ctx.globalAlpha = moon;
        const m = ctx.createRadialGradient(608, 330, 0, 635, 355, 49);
        m.addColorStop(0, palette.moon[0]); m.addColorStop(0.42, palette.moon[1]); m.addColorStop(1, palette.moon[2]);
        ctx.fillStyle = m; circle(628, 347, 43); ctx.fill();
        ctx.globalAlpha = 1;
      }

      const nodePeak = Math.max(1, ...state.nodes.map(n => Number(n.capacity)));
      for (const node of state.nodes) {
        const p = project(point(node.latitude, node.longitude), node.latitude, node.longitude);
        const button = buttons.current.get(node.id);
        const nodeRadius = node.denied ? 4 : Math.max(2, Math.sqrt(Number(node.capacity) / nodePeak) * 7);
        if (button) {
          button.style.left = `${p.x / 8}%`; button.style.top = `${p.y / 8}%`;
          const hidden = p.z < 0;
          button.style.opacity = hidden ? "0" : "1";
          button.style.pointerEvents = hidden ? "none" : "";
          button.tabIndex = hidden ? -1 : 0;
          button.setAttribute("aria-hidden", String(hidden));
          button.style.setProperty("--node-size", `${nodeRadius * 2}px`);
        }
      }
    };
    frame = requestAnimationFrame(draw);
    return () => { cancelAnimationFrame(frame); cancelAnimationFrame(resizeFrame); observer?.disconnect(); intersection?.disconnect(); themeObserver.disconnect(); scheme?.removeEventListener("change", scheduleResize); };
  }, [motion]);

  return (
    <div className={`credit-world world-${projection} ${labels ? "" : "world-whisper"}`} ref={wrapper} style={{ position: "relative", width: "100%", aspectRatio: "1 / 1", minWidth: 0 }} onPointerEnter={() => setHovered(true)} onPointerLeave={() => setHovered(false)} onFocus={() => setFocused(true)} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false); }}>
      <canvas ref={canvas} width={800} height={800} className="world-canvas" style={{ position: "absolute", inset: 0, width: "100%", height: "100%" }} role="img" aria-label={`Credit body, ${projection} projection. ${book.utilization === null ? "Book measurements unavailable." : `${(book.utilization * 100).toFixed(1)}% utilization.`} ${nodes.length} ${nodes.length === 1 ? "profile" : "profiles"}. Surface texture is illustrative.`} />
      {nodes.map((node, i) => <button key={node.id} ref={el => { if (el) buttons.current.set(node.id, el); else buttons.current.delete(node.id); }} type="button" className={`world-node ${node.denied ? "node-hollow" : ""} ${selected === node.id ? "node-selected" : ""} ${node.score !== null && node.score >= 800 ? "node-exceptional" : ""} ${35 + i * 18 > 60 ? "node-flip" : ""}`} style={{ left: `${35 + i * 18}%`, top: `${36 + i * 12}%` }} aria-label={`${node.label}, score ${node.score ?? "unmeasured"}${node.denied ? ", denied" : ""}`} aria-pressed={selected === node.id} onClick={() => onSelect?.(node.id)}>
        <span className="node-point" /><span className="node-label">{node.label}<small>{node.denied ? "RESTRICTED" : node.score !== null ? `SCORE ${node.score}` : "UNMEASURED"}</small></span>
      </button>)}
      {projection === "layers" && <div className="world-layer-labels"><span>Identity</span><span>Reputation</span><span>Escrow</span><span>Settlement</span></div>}
      {projection === "moons" && book.idle !== null && <button type="button" className="moon-satellite" onClick={() => onSelect?.("facility")} aria-label="Dock with the retained facility">Open facility</button>}
    </div>
  );
}
