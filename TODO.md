# TODO

## Shipped

**Ctrl+V clipboard image paste** — spool clipboard image/file data to a
per-process temp dir (swept, not deleted, on next start); macOS
(`osascript`/`sips`), Linux/BSD (`wl-paste`/`xclip`), and Windows
(PowerShell) readers; `Client.SendPhotoMessage` for inline photos; composer
paste UI, notices, and attachment restore on send failure; app-level
Ctrl+V handling and spool cleanup. See the README's "Clipboard paste"
section for current behavior.

**Remediation & architecture program** (orchestrated, base a0bce8c) — three
waves, now closed:

- **Wave 1 — foundations**: clipboard spool hardening (PID-named dirs, exec
  timeouts, safe-existing-dir checks), `Esc` key fixed app-wide (was
  `"escape"` vs. bubbletea's `"esc"` in several places), single-flight
  paste + spool cleanup on replace/cancel/send-failure, Telegram client
  race/timeout/control-char fixes, `Chat.Muted` + live mute updates, dialog
  pagination, chat folders backend, REST API bearer-token auth +
  Host/Origin/Content-Type enforcement, real word-wrap, local timestamps.
- **Wave 2 — UI**: chatview bubble cache with exact line-index scrolling,
  jump-to-message (search results), focus-gated read receipts, chatlist
  folder tabs with Telegram filter semantics, mute display (🔕 + faint),
  statusbar unmuted/total unread split, `[keys]` config wiring +
  `NormalizeKey` (fixed the F1–F3 casing bug in the process), terminal
  focus reporting enabled.
- **Wave 3 — structural**: `gotd/td` update-sequence state + peer
  access-hash cache persisted to a bbolt `state.db` (`internal/telegram/state_store.go`),
  wired into the full (updates-enabled) client only — `telegram-api serve`
  and `telegram-mcp serve` stay in-memory/no-updates so they never contend
  for the exclusive bbolt lock. Docs (this pass) brought README and TODO in
  line with Waves 1–3.

Detailed history lives in git (`a0bce8c..HEAD`); this file only tracks what
remains.

## Deferred / follow-ups

- [ ] **Group info panel unreachable** — `groupinfo.Model` and
      `OpenGroupInfo` exist and are dispatched to when focused, but no
      keybinding ever sets `PanelGroupInfo` or calls `OpenGroupInfo`.
- [ ] **`dialog.NewAlert` is dead code** — no caller anywhere outside
      `internal/ui/components/dialog/model.go` itself.
- [ ] Multi-file paste (attach several files from one clipboard file drop
      list — currently only the first file reference is used)
- [ ] Clipboard *text* fallback (paste clipboard text into the composer
      when no image/file is present — currently shows "no image or file
      in clipboard")
- [ ] Per-chat drafts (switching chats currently discards the draft and
      any pending attachment)
- [ ] `isWildcardHost` covers `0.0.0.0` / `::` / `0:0:0:0:0:0:0:0` — revisit
      for other wildcard/edge-case spellings if the REST API's
      loopback-only assumption ever loosens further
- [ ] Expose photo sending via the REST API (`/api/send-file` always sends
      as a document) and the MCP `send_file` tool

## Wave 4 — field-test fixes — ACCEPTED 2026-08-28 (mech + adversarial review clean after 1 nit round)
- [x] K: alt-binding root cause = kitty protocol associated-text (Option+1 → Text "¡"); Keystroke()-authoritative matching, quick-type modifier guard, NormalizeKey canonicalization, 40-case real-decoder regression tests. Caveat: terminals sending only the composed char (Terminal.app/iTerm2 defaults) are undetectable — set Option-as-Meta/Esc+, or use F1-F3
- [x] S: search overlay — centered capped box (72x24, window-clamped), textarea single-line scroll window (cursor artifact fix), hints incl. Esc, honest empty states
- [x] C: chat-open — loading clears at first paint, photo prefetch capped at 10 newest + lazy on scroll (also fixed always-false Downloaded skip → thumbnails re-downloaded every open), priority senders; PgUp/PgDn; Ctrl+F in-chat search with n/N (esc releases results); external image open nil-panic fixed + header hints; CJK header truncation
- [x] T: SearchChatMessages API (messages.search, channel-verified)

## Wave 5 — field-test round 2 (2026-08-28, in progress)
- [ ] R: kill runtime terminal queries (OSC junk typed into composer); style from theme
- [ ] L: cell-aware list rows (emoji titles shear panel frames); terminal-independent folder selection (arrows/digits/click); statusbar hints
- [ ] K: vi-convention keymap pass; non-alt contacts binding; log silencing in TUI; Ghostty guidance
- [ ] D: README refresh (after code)
- [ ] E (wave 6, concurrent): composer editing — newline chords (decoder-verified), emacs/vi modes (config compose_editing=emacs|vi|auto from $EDITOR), ctrl+o external $EDITOR full-screen editing
- [ ] M (wave 7, after V5/6): teletui -migrate-config flag (upgrade old-default keys, add new fields, .bak backup, summary); README + config.example.toml keymap refresh incl. the flag
- [ ] MD (wave 7): outgoing markdown → entities on send/edit/captions (Desktop subset, rune→UTF-16 offsets, round-trip tested vs incoming converter, parse_markdown toggle)
- [ ] H (wave 7): '?' context-sensitive help overlay from a bindings registry (single source of truth); lazygit-flavored additive aliases ([/] folder tabs); keys.help config
