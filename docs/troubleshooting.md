# Troubleshooting

**Logging is silenced by default**, `tele-tui` only. The TUI owns the
terminal in raw mode, so a stray log write lands in the middle of a
rendered frame — this used to happen from background goroutines logging
"connection state: connecting" every time the network blipped.
`telegram-api` and `telegram-mcp` are unaffected and keep logging to
stderr normally.

Get the log back with `TELETUI_DEBUG`:

```bash
TELETUI_DEBUG=/tmp/teletui.log bin/tele-tui
```

The file is opened in **append** mode with a
`=== teletui session started <RFC3339> (pid <pid>) ===` banner per run,
never truncated — debugging this app usually means restarting it
repeatedly, and truncating on every start would destroy the log of the run
that actually reproduced the problem. If the path can't be opened, that's
reported once on stderr (this runs before the alt screen takes over) and
the run continues with logging disabled, rather than sitting there waiting
for output that will never arrive.

**If the Telegram client dies for good** — the session was revoked from
another device, or the connection failed in a way the client gave up on —
the whole UI is replaced by an error panel: what happened, a plain
statement that it will not recover on its own, and a nudge to restart.
Every keybinding except quit goes inert at that point — the panels behind
the error screen are still holding their last-known state, but acting on
them would only mutate data you can no longer see. The panel points at
`TELETUI_DEBUG` for more detail.

**Non-fatal degradations** — the client keeps running, just with something
turned off — surface as a `⚠ ...` notice on the composer's hint line
instead of a full-screen panel. Two you may see in practice: the
update-state database being locked by another `tele-tui`/`login` process
(gap recovery disabled for this run only — see [Persistence](configuration.md#persistence)),
and the peer cache in `state.db` having belonged to a different account
(rebuilt automatically, also covered there).

## Alt bindings do nothing on macOS

Your terminal is sending the composed character rather than a meta key. It
is a terminal setting, not a client one, and the fix per terminal is in
[keys.md](keys.md#macos-alt-bindings-and-the-option-key) — along with the
alt-free alternative every binding except two already has.
