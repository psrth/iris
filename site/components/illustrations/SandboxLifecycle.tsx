"use client";

import { useActive, useCountUp, usePhase } from "../Illustration";

export default function SandboxLifecycle() {
  const active = useActive();
  // 0 spawned · 1 working · 2 purged · 3 log persists · 4 hold
  const phase = usePhase(active, [900, 1500, 900, 1700, 2200]);
  const on = (n: number) => (!active || phase >= n ? "on" : "");
  const msgs = useCountUp(active && phase === 3, 41, 1200, 12);

  return (
    <svg className="life" viewBox="0 0 560 150" role="img" aria-label="A sandbox is spawned, works, and is purged; the session log persists">
      {/* spawned */}
      <rect className={`box ${on(0)}`} x="0.5" y="0.5" width="149" height="83" />
      <circle className={`hollow ${on(0)}`} cx="75" cy="42" r="4" />
      <text className={`cap ${on(0)}`} x="0" y="108">14:02 · SPAWNED</text>

      <path className={`arrow ${on(1)}`} d="M172 42h18m-4-4l4 4-4 4" />

      {/* working */}
      <rect className={`box ${on(1)}`} x="205.5" y="0.5" width="149" height="83" />
      <circle className={`solid ${on(1)}`} cx="226" cy="30" r="4" />
      <line className={`bar ${on(1)}`} x1="238" y1="30" x2="302" y2="30" style={{ transitionDelay: "150ms" }} />
      <line className={`bar ${on(1)}`} x1="238" y1="42" x2="330" y2="42" style={{ transitionDelay: "350ms" }} />
      <line className={`bar ${on(1)}`} x1="238" y1="54" x2="286" y2="54" style={{ transitionDelay: "550ms" }} />
      <text className={`cap ${on(1)}`} x="205" y="108">WORKING · SEQ 12 → 41</text>

      <path className={`arrow ${on(2)}`} d="M377 42h18m-4-4l4 4-4 4" />

      {/* purged */}
      <rect className={`box faint ${on(2)}`} x="410.5" y="0.5" width="149" height="83" />
      <text className={`cap faint ${on(2)}`} x="410" y="108">16:42 · PURGED</text>

      {/* the log outlives the sandbox */}
      <text className={`cap strong ${on(3)}`} x="0" y="138">SESSION LOG · HOST</text>
      <line className={`logline ${on(3)}`} x1="132" y1="134" x2="372" y2="134" pathLength={1} />
      <text className={`cap ${on(3)}`} x="560" y="138" textAnchor="end">{msgs} MSGS · 3 FILES · PERSIST</text>
    </svg>
  );
}
