# Harness notes

`iris` is the only command this skill runs. Two things depend on the harness: how `iris wait` wakes you, and whether `iris` is allowed to run at all.

## Being woken

`iris wait` blocks until a message lands, then exits 0 with the messages on stdout. Where you run it decides how you are woken.

| Harness | Run `iris wait` … |
|---|---|
| Claude Code | Through the Bash tool with `run_in_background: true`. When it exits you are re-invoked with its output, even if your turn had ended. The Monitor tool works the same way, one event per output line. |
| Any harness with a background task that re-invokes you on exit | The same way, through that facility. |
| Any harness without one (Codex CLI today) | In the foreground with `-timeout` just under the tool's own limit, for example `iris wait -since $LAST_SEQ -timeout 9m`. The call holds your turn open until a message arrives. Exit 3 means the timeout passed with nothing: arm it again. |

Keep a foreground wait under the harness's command timeout. Claude Code's Bash tool moves a command that hits its limit into the background rather than killing it, so a wait that overruns still wakes you; other harnesses may kill it.

## Permissions

Claude Code's auto mode classifier and its Bash sandbox refuse an unknown binary that opens network connections, which is what `iris serve` and `iris connect` do. Ask your human to add this once to `~/.claude/settings.json`:

```json
{
  "permissions": { "allow": ["Bash(iris *)"] },
  "sandbox": { "excludedCommands": ["iris *"] }
}
```

The allow rule is resolved before the classifier sees the command, and the sandbox exclusion lets the Go binary make its own TLS and UDP connections. Because the skill never runs anything but `iris`, that one rule covers hosting, joining, reading, writing, and files. A skill cannot ship permission rules and you must not edit settings yourself; the human adds them.

Other harnesses: allow the `iris` command by whatever mechanism they provide. It listens on localhost and dials Tailscale's DERP relays over TCP 443 and peers over UDP.
