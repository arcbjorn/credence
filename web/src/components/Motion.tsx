import { useEffect, useRef, useState } from "react";

/**
 * Motion that carries meaning, rather than decorating it.
 *
 * The rule this file follows: an animation here must correspond to
 * something the data actually does. A counter rises because the capacity
 * rose; a curve draws because the loans happened in that order. Motion
 * invented for its own sake would be the interface asserting energy the
 * evidence does not have -- the visual form of the thing `docs/09`
 * forbids when it bans collapsing evidence modes into one green light.
 *
 * Every hook here respects `prefers-reduced-motion`. The stylesheet has
 * a global guard, but that only kills CSS transitions: a JavaScript
 * counter would keep ticking, so each hook checks for itself and jumps
 * straight to the final value.
 */

/** Does this reader want motion? Checked once, and kept current. */
export function useMotionAllowed(): boolean {
  const [allowed, setAllowed] = useState(() => {
    if (typeof window === "undefined" || !window.matchMedia) return false;
    return !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  });

  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) return;
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setAllowed(!query.matches);
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);

  return allowed;
}

/**
 * Fires once when an element first enters the viewport.
 *
 * Returns `true` immediately when IntersectionObserver is unavailable:
 * a screen that stayed blank because an API was missing would be worse
 * than one that never animated.
 */
export function useInView<T extends Element>() {
  const ref = useRef<T | null>(null);
  const [seen, setSeen] = useState(false);

  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    if (typeof IntersectionObserver === "undefined") {
      setSeen(true);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            setSeen(true);
            observer.disconnect();
          }
        }
      },
      // Fires a little before the element is fully visible, so the
      // motion has started by the time a reader's eye arrives.
      { threshold: 0.25, rootMargin: "0px 0px -8% 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  return { ref, seen };
}

/** Ease-out cubic: fast first, settling. A figure that eased IN would
 *  read as hesitant, and these are numbers the system is sure of. */
function easeOut(t: number): number {
  return 1 - Math.pow(1 - t, 3);
}

/**
 * A figure that counts to its value when it scrolls into view.
 *
 * Owns its own element so the caller never threads a ref through. An
 * earlier draft exposed a `useCountUp` hook that smuggled its ref out by
 * assigning to the function object -- which works once and breaks the
 * moment two counters mount.
 *
 * The count is over the DISPLAYED magnitude, not base units: at 6
 * decimals a settlement figure is in the billions, and interpolating
 * there is a blur rather than a number anyone can follow.
 */
export function Counted({
  value,
  decimals = 0,
  className,
  suffix,
  prefix,
}: {
  value: number;
  decimals?: number;
  className?: string;
  suffix?: string;
  prefix?: string;
}) {
  const motion = useMotionAllowed();
  const { ref, seen } = useInView<HTMLSpanElement>();
  const [shown, setShown] = useState(motion ? 0 : value);

  useEffect(() => {
    if (!seen || !motion) {
      setShown(value);
      return;
    }
    let raf = 0;
    const started = performance.now();
    const step = (now: number) => {
      const t = Math.min(1, (now - started) / 900);
      setShown(value * easeOut(t));
      if (t < 1) raf = requestAnimationFrame(step);
      else setShown(value);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [seen, motion, value]);

  return (
    <span ref={ref} className={className}>
      {prefix}
      {shown.toLocaleString(undefined, {
        minimumFractionDigits: decimals,
        maximumFractionDigits: decimals,
      })}
      {suffix}
    </span>
  );
}
