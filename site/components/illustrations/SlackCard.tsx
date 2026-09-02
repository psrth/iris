"use client";

import { useEffect, useState } from "react";
import { useActive, usePhase } from "../Illustration";

const MSG = "my claude thinks it might be an issue with the upstream DNS configuration, let's see what your codex thinks?";

export default function SlackCard() {
  const active = useActive();
  // 0 typing dots · 1 typewriter · 2 chip appears · 3 hold
  const phase = usePhase(active, [900, 2300, 700, 3200]);
  const [n, setN] = useState(MSG.length);

  useEffect(() => {
    if (!active) { setN(MSG.length); return; }
    if (phase === 0) { setN(0); return; }
    if (phase >= 2) { setN(MSG.length); return; }
    setN(0);
    const id = setInterval(() => setN((k) => (k >= MSG.length ? k : k + 1)), 20);
    return () => clearInterval(id);
  }, [active, phase]);

  const typing = active && phase === 0;
  const chip = !active || phase >= 2;

  return (
    <div className="slack">
      <div className="avatar">SR</div>
      <div className="slackBody">
        <div className="slackMeta">
          <b>sarah</b>
          <span>14:02</span>
        </div>
        <div className="typewrap">
          <p className="ghost" aria-hidden="true">{MSG}</p>
          {typing ? (
            <div className="typing overlay"><i /><i /><i /></div>
          ) : (
            <p className="live">
              {MSG.slice(0, n)}
              {active && phase === 1 && n < MSG.length ? <span className="caret" /> : null}
            </p>
          )}
        </div>
        <div className={`tokenChip ${chip ? "in" : ""}`}>
          <i className="dot" />
          tcomFwWCCcjS…
          <small>IRIS TOKEN</small>
        </div>
      </div>
    </div>
  );
}
