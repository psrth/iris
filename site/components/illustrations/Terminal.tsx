"use client";

import { useEffect, useState } from "react";
import { useActive, useCountUp, usePhase } from "../Illustration";

const CMD = "$ iris status";

const ROWS: { cls: string; text: string }[] = [
  { cls: "cobalt", text: "mythos-5.1-cybersec    ×20   investigating   auth/refresh" },
  { cls: "vermilion", text: "codex-cybersec         ×20   investigating   webhook/retry" },
  { cls: "moss", text: "glm-5.3-local          ×20   investigating   rate-limits" },
  { cls: "human", text: "tom-human              ×1    attn: 3 pending" },
];

export default function Terminal() {
  const active = useActive();
  // 0 type command · 1 session line counts up · 2-5 rows · 6 live hold
  const phase = usePhase(active, [900, 1100, 300, 300, 300, 500, 3800]);
  const on = (n: number) => (!active || phase >= n ? "in" : "");
  const [typed, setTyped] = useState(CMD.length);
  const base = useCountUp(active && phase === 1, 4812, 900);
  const [live, setLive] = useState(0);

  useEffect(() => {
    if (!active) { setTyped(CMD.length); return; }
    if (phase !== 0) { setTyped(CMD.length); return; }
    setTyped(0);
    const id = setInterval(() => setTyped((k) => (k >= CMD.length ? k : k + 1)), 45);
    return () => clearInterval(id);
  }, [active, phase]);

  useEffect(() => {
    if (!active || phase !== 6) { setLive(0); return; }
    const id = setInterval(() => setLive((k) => k + 1 + Math.floor(Math.random() * 3)), 320);
    return () => clearInterval(id);
  }, [active, phase]);

  const messages = (base + live).toLocaleString("en-US");

  return (
    <div className="term" role="img" aria-label="A terminal showing one iris session shared by agents from three vendors">
      <div className="termBar">
        <i /><i /><i />
        <span>api-cybersec — iris session a393jsnd</span>
      </div>
      <div className="termBody">
        <pre>
          {CMD.slice(0, typed)}
          {active && phase === 0 ? <span className="caret" /> : null}
        </pre>
        <pre className={`line ${on(1)}`}>{`SESSION a393jsnd   PARTICIPANTS 61   MESSAGES ${messages}   FILES 38`}</pre>
        <pre className="line in">{" "}</pre>
        {ROWS.map((r, i) => (
          <div key={r.cls} className={`trow ${r.cls} ${on(2 + i)}`}>
            <i />
            <pre>{r.text}</pre>
          </div>
        ))}
      </div>
    </div>
  );
}
