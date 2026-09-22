import { useEffect, useState } from "react";
import { Link, NavLink, useLocation } from "react-router";

const ROOMS = [
  { to: "/observatory", label: "Observatory", bearing: "N" },
  { to: "/atlas", label: "Atlas", bearing: "E" },
  { to: "/moons", label: "Moons", bearing: "S" },
  { to: "/body", label: "Body", bearing: "W" },
];
const RECORDS = [
  ["/agent", "Growth"], ["/compare", "Profiles"], ["/assessment", "Assessment"],
  ["/lifecycle", "Arc"], ["/lender", "Core"], ["/enforcement", "Limits"],
  ["/evidence", "Evidence"], ["/indexed", "Chain log"], ["/perpl", "Weather"], ["/replay", "Replay"],
];

export function BrandMark() {
  return <svg viewBox="0 0 32 32" fill="none" aria-hidden="true"><circle cx="16" cy="16" r="12" stroke="currentColor" strokeWidth="1.2" /><ellipse cx="16" cy="16" rx="5.4" ry="12" stroke="currentColor" strokeWidth="1.1" transform="rotate(34 16 16)" /><path d="M4 16h24M7 8.5c5 4 13 4 18 0M7 23.5c5-4 13-4 18 0" stroke="currentColor" strokeWidth=".8" /><circle cx="25.5" cy="8.5" r="2.7" fill="var(--bg)" stroke="currentColor" /></svg>;
}

export function Shell({ children }: { children: React.ReactNode }) {
  const { pathname } = useLocation();
  const landing = pathname === "/";
  const room = ROOMS.some(r => r.to === pathname) || pathname === "/log";
  const [atTop, setAtTop] = useState(true);
  const [open, setOpen] = useState(false);
  const [whisper, setWhisper] = useState(false);
  const [dark, setDark] = useState(() => {
    try { return localStorage.getItem("credence-theme") === "dark"; } catch { return false; }
  });

  useEffect(() => {
    const onScroll = () => setAtTop(window.scrollY <= 24);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);
  useEffect(() => {
    setOpen(false);
    setAtTop(window.scrollY <= 24);
    document.documentElement.scrollTop = 0;
    document.body.scrollTop = 0;
    const name = pathname === "/" ? "The credit body" : pathname.slice(1).replace(/^./, c => c.toUpperCase());
    document.title = `${name} — Credence`;
  }, [pathname]);
  useEffect(() => {
    document.documentElement.dataset.theme = dark ? "dark" : "light";
    try { localStorage.setItem("credence-theme", dark ? "dark" : "light"); } catch { /* Storage is optional. */ }
  }, [dark]);
  useEffect(() => {
    const down = (e: KeyboardEvent) => { if (e.key === "Alt") setWhisper(true); if (e.key === "Escape") setOpen(false); };
    const up = (e: KeyboardEvent) => { if (e.key === "Alt") setWhisper(false); };
    const reset = () => setWhisper(false);
    window.addEventListener("keydown", down); window.addEventListener("keyup", up); window.addEventListener("blur", reset);
    return () => { window.removeEventListener("keydown", down); window.removeEventListener("keyup", up); window.removeEventListener("blur", reset); };
  }, []);

  return <div className={`shell ${landing ? "shell-landing" : ""} ${room ? "shell-room" : ""} ${whisper ? "whisper" : ""}`}>
    <a className="skip-link" href="#main-content" onClick={event => { event.preventDefault(); document.getElementById("main-content")?.focus(); }}>Skip to content</a>
    <header className={`bar ${landing && atTop ? "bar-quiet" : ""}`}>
      <NavLink to="/" className="bar-mark" end aria-label="Credence home"><BrandMark /><span>credence<span className="wordmark-period">.</span></span></NavLink>
      <nav aria-label="Primary navigation" id="primary-navigation" className={`bar-links ${open ? "bar-open" : ""}`}>
        {landing ? <><Link to="/observatory">The protocol</Link><Link to="/guide">Field guide <span>↗</span></Link></> : ROOMS.map(r => <NavLink to={r.to} key={r.to}><span className="nav-bearing">{r.bearing}</span>{r.label}</NavLink>)}
      </nav>
      <div className="bar-actions">
        <button type="button" className="theme-switch" aria-label={dark ? "Use paper theme" : "Use night theme"} onClick={() => setDark(!dark)}><span aria-hidden="true">◐</span></button>
        {landing ? <Link className="bar-enter" to="/observatory">Enter the atlas <span>↗</span></Link> : <Link className="guide-link" to="/guide">Field guide ↗</Link>}
        <button type="button" className="bar-toggle" aria-expanded={open} aria-controls="primary-navigation" aria-label={open ? "Close navigation" : "Open navigation"} onClick={() => setOpen(!open)}>{open ? "Close" : "Menu"}</button>
      </div>
    </header>
    <main id="main-content" tabIndex={-1} className={`stage ${landing || room ? "" : "stage-inner"}`}>
      {!landing && !room && <div className="record-heading"><Link to="/observatory">Observatory</Link><span>/</span><span>Field records</span></div>}
      {!landing && !room && pathname !== "/guide" && <nav className="record-nav" aria-label="Field records">{RECORDS.map(([to, label]) => <NavLink key={to} to={to!}>{label}</NavLink>)}</nav>}
      {children}
    </main>
    {!landing && !room && <footer className="inner-footer"><Link to="/observatory">Return to the observatory ↗</Link><span>Credit has a memory.</span></footer>}
  </div>;
}
