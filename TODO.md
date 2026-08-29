# TODO

## Shipped

**Ctrl+V clipboard image paste** — spool clipboard image/file data to a
per-process temp dir (swept, not deleted, on next start); macOS
(`osascript`/`sips`), Linux/BSD (`wl-paste`/`xclip`), and Windows
(PowerShell) readers; `Client.SendPhotoMessage` for inline photos; composer
paste UI, notices, and attachment restore on send failure; app-level
Ctrl+V handling and spool cleanup. See the README's "Clipboard paste"
section for current behavior.

**Remediation & architecture program** (orchestrated, base a0bce8c) —
seven waves, now closed:

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
  access-hash cache persisted to a bbolt `state.db`
  (`internal/telegram/state_store.go`), wired into the full
  (updates-enabled) client only — `telegram-api serve` and
  `telegram-mcp serve` stay in-memory/no-updates so they never contend for
  the exclusive bbolt lock.
- **Wave 4 — field-test fixes**: root-caused the alt-binding failures to
  the Kitty keyboard protocol reporting Option+key as its composed
  character on macOS (Option+1 → text "¡"); `Keystroke()`-authoritative
  key matching, `NormalizeKey` canonicalization; search overlay geometry
  and empty-state fixes; chat-open loading/photo-prefetch/priority-sender
  fixes; `PgUp`/`PgDn` and `Ctrl+F` in-chat search with `n`/`N`;
  `SearchChatMessages` REST/MCP support.
- **Wave 5 — field-test round 2**: dropped the runtime terminal
  capability queries that could leak OSC/DCS response bytes into the
  composer — image-protocol detection is env-var-only now; cell-accurate
  list rows (emoji/wide-rune titles no longer shear the panel frame);
  terminal-independent folder switching (arrows, digits, `[`/`]`, clicks);
  a full vi-convention keymap pass; `Alt+C`'s alt-free `F4` fallback;
  TUI log silencing (see Wave 6/7 below for `TELETUI_DEBUG`); Ghostty/
  Terminal.app/iTerm2 Option-as-Alt guidance.
- **Wave 6 — composer editing**: `emacs`/`vi`/`auto` line-editing keymaps
  (`ui.compose_editing`, inferred from `$VISUAL`/`$EDITOR` on `"auto"`),
  decoder-verified newline chords (`Ctrl+J`, `Shift+Enter`, `Ctrl+Enter`,
  `Alt+Enter`), `Ctrl+O` full-screen editing via `$VISUAL`/`$EDITOR`.
- **Wave 7 — migration, markdown, help**: `-migrate-config` (stale-default
  key retirement, new-field fill-in, change/collision/unknown-key report,
  timestamped `.bak`, atomic symlink-preserving `0600` writes, idempotent);
  outgoing markdown → message entities on send/edit/captions (Telegram
  Desktop subset, opt-in via `ui.parse_markdown`, off by default, on by
  migration); `?` context-sensitive help overlay built from the same
  resolved bindings `internal/app` dispatches on (single source of truth);
  explicit compose (`i`/`c` to enter the composer; quick-type removed);
  client-death error panel + non-fatal `⚠` degradation notices.
- **Docs**: this pass (and the wave-3/wave-4 docs passes before it)
  brought README and TODO in line with Waves 1–7.

Detailed history lives in git (`a0bce8c..HEAD`); this file only tracks what
remains.

## Shipped on `fix/hardening-remaining`

- [x] **Jail `send_file` paths** — REST/MCP resolve via
      `ResolveAllowedSendPath` (abs + EvalSymlinks, under `files_dir` or
      cwd). TUI attach stays unrestricted.
- [x] **HTTP timeouts and body limits** — `ReadHeaderTimeout` 10s,
      `ReadTimeout` 30s, `WriteTimeout` 10m, `IdleTimeout` 120s;
      JSON bodies capped at 1 MiB.
- [x] **Mask 2FA password** — TUI `TextArea.EchoPassword`; CLI
      `ReadAuthLine` uses `term.ReadPassword` on a TTY. QR login
      already hid input.
- [x] **Session / files / config dirs `0700`**
- [x] **Honor `[media]`** — `ApplyMedia` wires protocol, bubble size,
      voice/video players, `AutoDownloadPhotos`, `AutoDownloadLimitMB`.
      Voice notes still download on play (no eager prefetch).
- [x] **`DownloadFileSync` singleflight** per file key
- [x] **Bounded `image.Decode`** — 20 MiB / 20e6 pixels via
      `DecodeConfig` before Decode

