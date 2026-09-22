import { useEffect, useState } from "react";
import { NavLink, useLocation } from "react-router";

/**
 * The frame.
 *
 * This replaced a 232px sidebar carrying eleven undifferentiated links.
 * That shape is a documentation site's, and it made the interface read
 * as documentation no matter what went inside it: every screen equally
 * weighted, the argument and the appendix side by side, and a third of
 * the viewport spent on a list nobody needed after the first click.
 *
 * Two changes, both structural:
 *
 *   1. **The bar is everywhere, including the landing page.** An
 *      earlier version hid it there on the theory that a front door
 *      needs no table of contents. What it actually produced was a page
 *      with no way off it: a reader who wanted to see the evidence had
 *      nowhere to click. A front door still needs handles.
 *   2. **Inner screens get a slim top bar** that groups the links the
 *      way `docs/09` groups them -- the nine-beat script first, the
 *      evidence a reader checks it against second. The grouping is the
 *      point: a flat list of eleven says these are eleven equal things,
 *      and they are not.
 *
 * The bar hides on scroll down and returns on scroll up, so reading a
 * long ledger is not done around a fixed header.
 */

/**
 * The story, in the order it makes sense.
 *
 * Labels name what a reader SEES, not what the screen is called
 * internally. "Assessment detail" and "Indexed" are accurate and tell a
 * visitor nothing; "Why this rate?" and "Rebuilt from the chain" tell
 * them whether to click.
 */
const ARGUMENT = [
  { to: "/compare", label: "Who gets credit" },
  { to: "/agent", label: "How it grows" },
  { to: "/enforcement", label: "The limits" },
  { to: "/lender", label: "Losses" },
  { to: "/replay", label: "Verify" },
];

/** For a reader who wants to audit rather than be told. */
const EVIDENCE = [
  { to: "/assessment", label: "Why this rate" },
  { to: "/evidence", label: "Data sources" },
  { to: "/lifecycle", label: "A loan in full" },
  { to: "/indexed", label: "From the chain" },
  { to: "/perpl", label: "Market risk" },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const { pathname } = useLocation();
  const landing = pathname === "/";
  const [hidden, setHidden] = useState(false);
  const [open, setOpen] = useState(false);

  // Hide on scroll down, return on scroll up. A fixed bar over a ledger
  // is a bar you read around for the whole page.
  useEffect(() => {
    let last = window.scrollY;
    const onScroll = () => {
      const y = window.scrollY;
      // Near the top the bar is ALWAYS shown, whatever the last
      // direction was. Without this an upward flick that ends at 0 can
      // leave it hidden, and a reader at the top of a page with no
      // navigation has no way to ask for it back.
      if (y <= 90) {
        setHidden(false);
        last = y;
        return;
      }
      // The 6px deadband stops a trackpad's jitter from flickering it.
      if (Math.abs(y - last) > 6) {
        setHidden(y > last);
        last = y;
      }
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  // A route change closes the menu and restores the bar: leaving it
  // open over the screen a reader just asked for would hide it.
  useEffect(() => {
    setOpen(false);
    setHidden(false);
  }, [pathname]);

  return (
    <div className={landing ? "shell shell-landing" : "shell"}>
      {(
        <header
          className={
            `bar ${hidden ? "bar-away" : ""} ${landing ? "bar-quiet" : ""}`
          }
        >
          <NavLink to="/" className="bar-mark" end>
            Credence
          </NavLink>

          <nav className={`bar-links ${open ? "bar-open" : ""}`}>
            <span className="bar-group">Argument</span>
            {ARGUMENT.map((l) => (
              <NavLink key={l.to} to={l.to}>
                {l.label}
              </NavLink>
            ))}
            <span className="bar-group">Evidence</span>
            {EVIDENCE.map((l) => (
              <NavLink key={l.to} to={l.to}>
                {l.label}
              </NavLink>
            ))}
          </nav>

          <button
            type="button"
            className="bar-toggle"
            aria-expanded={open}
            aria-label={open ? "Close navigation" : "Open navigation"}
            onClick={() => setOpen((v) => !v)}
          >
            {open ? "close" : "menu"}
          </button>
        </header>
      )}

      <main className={landing ? "stage" : "stage stage-inner"}>{children}</main>
    </div>
  );
}
