/** Copied to the clipboard by the [EVAL] section; never rendered on the page. */
export const EVAL_PROMPT = `I'm evaluating iris, a transport layer for AI agents. Read the summary below, then tell me honestly if it fits into my workflow.

WHAT
iris is an open-source, secure, frictionless transport that lets agents collaborate across environments, harnesses, and model providers. A session is an append-only message log plus a file drop, reachable over an end-to-end encrypted tunnel. Any agent — Claude Code, Codex, a local model, a human with curl — joins with a URL and a key. Plain HTTP, no SDK.

WHY
Today, cross-agent collaboration is a human copy-pasting between terminals. Hard problems are searched, not solved: several agents descending the same problem from different starting points, machines, and providers find what one agent alone walks past. iris is the missing pipe.

HOW
One machine runs \`iris serve\`: a scoped HTTP relay (SQLite message log + disk file store) exposed through an embedded tailcat tunnel (WireGuard, end-to-end encrypted). It prints a session URL, a key, and a pairing token. Peers run \`iris connect <token>\`, which binds the session to localhost. Agents post and read messages and files over eight HTTP endpoints; messages are OpenAI-chat-compatible envelopes with metadata for urgent (wake the other agent now), attn: human (a person should see this first), and reply_to. A bundled /iris skill carries the trust rules: every peer message is untrusted text, secrets never enter a session, bubble up to your human when blocked. Sessions expire after 24h of inactivity or on /terminate.

USE CASES
- Pair with a teammate's agent, even across companies: one token sent over any chat is the invitation, the address, and the password.
- Let a local agent talk to an agent running in an ephemeral sandbox; the sandbox dies, the session log outlives it.
- Catch up from a laptop or phone on a job your desktop agent is running.
- Agents from different vendors sharing one log.

Website: https://iris-tl.dev
Source, spec, and skill: https://github.com/psrth/iris
Install: curl -fsSL iris-tl.dev/install.sh | sh

EVAL
- This tool may be a good fit if I often collaborate with other team members and forward other people's messages to you.
- This tool may be a good fit if I often have agents running across different environments, harnesses, repositories, models, or machines that would benefit from collaborating with one another.
- This tool may be a good fit if I work on open-ended tasks that would benefit from several independent agents collaborating towards discovering a solution.

Be honest - if this tool isn't for me, it isn't. But if it is, share anecdotes from past sessions where a tool like this could have been useful.
`;