## Remaining — product / cleanup

Reviewed against the TUI 2.0 design (docs/tui-2.0.md) — some of this is
eliminated by that work, some is absorbed into it, and some is untouched.
Marked accordingly so nothing gets fixed twice or fixed in a way TUI 2.0
immediately undoes.

### Eliminated by TUI 2.0 — do not fix

- [x] ~~**Group info panel unreachable**~~ — `groupinfo.Model` and
      `OpenGroupInfo` exist but no keybinding ever reaches them. **Do not add
      a binding**: the context rail supersedes the component outright
      (docs/tui-2.0.md phase 6, which deletes `groupinfo` once the rail
      preserves its information). Closed as won't-fix.
- [x] ~~**`pkg/utils` sanitizers unused**~~ — confirmed *the whole package*
      has zero callers outside itself (`sanitize.go`, `truncate.go`,
      `timeformat.go`). Worse than unused: `Truncate`, `TruncateMiddle`, and
      `PadRight` are all **rune-count** geometry, exactly what TUI 2.0 phase 0
      prohibits, so they are a trap for anyone who reaches for them while
      building the new layout. Delete the package as part of the phase 0
      width standardisation rather than finding it callers. Inbound text stays
      sanitized in `telegram.sanitizeTerminal`.

### Absorbed into TUI 2.0 — fix there, not separately

- [ ] **`dialog.NewAlert` is dead code** — `NewConfirm` has callers, `NewAlert`
      has none. Decision 8 requires confirmation and error behaviour for
      destructive palette commands (pin, mute, secret chat, export), which is
      what a single-button alert is for. Expect phase 7 to give it its first
      caller; revisit only if phase 7 ships without one.
- [ ] **Status bar hints are hardcoded and do not follow rebinds** — the
      second half of the stale-`config.example.toml` item below. TUI 2.0
      replaces the status bar with a context-sensitive hint bar that should
      read from the phase 7 command registry, which is precisely what stops
      hints drifting from bindings. Fix it there, once.
- [ ] **Clipboard *text* fallback** — phase 8 already adds a text/code copy
      abstraction for `y` (with a possible OSC 52 path). Read and write
      directions are the same code area; do both in one pass.
- [x] **Per-chat drafts** — **promoted from nice-to-have to in-scope TUI 2.0
      work** (decision 13). All six frame goldens render a chat-list preview
      row as `draft: saved locally`, and that stands: the composer keeps a
      draft per chat, switching chats preserves it and its staged attachment
      instead of discarding them, and the chat list reads that state.
      In-memory for the session; no Telegram draft sync. Lands with the
      composer in phase 5, with the preview row in phase 2.
- [ ] **Multi-file paste** (currently only the first clipboard file) — now
      explicitly gated: decision 5 keeps a single staged attachment for TUI
      2.0 and defers albums, which need slice-based composer state, ordering
      and caption rules, and Telegram multi-media send. Blocked, not dropped.

### Raised in priority by TUI 2.0

- [ ] **SIGINT handler `os.Exit(0)`** — `cmd/teletui` skips bubbletea
      teardown. Already untidy; phase 8 makes it riskier, since a hard exit
      while a Kitty/Sixel overlay is on screen can leave the terminal in a
      graphics or alt-screen state the shell inherits. Worth fixing before
      the media overlay lands.

### Untouched by TUI 2.0

- [ ] **`config.example.toml` `[keys]` comment is stale** — still warns that a
      bare printable "shadows quick-type"; quick-type was removed. A plain doc
      fix, needed now. (The file needs a second pass later anyway for
      `ui.inline_images` / `ui.rail` / `ui.mode_indicator` and the removal of
      `chat_list_width` / `show_avatars` under decision 10.)
- [ ] **MCP and REST remain a 1:1 copy**
- [ ] `isWildcardHost` edge-case spellings if REST binds beyond loopback
- [ ] Expose photo sending via REST `/api/send-file` and MCP `send_file`
      (currently always a document)

## TUI 2.0 — design closed, implementation not started

Design record: [docs/tui-2.0.md](docs/tui-2.0.md), now contracted — all
thirteen decisions are resolved. Handoff archived in
[docs/handoff/](docs/handoff/). Goldens in
[docs/fixtures/](docs/fixtures/). **No code has been written yet**; the design
phase is what is complete.

