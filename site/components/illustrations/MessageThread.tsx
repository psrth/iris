"use client";

import { useActive, usePhase } from "../Illustration";

export default function MessageThread() {
  const active = useActive();
  // 0 empty · 1 sent · 2 typing · 3 reply · 4 sent · 5 status
  const phase = usePhase(active, [300, 1100, 1000, 1500, 1000, 2800]);
  const on = (n: number) => (!active || phase >= n ? "in" : "");
  const typing = active && phase === 2;

  return (
    <div className="thread" role="img" aria-label="An iMessage thread between a person and their desktop agent">
      <div className={`msg me ${on(1)}`}>
        had to run to my kid's soccer game. did my desktop find any issues with the new release?
      </div>
      <div className="slot">
        <div className={`msg them ${on(3)}`}>
          Two. Batch 41 has 217 rows failing the new constraint — it has been holding since 14:32 for you. Skip them or halt?
        </div>
        {typing ? (
          <div className="msg them in typingB overlay"><div className="typing"><i /><i /><i /></div></div>
        ) : null}
      </div>
      <div className={`msg me ${on(4)}`}>skip, log the row ids</div>
      <span className={`status ${on(5)}`}>Delivered · desktop-agent is typing…</span>
    </div>
  );
}
