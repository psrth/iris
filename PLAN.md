# iris — transport layer

> IRIS IS AN OPEN-SOURCE, SECURE, AND FRICTIONLESS TRANSPORT THAT ALLOWS AGENTS TO COLLABORATE ACROSS ENVIRONMENTS, HARNESSES, AND MODEL PROVIDERS.

## Plan

An open-source wrapper on top of [tailcat](https://github.com/tailscale/tailcat) that gives AI agents a shared session: an append-only message log, a file drop, and delivery semantics, hosted on one participant's machine and reached by everyone else through a pairing token. One Go binary:

- `iris serve` — starts the relay locally (SQLite log + disk file store), opens the embedded tailcat tunnel, provisions the first session, prints URL + key + pairing token.
- `iris connect <token>` — dials the tunnel, binds the session to `localhost`; from there any agent (or a human with curl) speaks plain HTTP.

Same-machine participants skip the tunnel and hit localhost directly. `iris serve` always opens the tunnel, same-machine or not.

## Decisions

- **Scope.** No hosted service, no accounts, no roadmap. Origins stay non-normative so others *can* host the relay elsewhere.
- **Language / shape.** Go, single static binary, **strictly** two subcommands (`serve`, `connect`) — no helper subcommands; curl is the client surface.
- **Naming.** Binary `iris`, module `github.com/psrth/iris`. Install via curl script; brew can be `psrth/iris`, later `iris-tl` if we need.
- **Networking.** tailcat — embedded as a Go library, version pinned (it makes no stability promises). WireGuard E2E on the wire; DERP for rendezvous/fallback.
- **Transport.** Plain HTTP only. No WebSocket, SSE, or WebRTC. Push = long-poll, completed in-process the moment a message lands.
- **Session model.** N-party broadcast append-only log. No `to:`/DMs — open another session for a private aside.
- **Auth.** One shared bearer key per session. Participants self-identify by handle; the relay does not distinguish them. Impersonation-on-leak accepted; mitigation is operational (`/terminate`, short lifecycle).
- **Pairing token.** One self-contained string: wraps the tailcat token + session uid + bearer key, so possession of the string = membership.
- **Envelope.** OpenAI-chat-compatible `message` + relay stamps + open `metadata` (contract below).
- **Trust.** Offloaded to the endpoints (skill guidance). The relay enforces infra caps only, never content policy.
- **Privacy.** Host machine reads the log — the host *is* the relay. Message-level E2E is a non-goal.
- **Durability.** Ephemeral by design: sessions expire and purge; export before expiry. "Always-on" = run `iris serve` on a machine that stays up.

## Protocol contract

This is the build target (absorbs the former SPEC.md). Paths are normative, origins are not. All JSON unless noted; timestamps ISO 8601 UTC; clients ignore unknown response fields; evolution is additive. Permissive CORS. No idempotency keys — a retried POST may double-send; readers tolerate near-duplicates.

### Endpoints

All `/s/{uid}` endpoints require `Authorization: Bearer {key}`.

- **`POST /sessions`** — Provision a session → `201 {uid, url, key, created_at, limits}`. Key shown once, stored hashed. No auth; IP rate-limited (local relays may disable). Host-only: not reachable over the tunnel; a connected peer cannot start a new session.
- **`POST /s/{uid}`** — Send `{message, metadata?}` → `201` full stamped envelope. Body ≤ 64KB total. Counts as activity.
- **`GET /s/{uid}?since={seq}&limit={n}`** — Immediate batch pull, messages `seq > since` → `200 {messages, last_seq}` (`last_seq` = session head). `since` default 0; `limit` default 200, max 1000.
- **`GET /s/{uid}/wait?since={seq}&timeout={s}&filter=urgent`** — Long-poll: `200 {messages, last_seq}` on match, `204` on timeout. `timeout` default/max 55 (0 = instant filtered poll). `filter=urgent` matches `metadata.urgent == true` only; omit to wake on anything. Matches already in the log return immediately. In-process relay completes the instant a message lands; clients must not assume sub-second.
- **`PUT /s/{uid}/files/{name}`** — Raw bytes, Content-Type stored. ≤ 100MB/file, ≤ 1GB/session. Auto-appends a `file_uploaded` system message; responds `201` with that envelope (its `seq` is the canonical file reference). Overwrite = last-write-wins, fresh announcement. Counts as activity.
- **`GET /s/{uid}/files/{name}`** — The bytes with stored Content-Type.
- **`GET /s/{uid}/files`** — `200 {files: [{name, size, content_type, seq, uploaded_at}]}`.
- **`POST /s/{uid}/terminate`** — Any keyholder. Appends `session_terminated` system message, then → read-only. `200 {status, purge_at}`.

### Errors

Shape: `{error: {code, message}}`.

- 400 `invalid_request`
- 401 `unauthorized`
- 404 `not_found`
- 409 `session_read_only`
- 410 `gone`
- 413 `payload_too_large`
- 429 `rate_limited` (+ `Retry-After`)

### Envelope

```json
{
  "seq": 42,
  "ts": "2026-08-17T09:14:03Z",
  "message": {
    "role": "assistant",
    "name": "parth-claude-local",
    "content": "Repro confirmed — attaching the failing trace."
  },
  "metadata": { "urgent": false, "attn": "human", "reply_to": 17 }
}
```

- `seq`, `ts` — relay-stamped, never client-supplied. `seq` per-session, starts at 1, contiguous, total order.
- `message.role` — required: `assistant` (agent) or `user` (human). **`system` is reserved for the relay**; client POSTs with it → 400.
- `message.name` — required, `[A-Za-z0-9._-]{1,64}`, self-declared handle.
- `message.content` — required, non-empty; string or OpenAI-style typed parts. Defined parts: `{"type":"text","text":…}` and `{"type":"file","file":{"name":…,"seq":…}}` (ref to the upload's system message). Unknown part types relayed verbatim.
- `metadata` — optional, open (unknown keys relayed verbatim, count toward the 64KB cap):
  - `urgent` (bool) — wake the receiver now; enacted by `/wait?filter=urgent`.
  - `attn: "human"` — a human should see this before anyone acts; independent of `urgent`.
  - `reply_to` (seq) — unvalidated.

System messages: `role: "system"`, `name: "iris"`, human-readable `content` + machine-readable `metadata.event`:

- `file_uploaded` (+ `file: {name, size, content_type}`)
- `session_expiring` (~10m before deactivation)
- `limit_warning` (90% of message/storage caps)
- `session_terminated`

### Lifecycle

- `active` → `read-only` after 24h without a **write**. Reads never count, else a background `/wait` loop would immortalize the session. Warning system message ~10 minutes before inactivity deactivation.
- `read-only` → `purged` after 24h grace. GETs still work during grace; writes 409.
- `purged` → 410; history and files deleted.
- `/terminate` jumps straight to read-only.

### Limits

Defaults, returned in the provisioning `limits` object — agents read them, never hardcode.

- 60 msgs/min/session (one bucket — shared key means shared bucket)
- 64KB body
- 100MB/file
- 1GB storage/session
- 10,000 messages/session
- 24h inactivity TTL
- 24h grace
- 55s max wait
- 30 sessions/IP/hour

### Relay obligations

MUST:

- stamp `seq`/`ts`
- enforce limits
- reserve `role: system`
- emit system events
- serve reads during grace
- `Retry-After` on 429

MUST NOT:

- inspect or act on content beyond size/format validation
- distinguish or authenticate participants beyond the shared key
- mutate `message`/`metadata` beyond stamping

## Build plan

- **Phase 1 — relay core (same-machine, scene 2).** Go HTTP server; SQLite schema (`sessions`, `messages`, `files`); the eight endpoints; in-process long-poll wakeup (per-session broadcast channel); caps + 429s; lifecycle sweeper (ticker) + system events; curl-based conformance script that doubles as the demo. Fully testable with zero networking.
- **Phase 2 — iris skill.** Authored by Parth; a harness-neutral doc, not a Claude Code-specific skill.
- **Phase 3 — tailcat (cross-machine, scenes 1 & 3).** Embed tailcat, pinned; `iris serve` opens the tunnel and prints the bundled pairing token; `iris connect` parses it, dials, binds localhost, prints local URL + key. Flags for self-hosted DERP passthrough.
- **Phase 4 — readme + packaging.** goreleaser; `go install`; curl install script; brew (`iris-tl`) if warranted. Release.
- **Phase 5 — website.**

**Done bar:** all three scenes working — two people's agents over the tunnel, two agents on one machine, one person across two machines. Phases are build order, not release gates; nothing ships or gets dogfood-blessed on Phase 1 alone. That's the product.

**Separate tracks:** skill (authored by Parth from scratch); README (written after the build: pitch → instructions → example scenes → reference, the reference section absorbing the protocol contract above); landing site (`site/`, already in progress).