- [x] **Visual sign-off (decision 11)** — `docs/fixtures/` holds cell-exact
      goldens at 80×24, 100×30, 120×40, 137×29, 200×60, plus a
      CJK/emoji/RTL/ZWJ fixture and a block gallery. All seven verified
      line-by-line under both `uniseg.StringWidth` and `ansi.StringWidth`;
      one ZWJ padding defect found and fixed.
- [x] **Handoff reconciled** — divergences recorded in docs/tui-2.0.md
      ("Divergences from the handoff prose"); three fixture-forced amendments
      folded into the spec; four factual errors in the reconciliation table
      corrected against the code.
- [x] **Threads deferred (decision 12)** — `t thread` was in five hint-bar
      goldens and specified nowhere. Removed; the five rows were regenerated
      cell-exact. `t` now belongs to the voice-note transcript alone.

### Design decisions — all closed

**Nothing blocks implementation.** Decisions 3 and 6–13 were resolved on
2026-08-29; 1, 2, 4, and 5 were resolved when the design record was written.

- [x] **D3 — mode model**: app mode is independent of the composer's emacs/vi
      submode and the badge reports app mode only. The vi Escape ladder is
      **unchanged**: first Escape leaves vi insert and flips the badge to
      NORMAL, second Escape cancels a pending reply/edit/attachment. Emacs
      cancels on the first Escape as it does today. The badge is additive —
      it describes the ladder rather than altering it, which is why
      `keys_test` must pass unmodified.
- [x] **D6 — rail data policy**: deferred until the rail is opened; no fetch
      on chat open.
- [x] **D7 — top-bar facts**: functions deferred, cells kept with placeholder
      text (`mtproto 2.0`, `devices 1`). Goldens regenerated. **See release
      blocker below.**
- [x] **D8 — command authority**: pin/unpin, mute/unmute, reload-config
      authorised; secret chat and Markdown export deferred and not registered.
- [x] **D9 — responsive precedence**: 12–19 rows keeps the width-based column
      layout with the top bar and no hint bar. Narrowing order is rail off,
      then chat list, then thread — the thread is the region that survives.
      Narrow single-panel with no chat selected shows the chat list.
- [x] **D10 — configuration migration**: `ui.chat_list_width` and
      `ui.show_avatars` removed outright (reported by `-migrate-config`, old
      values recoverable from the backup); `ui.mode_indicator` never added,
      since the badge must not be configurable away.
- [x] **D13 — per-chat drafts**: in scope. Drafts survive a chat switch along
      with their staged attachment, and the chat list shows `draft: saved
      locally`. In-memory for the session; no Telegram draft sync.

### Release blockers created by a deferral

- [ ] **Top-bar placeholders must not ship.** The goldens and the spec carry
      `mtproto 2.0` and `devices 1` as literal placeholder text so the layout
      and shrink order could be settled without the data. Before release,
      either wire both to a real source or drop the two cells and regenerate
      the affected top-bar rows. A hard-coded transport version presented as
      live connection status is a lie in the UI.

### First code, in this order

- [ ] **Golden harness** (~60 lines): fixture loader, ANSI stripper, per-line
      width assertion, row-count assertion. Build it before any renderer —
      it turns the frame work into red/green.
- [ ] **Width standardisation**: use `ansi.StringWidth`, promote
      `widgets.FitLine` to a shared package. Do **not** add a new
      display-width utility or promote `uniseg` to a direct dependency.
- [ ] **Mode resolver + command registry + palette** — no dependency on the
      frame redesign, so it can run in parallel from day one, and phase 2's
      hint bar should read from the registry rather than hardcode hints.
- [ ] **Frame** (theme roles, layout, borderless assembly, top bar, hint bar,
      chat list) as **one** branch — phases 1–3 are not separable, since the
      existing components rely on Lipgloss borders to absorb width slop.
- [ ] **Thread grid** — the riskiest parcel: `chatview/model.go` is ~1960
      lines against ~2000 lines of tests, and the line-index scroll
      machinery must survive behaviourally intact. Land the cursor-identity
      fix (`getTargetMessage` is a bottom-visible approximation) as its own
      commit first.

### Documentation debt that comes due when code ships

- [ ] README "Features" still advertises **Message Bubbles** and **Profile
      Avatars**; both are explicit TUI 2.0 non-goals.
- [ ] README "Screenshot" block is drawn in bubbles with rounded borders.
- [ ] README "Architecture" diagram labels the message panel `(bubbles)`.
