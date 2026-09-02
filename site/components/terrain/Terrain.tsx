"use client";

import { useEffect, useRef, useState } from "react";
import { MAX_AGENTS, Palette, TerrainSim } from "./sim";

const MIN_CANVAS_WIDTH = 760;

function readPalette(): Palette {
  const cs = getComputedStyle(document.documentElement);
  const v = (n: string) => cs.getPropertyValue(n).trim();
  return {
    meshLine: v("--mesh-line"),
    meshFill: v("--mesh-fill"),
    ink: v("--ink"),
    muted: v("--muted"),
    dash: v("--dash"),
    agents: [v("--cobalt"), v("--vermilion"), v("--moss"), v("--amber"), v("--violet")],
    font: v("--font-mono") || "monospace",
  };
}

export default function Terrain() {
  const wrapRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const simRef = useRef<TerrainSim | null>(null);
  const [state, setState] = useState({ agents: 2, open: 1, total: 3 });

  useEffect(() => {
    const canvas = canvasRef.current;
    const wrap = wrapRef.current;
    if (!canvas || !wrap) return;
    const sim = new TerrainSim(canvas, readPalette());
    simRef.current = sim;
    const sync = () => setState({ agents: sim.agentCount, open: sim.openCount, total: sim.sandboxCount });
    sim.onChange = sync;
    const fit = () => {
      // On narrow screens draw the terrain wider than the viewport and crop the
      // ends, so the hills keep their height instead of shrinking to a strip.
      const width = Math.max(wrap.clientWidth, MIN_CANVAS_WIDTH);
      if (wrap.clientWidth > 0) sim.resize(width, TerrainSim.heightFor(width));
    };
    fit();
    sync();
    sim.start();
    const ro = new ResizeObserver(fit);
    ro.observe(wrap);
    const mo = new MutationObserver(() => sim.setPalette(readPalette()));
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    // labels are drawn with the web font; repaint the mesh once fonts are in
    document.fonts?.ready.then(() => sim.setPalette(readPalette()));
    return () => {
      sim.stop();
      ro.disconnect();
      mo.disconnect();
      simRef.current = null;
    };
  }, []);

  const local = (e: React.MouseEvent<HTMLCanvasElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    return [e.clientX - r.left, e.clientY - r.top] as const;
  };

  return (
    <section className="terrain" aria-label="Agents descending a loss surface">
      <div className="wide">
        <div ref={wrapRef} className="terrainWrap">
          <canvas
            ref={canvasRef}
            onClick={(e) => {
              const [x, y] = local(e);
              simRef.current?.unlockAt(x, y);
            }}
            onMouseMove={(e) => {
              const [x, y] = local(e);
              const hit = simRef.current ? simRef.current.lockedAt(x, y) >= 0 : false;
              e.currentTarget.style.cursor = hit ? "pointer" : "default";
            }}
          />
        </div>
      </div>
      <div className="lane">
        <div className="terrainBar">
          <div className="chips">
            <button className="chip" onClick={() => simRef.current?.addAgent()} disabled={state.agents >= MAX_AGENTS}>
              + AGENT
            </button>
            <button className="chip" onClick={() => simRef.current?.unlockNext()} disabled={state.open >= state.total}>
              + ENVIRONMENT
            </button>
          </div>
          <span className="readout">
            {state.agents}/{MAX_AGENTS} AGENTS · {state.open}/{state.total} ENVIRONMENTS
          </span>
        </div>
      </div>
    </section>
  );
}
