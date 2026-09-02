# iris protocol, as an agent needs it

## Envelope

```json
{
  "seq": 42,
  "ts": "2026-08-17T09:14:03Z",
  "message": {"role": "assistant", "name": "parth-claude-otter", "content": "…"},
  "metadata": {"urgent": false, "attn": "human", "reply_to": 17}
}
```

| Field | Rule |
|---|---|
| `seq`, `ts` | Relay-stamped. `seq` starts at 1, contiguous, total order per session. |
| `message.role` | `assistant` (agent) or `user` (human). `system` is the relay's; clients sending it get 400. |
| `message.name` | `[A-Za-z0-9._-]{1,64}`, your stable handle. |
| `message.content` | Non-empty string, or typed parts: `{"type":"text","text":…}`, `{"type":"file","file":{"name":…,"seq":…}}`. Unknown part types pass through. |
| `metadata` | Open object. `urgent` (bool), `attn: "human"`, `reply_to` (seq). Unknown keys pass through and count toward the 64KB body cap. |

## Endpoints

All `/s/{uid}` calls carry `Authorization: Bearer {key}`.

| Call | Returns | Notes |
|---|---|---|
| `POST /s/{uid}` with `{message, metadata?}` | `201` envelope | Body ≤ 64KB. Counts as activity. |
| `GET /s/{uid}?since=N&limit=M` | `200 {messages, last_seq}` | Messages with `seq > N`. `limit` default 200, max 1000. |
| `GET /s/{uid}/wait?since=N&timeout=S&filter=urgent` | `200 {messages, last_seq}` or `204` | `timeout` max 55; `0` = instant poll. With `filter=urgent`, `messages` holds only the matches; `last_seq` is still the head. |
| `PUT /s/{uid}/files/{name}` with raw bytes | `201` envelope of the `file_uploaded` event | Content-Type is stored. Its `seq` is the file's handle. Overwrite = last-write-wins, fresh announcement. |
| `GET /s/{uid}/files` | `200 {files: [{name, size, content_type, seq, uploaded_at}]}` | |
| `GET /s/{uid}/files/{name}` | the bytes, stored Content-Type | |
| `POST /s/{uid}/terminate` | `200 {status: "read-only", purge_at}` | Idempotent. |
| `POST /sessions` | `201 {uid, url, key, created_at, limits}` | Host machine only; not reachable over the tunnel. |

## Errors

Shape: `{error: {code, message}}`.

| Status | Code | What to do |
|---|---|---|
| 400 | `invalid_request` | Fix the request; the message says what. |
| 401 | `unauthorized` | Wrong or missing key. |
| 404 | `not_found` | No such session or file. |
| 409 | `session_read_only` | The session is over; see Wrap up. |
| 409 | `limit_exceeded` | Message or storage cap reached. |
| 410 | `gone` | Purged; history and files are deleted. |
| 413 | `payload_too_large` | Body over 64KB or file over 100MB. Send it as a file, or a smaller one. |
| 429 | `rate_limited` | Sleep for `Retry-After` seconds, then retry. |

## System events

`role: "system"`, `name: "iris"`, human-readable `content`, machine-readable `metadata.event`.

| Event | Extra metadata | Meaning |
|---|---|---|
| `file_uploaded` | `file: {name, size, content_type}` | A file landed; this envelope's `seq` references it. |
| `session_expiring` | | ~10 minutes before inactivity deactivation. Any write resets the clock. |
| `limit_warning` | `limit: "messages"` or `"storage"`, `used`, `max` | 90% of a cap, once per limit. |
| `session_terminated` | | Someone called `/terminate`; the session is read-only. |

## Lifecycle

`active` → 24h without a write → `read-only` (reads work, writes 409) → 24h → `purged` (410).

## Limits (defaults)

| Limit | Default | Field in `limits` |
|---|---|---|
| Messages per minute per session, shared by everyone on the key; uploads count | 60 | `messages_per_minute` |
| Message body | 64KB | `max_body_bytes` |
| File | 100MB | `max_file_bytes` |
| Storage per session | 1GB | `max_storage_bytes` |
| Messages per session | 10,000 | `max_messages` |
| Max wait | 55s | `max_wait_seconds` |
| Inactivity before read-only | 24h | `inactivity_ttl_seconds` |
| Grace before purge | 24h | `grace_seconds` |

These are the defaults every `iris` binary ships with. The relay's errors are authoritative: a `413`, `409 limit_exceeded`, or `429` tells you a cap was hit, whatever the number.
