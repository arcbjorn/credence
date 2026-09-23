import { useEffect, useRef, useState } from "react";
import { Link, NavLink, useLocation } from "react-router";
import { PRIMARY_LINKS, screenForPath, sectionForScreen } from "../lib/navigation";
import { RecordLinks } from "./RecordLinks";

export function BrandMark() {
  return <svg viewBox="0 0 32 32" fill="none" aria-hidden="true"><circle cx="16" cy="16" r="12" stroke="currentColor" strokeWidth="1.2" /><ellipse cx="16" cy="16" rx="5.4" ry="12" stroke="currentColor" strokeWidth="1.1" transform="rotate(34 16 16)" /><path d="M4 16h24M7 8.5c5 4 13 4 18 0M7 23.5c5-4 13-4 18 0" stroke="currentColor" strokeWidth=".8" /><circle cx="25.5" cy="8.5" r="2.7" fill="var(--bg)" stroke="currentColor" /></svg>;
}

export function Shell({ children }: { children: React.ReactNode }) {
  const { pathname } = useLocation();
  const screen = screenForPath(pathname);
  const section = sectionForScreen(screen);
  const landing = screen.layout === "landing";
  const room = screen.layout === "world";
  const record = Boolean(section && screen.path !== section.path);
  const [open, setOpen] = useState(false);
  const [whisper, setWhisper] = useState(false);
  const [dark, setDark] = useState(() => {
    try { return localStorage.getItem("credence-theme") === "dark"; } catch { return false; }
  });
  const menuButton = useRef<HTMLButtonElement>(null);
  const header = useRef<HTMLElement>(null);
  const previousPath = useRef(pathname);

  useEffect(() => {
    setOpen(false);
    // Preserve a restored scroll position on load, but enter a new screen
    // at its beginning. The header itself never changes with scroll.
    if (previousPath.current !== pathname) {
      document.documentElement.scrollTop = 0;
      document.body.scrollTop = 0;
      previousPath.current = pathname;
    }
    document.title = `${screen.title} — Credence`;
  }, [pathname, screen.title]);

  useEffect(() => {
    document.documentElement.dataset.theme = dark ? "dark" : "light";
    try { localStorage.setItem("credence-theme", dark ? "dark" : "light"); } catch { /* Storage is optional. */ }
  }, [dark]);

  useEffect(() => {
    const down = (event: KeyboardEvent) => {
      if (event.key === "Alt") setWhisper(true);
      if (event.key === "Escape" && open) {
        setOpen(false);
        menuButton.current?.focus();
      }
    };
    const up = (event: KeyboardEvent) => { if (event.key === "Alt") setWhisper(false); };
    const reset = () => setWhisper(false);
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    window.addEventListener("blur", reset);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
      window.removeEventListener("blur", reset);
    };
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const dismiss = (event: PointerEvent) => {
      if (event.target instanceof Node && !header.current?.contains(event.target)) setOpen(false);
    };
    document.addEventListener("pointerdown", dismiss);
    return () => document.removeEventListener("pointerdown", dismiss);
  }, [open]);

  const leaveMenu = () => {
    setOpen(false);
    if (open) document.getElementById("main-content")?.focus({ preventScroll: true });
  };

  return (
    <div className={`shell ${landing ? "shell-landing" : ""} ${room ? "shell-room" : ""} ${whisper ? "whisper" : ""}`}>
      <a className="skip-link" href="#main-content" onClick={event => {
        event.preventDefault();
        document.getElementById("main-content")?.focus();
      }}>Skip to content</a>

      <header className="bar" ref={header}>
        <NavLink to="/" className="bar-mark" end aria-label="Credence home" onClick={leaveMenu}>
          <BrandMark /><span>credence<span className="wordmark-period">.</span></span>
        </NavLink>
        <nav aria-label="Primary navigation" id="primary-navigation" className={`bar-links ${open ? "bar-open" : ""}`}>
          {PRIMARY_LINKS.map(item => {
            const active = screen.path === item.path || (screen.path !== "/log" && section?.path === item.path);
            return (
              <Link
                key={item.path}
                to={item.path}
                className={active ? "active" : undefined}
                aria-current={active ? (screen.path === item.path ? "page" : "location") : undefined}
                onClick={leaveMenu}
              >{item.label}</Link>
            );
          })}
        </nav>
        <div className="bar-actions">
          <button type="button" className="action action-icon theme-switch" aria-label={dark ? "Use paper theme" : "Use night theme"} onClick={() => setDark(!dark)}>
            <span aria-hidden="true">◐</span>
          </button>
          <button ref={menuButton} type="button" className="action bar-toggle" aria-expanded={open} aria-controls="primary-navigation" aria-label={open ? "Close navigation" : "Open navigation"} onClick={() => setOpen(!open)}>
            {open ? "Close" : "Menu"}
          </button>
        </div>
      </header>

      <main id="main-content" tabIndex={-1} className={`stage ${landing || room ? "" : "stage-inner"}`}>
        {record && !room && section && (
          <nav className="record-breadcrumb" aria-label="Breadcrumb">
            <ol>
              <li><Link to={section.path}>{section.label}</Link></li>
            </ol>
          </nav>
        )}
        {children}
        {screen.layout === "document" && section && <RecordLinks section={section} currentPath={screen.path} />}
      </main>

    </div>
  );
}
