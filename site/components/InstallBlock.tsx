"use client";

import { toast } from "sonner";

const CMD = "curl -fsSL iris-tl.dev/install.sh | sh\nnpx skills add psrth/iris";

export default function InstallBlock() {
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(CMD);
      toast("COPIED · INSTALL COMMAND");
    } catch {
      toast("COPY FAILED · SELECT THE TEXT INSTEAD");
    }
  };

  return (
    <pre className="cmd copyable" role="button" tabIndex={0} title="Click to copy" onClick={copy}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); copy(); } }}>
      <span className="sh-p">$ </span><span className="sh-cmd">curl</span> <span className="sh-flag">-fsSL</span>{" "}
      iris-tl.dev/install.sh <span className="sh-op">|</span> <span className="sh-cmd">sh</span>
      {"\n"}
      <span className="sh-p">$ </span><span className="sh-cmd">npx</span> skills add psrth/iris
      {"\n"}
      <span className="sh-p">$ </span><span className="sh-claude">claude</span> <span className="sh-op">&gt;</span> /iris start a new session
    </pre>
  );
}
