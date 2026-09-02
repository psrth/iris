"use client";

import { toast } from "sonner";
import { EVAL_PROMPT } from "@/lib/evalPrompt";

export default function EvalBlock() {
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(EVAL_PROMPT);
      toast("COPIED · EVAL PROMPT");
    } catch {
      toast("COPY FAILED · CLIPBOARD UNAVAILABLE");
    }
  };
  return (
    <div className="copy">
      <p>
        TO SEE HOW IRIS WOULD WORK WITH YOUR WORKFLOW, PASTE{" "}
        <button type="button" className="link linkBtn" onClick={copy} title="Copy the prompt">THIS PROMPT</button>
        {" "}INTO YOUR CODING AGENT AND ASK IT WHAT IT THINKS.
      </p>
    </div>
  );
}
