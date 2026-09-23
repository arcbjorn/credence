import { useEffect, useRef, useState } from "react";
import { useMotionAllowed } from "./Motion";
import type { BookState } from "../lib/book";
import { POOL_ID, POOL_POSITION, type LoanArc, type Projection, type WorldNode } from "../lib/observatory";
import { canvasResolution } from "../lib/canvas";
import { loanArc, sphereOccludes, spherePoint as point, traceVisiblePath, viewTransform, type Point3 } from "../lib/world-geometry";
import { agentColor, worldPalette } from "../lib/world-palette";
import { eclipseCovers, eclipseLayout } from "../lib/eclipse";

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
  loans?: LoanArc[];
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
    let eclipse = 0;
    let eclipseUtilization = latest.current.book.utilization ?? 0;
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
      eclipse += ((state.projection === "eclipse" && state.book.utilization !== null ? 1 : 0) - eclipse) * lerp;
      eclipseUtilization += ((state.book.utilization ?? eclipseUtilization) - eclipseUtilization) * (motion ? Math.min(1, dt / 2) : 1);
      const health = state.book.utilization === null ? 0 : 1 - state.book.utilization;
      atmosphere += (health - atmosphere) * (motion ? Math.min(1, dt / 2) : 1);
      const knownCash = state.book.idle !== null && state.book.outstanding !== null;
      const cash = knownCash && state.book.idle! + state.book.outstanding! > 0n ? Number(state.book.idle! * 10000n / (state.book.idle! + state.book.outstanding!)) / 10000 : 0;
      core += (cash - core) * (motion ? Math.min(1, dt / 2) : 1);
      if (state.replay !== lastReplay) { replayStart = now; lastReplay = state.replay; }
      const replaying = lastReplay > 0 && now - replayStart < 2000;
      const replayProgress = motion ? Math.min(1, (now - replayStart) / 2000) : 1;
      ctx.clearRect(0, 0, 800, 800);
      const alignment = eclipseLayout(eclipseUtilization);
      const cx = 400 - moon * 110 + alignment.lightX * eclipse;
      const cy = 400;
      const radius = 258 - moon * 98 + (alignment.radius - 258) * eclipse;
      const shadowX = 400 + alignment.shadowX;
      // First make room for the second body, then reveal its material.
      const shadowOpacity = Math.max(0, (eclipse - .2) / .8);
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

      // One passage per agent: from the pool to the borrower it lends to.
      //
      // This drew one arc PER LOAN, which put seventeen curves on screen
      // for five relationships -- a six-deep fan at one agent and a
      // single line at another, where the density was just the loan count
      // and read as noise. How much and how often belong in the dock, as
      // figures; the line only has to say who borrows from whom.
      const shownLoans = state.loans;
      const peak = Math.max(1, ...shownLoans.map(l => Number(l.borrowed)));
      const hueOf = new Map(state.nodes.map((n, i) => [n.id, i]));
      // The pool, if the population is carrying one. Falling back to the
      // old fixed coordinate keeps a book with no measured facility
      // drawing something rather than collapsing every arc to a point.
      const pool = state.nodes.find(n => n.id === POOL_ID);
      const from = point(pool?.latitude ?? POOL_POSITION.latitude, pool?.longitude ?? POOL_POSITION.longitude);
      for (const [i, loan] of shownLoans.entries()) {
        const borrower = state.nodes.find(n => n.id === loan.agent);
        // An agent with no node has nowhere to draw to. Skipping is
        // honest; the old fallback to `nodes[0]` drew someone else's
        // passage under this agent's name.
        if (!borrower) continue;
        const lat = borrower.latitude, lon = borrower.longitude;
        // Height is the term it borrowed for, which is a real property of
        // the passage. Nothing is added per repeat any more: that fan was
        // geometry invented to separate curves that should never have
        // been separate lines.
        const lift = .16 + Math.min(.5, Math.max(0, loan.term_seconds) / 86400 / 90 * .5);
        const key = `${Math.round(lift * 1000)}/${lat}/${lon}`;
        let arc = arcCache.get(key);
        if (!arc) {
          arc = loanArc(from, point(lat, lon), lift);
          if (arcCache.size > 64) arcCache.clear();
          arcCache.set(key, arc);
        }
        const path = arc.map(v => { const p = project(v); return { ...p, visible: !sphereOccludes(p.view) }; });
        const hue = agentColor(hueOf.get(loan.agent) ?? 0, palette.dark);
        ctx.strokeStyle = flatten > 0.5 ? theme.machine : hue;
        // Weight is TOTAL borrowed, so a thick line is a big relationship
        // rather than a busy one. `sqrt` because the range spans an order
        // of magnitude and a linear map leaves the small agents hairline.
        ctx.lineWidth = 0.8 + Math.sqrt(Number(loan.borrowed) / peak) * 1.6;
        const opacity = .7 * (1 - moon * .5) * (1 - eclipse * .55);
        const transparency = Math.max(flatten, xray * .25, layer * .2);
        if (transparency > .001) {
          ctx.globalAlpha = opacity * transparency;
          ctx.beginPath(); traceVisiblePath(ctx, path.map(p => ({ ...p, visible: !p.visible }))); ctx.stroke();
        }
        ctx.globalAlpha = opacity;
        ctx.beginPath(); traceVisiblePath(ctx, path); ctx.stroke();

        // The two ends of the passage: the pool it was drawn from, and
        // the agent it reached. The origin is hollow and the destination
        // filled, so direction survives greyscale rather than depending
        // on telling two ends of a curve apart by position.
        const ends = [
          { p: path[0], filled: false },
          { p: path[path.length - 1], filled: true },
        ];
        for (const { p, filled } of ends) {
          if (!p) continue;
          ctx.globalAlpha = (p.visible ? 1 : transparency) * opacity;
          if (ctx.globalAlpha < .004) continue;
          const r = filled ? 2.8 : 2.2;
          circle(p.x, p.y, r);
          if (filled) {
            ctx.fillStyle = hue;
            ctx.fill();
          } else {
            // A hollow dot needs the ground punched out under it, or the
            // arc's own stroke shows through and it reads as filled.
            ctx.fillStyle = palette.dark ? "#0b1413" : "#f7f3ec";
            ctx.fill();
            ctx.strokeStyle = hue;
            ctx.lineWidth = 1.1;
            ctx.stroke();
            ctx.strokeStyle = flatten > 0.5 ? theme.machine : hue;
          }
        }
        ctx.globalAlpha = opacity;

        if (replaying && i === shownLoans.length - 1) {
          const p = path[Math.min(path.length - 1, Math.round(replayProgress * (path.length - 1)))]!;
          ctx.globalAlpha = p.visible ? 1 : transparency;
          ctx.fillStyle = palette.pulse; ctx.shadowColor = hue; ctx.shadowBlur = palette.dark ? 14 : 4;
          circle(p.x, p.y, 3); ctx.fill(); ctx.shadowBlur = 0;
        }
      }
      ctx.globalAlpha = 1;

      if (shadowOpacity > .001) {
        ctx.save();
        ctx.globalAlpha = shadowOpacity;
        // A softly lit, opaque surface, rather than a flat black overlay.
        const r = alignment.radius;
        const shade = ctx.createRadialGradient(shadowX - r * .8, cy - r * .55, 0, shadowX + r * .2, cy + r * .2, r * 1.7);
        [0, .28, .56, .82, 1].forEach((stop, i) => shade.addColorStop(stop, palette.eclipse[i]!));
        ctx.fillStyle = shade;
        circle(shadowX, cy, r); ctx.fill();
        const rim = ctx.createLinearGradient(shadowX - r, cy - r, shadowX + r, cy + r);
        [0, .5, 1].forEach((stop, i) => rim.addColorStop(stop, palette.eclipseRim[i]!));
        ctx.strokeStyle = rim; ctx.lineWidth = .8; ctx.stroke();
        ctx.restore();
      }
      if (moon > 0.001 && state.book.idle !== null) {
        ctx.globalAlpha = moon;
        const m = ctx.createRadialGradient(608, 330, 0, 635, 355, 49);
        m.addColorStop(0, palette.moon[0]); m.addColorStop(0.42, palette.moon[1]); m.addColorStop(1, palette.moon[2]);
        ctx.fillStyle = m; circle(628, 347, 43); ctx.fill();
        ctx.globalAlpha = 1;
      }

      // Agents are sized against each OTHER. The pool is excluded from the
      // scale: it holds the whole facility, an order of magnitude above
      // any single borrower, so letting it set the peak shrank every
      // agent to a third of its size and flattened the difference between them.
      const nodePeak = Math.max(1, ...state.nodes.filter(n => n.id !== POOL_ID).map(n => Number(n.capacity)));
      for (const node of state.nodes) {
        const p = project(point(node.latitude, node.longitude), node.latitude, node.longitude);
        const button = buttons.current.get(node.id);
        // The pool gets a fixed, deliberately larger radius rather than a
        // measured one: it is a different KIND of thing, and sizing it on
        // the same scale invites reading it as a very large agent.
        const nodeRadius = node.id === POOL_ID
          ? 9
          : node.denied ? 4 : Math.max(2, Math.sqrt(Number(node.capacity) / nodePeak) * 7);
        if (button) {
          button.style.left = `${p.x / 8}%`; button.style.top = `${p.y / 8}%`;
          const covered = eclipseCovers(p.x, p.y, shadowX, cy, alignment.radius);
          const hidden = p.z < 0 || (covered && shadowOpacity > .5);
          button.style.opacity = p.z < 0 ? "0" : String(covered ? 1 - shadowOpacity : 1);
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

  // The pool is the facility every passage leaves from, not a borrower.
  // Counting it here reported one more profile than the globe shows.
  const agentCount = nodes.filter(n => n.id !== POOL_ID).length;

  return (
    <div className={`credit-world world-${projection} ${labels ? "" : "world-whisper"}`} ref={wrapper} style={{ position: "relative", width: "100%", aspectRatio: "1 / 1", minWidth: 0 }} onPointerEnter={() => setHovered(true)} onPointerLeave={() => setHovered(false)} onFocus={() => setFocused(true)} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false); }}>
      <canvas ref={canvas} width={800} height={800} className="world-canvas" style={{ position: "absolute", inset: 0, width: "100%", height: "100%" }} role="img" aria-label={`Credit body, ${projection} projection. ${book.utilization === null ? "Book measurements unavailable." : `${(book.utilization * 100).toFixed(1)}% utilization.`} ${agentCount} ${agentCount === 1 ? "profile" : "profiles"}. Surface texture is illustrative.`} />
      {nodes.map((node, i) => <button key={node.id} ref={el => { if (el) buttons.current.set(node.id, el); else buttons.current.delete(node.id); }} type="button" className={`world-node ${node.id === POOL_ID ? "node-pool" : ""} ${node.denied ? "node-hollow" : ""} ${selected === node.id ? "node-selected" : ""} ${node.score !== null && node.score >= 800 ? "node-exceptional" : ""} ${35 + i * 18 > 60 ? "node-flip" : ""}`} style={{ left: `${35 + i * 18}%`, top: `${36 + i * 12}%`, ["--agent-hue" as string]: agentColor(i, false), ["--agent-hue-dark" as string]: agentColor(i, true) }} aria-label={node.id === POOL_ID ? `${node.label}, the facility every loan is drawn from` : `${node.label}, score ${node.score ?? "unmeasured"}${node.denied ? ", denied" : ""}`} aria-pressed={selected === node.id} onClick={() => onSelect?.(node.id)}>
        <span className="node-point" /><span className="node-label">{node.label}<small>{node.id === POOL_ID ? "THE POOL" : node.denied ? "RESTRICTED" : node.score !== null ? `SCORE ${node.score}` : "UNMEASURED"}</small></span>
      </button>)}
      {projection === "layers" && <div className="world-layer-labels"><span>Identity</span><span>Reputation</span><span>Escrow</span><span>Settlement</span></div>}
      {projection === "moons" && book.idle !== null && <button type="button" className="action action-small moon-satellite" onClick={() => onSelect?.("facility")} aria-label="Dock with the retained facility">Open facility</button>}
    </div>
  );
}
