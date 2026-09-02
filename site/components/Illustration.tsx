"use client";

import { createContext, useContext, useEffect, useRef, useState } from "react";

const ActiveCtx = createContext(false);

/** True while the enclosing illustration is hovered or scrolled into view. */
export const useActive = () => useContext(ActiveCtx);

export function Illustration({ children, className = "" }: { children: React.ReactNode; className?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const [inView, setInView] = useState(false);
  const [hover, setHover] = useState(false);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(([e]) => setInView(e.isIntersecting), { threshold: 0.55 });
    io.observe(el);
    return () => io.disconnect();
  }, []);

  const active = hover || inView;
  return (
    <div
      ref={ref}
      className={`illus ${active ? "active" : ""} ${className}`}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <ActiveCtx.Provider value={active}>{children}</ActiveCtx.Provider>
    </div>
  );
}

/**
 * Cycles through phases 0..n-1 while active, holding each for durations[i] ms,
 * then loops. Resets to 0 when inactive.
 */
export function usePhase(active: boolean, durations: number[]): number {
  const [phase, setPhase] = useState(0);
  useEffect(() => {
    if (!active) {
      setPhase(0);
      return;
    }
    let i = 0;
    setPhase(0);
    let t = setTimeout(next, durations[0]);
    function next() {
      i = (i + 1) % durations.length;
      setPhase(i);
      t = setTimeout(next, durations[i]);
    }
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active]);
  return phase;
}

/** Counts from 0 to target over `ms` while `run` is true; returns target when not running. */
export function useCountUp(run: boolean, target: number, ms: number, from = 0): number {
  const [v, setV] = useState(target);
  useEffect(() => {
    if (!run) {
      setV(target);
      return;
    }
    let raf = 0;
    const t0 = performance.now();
    const tick = (t: number) => {
      const k = Math.min(1, (t - t0) / ms);
      const e = 1 - Math.pow(1 - k, 3);
      setV(Math.round(from + (target - from) * e));
      if (k < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [run, target, ms, from]);
  return v;
}
