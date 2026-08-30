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
      building the new layout. **Deleted** in the phase 0 width
      standardisation rather than given callers. Inbound text stays sanitized
      in `telegram.sanitizeTerminal`.

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

- [x] ~~**`config.example.toml` `[keys]` comment is stale**~~ — **already
      fixed**, verified 2026-08-29. The quick-type wording is gone; the
      comment now correctly describes what happens today (a wired bare
      printable shadows that key in the chat list and chat view, with `quit`
      called out as the exception that also reaches the composer), and
      `forward` is marked as accepted for round-trip compatibility only. The
      file still needs a pass for `ui.inline_images` / `ui.rail` and the
      removal of `chat_list_width` / `show_avatars` under decision 10 — that
      lands with phase 5, not as a standalone doc fix.
- [ ] **MCP and REST remain a 1:1 copy**
- [ ] `isWildcardHost` edge-case spellings if REST binds beyond loopback
- [ ] Expose photo sending via REST `/api/send-file` and MCP `send_file`
      (currently always a document)

## TUI 2.0 — design closed, every panel shipped

Design record: [docs/tui-2.0.md](docs/tui-2.0.md), now contracted — all
thirteen decisions are resolved. Handoff archived in
[docs/handoff/](docs/handoff/). Goldens in
[docs/fixtures/](docs/fixtures/).

**State of play.** The shape of TUI 2.0 is on screen. Foundations, the
borderless frame with its top and hint bars, the two-line chat rows, the
command palette, and the thread grid have all landed; bubbles, avatars,
Glamour and the status bar are gone with them.

What is left is the media overlay and the remaining chat-view actions
(`y`, `space`, `M`), plus the four content blocks whose data the client does
not yet map. Until those land, the goldens are asserted on width only — see
"Byte equality against the goldens" below.

Implementation happens in a **separate worktree**, not the primary checkout —
the redesign spans several phases that are not individually shippable, and
the primary checkout stays free for fixes against a working client.

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

- [x] **Golden harness** — `internal/ui/golden` (PR #8). Loads a fixture,
      strips ANSI, asserts row count and per-line display width separately
      from byte equality so the width gate can be hard while copy churns.
      Also validates the shipped fixtures themselves on every run.
- [x] **Width standardisation** — `internal/ui/cell` is now the single
      source of terminal geometry: `Width`, `MaxWidth`, `Truncate`, `Clamp`,
      `ClampLeft`, `Pad`, `Fit`, `Wrap`, `FitLine`. No production code
      measures or cuts text any other way. Folded in three copies of the
      same truncate (`widgets.truncate`, `chatlist.truncateLabel`,
      `help.truncatePlain`) plus `widgets.fitCell` and `help.padPlain`, and
      deleted `pkg/utils` outright. `golden.Width` delegates to `cell.Width`
      so the harness cannot disagree with the renderer it judges.
      Net −246 lines in existing files.
- [x] **Mode resolver** — `internal/app/mode.go`. `Model.Mode()` derives
      NORMAL/INSERT/COMMAND from focus, the composer's vi submode, and which
      overlay owns the keyboard. Derived, never stored: there is no mode
      field, so nothing can contradict what `Update` actually does with a
      key. Rules are a pure function of an explicit `modeInputs` struct, so
      every combination is testable without a Model, and a new overlay has
      to decide its effect rather than inherit one. `keys_test` passes
      unmodified, per D3.
- [x] **Command registry + palette** — `internal/ui/components/palette` (the
      overlay) and `internal/app/commands.go` (the typed registry, one source
      for name, argument shape, description, key equivalent, and behaviour).
      `:` routes through `Model.Mode()`, so a focused emacs composer types a
      colon while a vi composer in command state opens the palette. Shipped
      with `mark-read`, `search <query>`, `keymap`, `quit`.

      **Navigation is arrows and ctrl+n/p, not j/k** — the handoff specified
      j/k, but `:jump`, `:keymap`, and `:mark-read` all contain one of those
      letters and could never be typed. Recorded as divergence 9.

- [ ] **Remaining palette commands** ← **next, and each is a service, not a
      palette change.** All are authorised by D8; none are blocked on
      permission:
      - `pin` / `unpin` — needs a Telegram RPC and domain mapping
      - `mute <duration>` / `unmute` — needs notification-settings RPCs
      - `reload-config` — needs runtime config reload; confirm first when the
        composer holds a draft or attachment (D8)
      - `theme <name>` — needs every component to accept a theme at runtime;
        probably falls out of phase 1's theme rework rather than being done
        separately
      - `jump <date>` — needs history-by-date

      They are absent from the registry rather than stubbed: an entry that
      cannot run teaches a command that does not exist.
- [x] **Frame** — theme roles (`internal/ui/theme/roles.go`), responsive
      budget (`internal/ui/layout`), borderless assembly
      (`internal/ui/frame`), `topbar`, `hintbar`. **The first user-visible
      change.** No panel borders; single-cell rules; every row exactly the
      terminal width, asserted at the five golden sizes plus a sweep of
      widths 20–300 and heights 3–60.

      The earlier claim that phases 1–3 are inseparable turned out to be
      wrong in detail: the fix is not "land them together" but "the frame
      fits panel output rather than trusting it". `frame.Render` fits every
      line to its region, so panels that are not yet exact-width are padded
      or clipped instead of shearing — which is what lets the chat list and
      thread grid land afterwards, separately.

- [x] **Chat list rows** — two-line rows on the golden's measured grid, type
      sigils (`@` DM, `#` group, `!` channel, `~` saved), selection bar,
      muted rows that say "muted" in words, the filter header and a
      contextual footer. `widgets.List` gained a pluggable `RenderRow` so
      the bespoke row did not require forking its cursor/scroll/hit-test
      machinery.

      Folder tabs finished their move: `topbar.TabAt` is the hit-test now
      and `chatlist.SelectFolderIndex` is the other half. The old tab-bar
      rendering, its chip and its hit-test are deleted rather than left
      dead — along with the seven tests that were keeping them alive, each
      guarantee re-homed to topbar or the app first.

- [x] **Thread grid** — the columnar time/sender/body grid replaces bubbles.
      24-cell gutter compressing to 20 when the body would fall below 32
      (two of the five goldens are on the narrow side, so both sides of the
      threshold are tested), deterministic sender colours, day and unread
      dividers, single-row reply quotes, delivery marks read from the chat's
      own read markers.

      The line index survived intact: dividers are attached to the message
      below them rather than being separate history entries, so it stays one
      count per message and `scrollToMessage`, `sliceLines` and
      `visibleMessages` are unchanged. Selection is not part of the render
      cache key — caching it would make the line index depend on where the
      cursor is, and every scroll and jump is built on that index.

      The **cursor-identity fix landed first**, on its own. The action keys
      now act on a stored message identity that sticks while its message is
      visible, clamps to the nearest visible message when a scroll carries
      it off, and follows the newest while the view is pinned to the bottom.
      The old rule was a position, and positions moved on their own: a photo
      below the fold finishing its download changed which message `r` would
      reply to.

      glamour left `go.mod` with the bubbles. Entities render as ANSI spans
      directly, which also removes the `WithAutoStyle` OSC 11 hazard rather
      than guarding it.

      Deleted rather than left dead: `RenderMessage` and its bubble-width
      helpers, `EntitiesToMarkdown`, `theme.ChatViewHeader`, and
      `internal/ui/components/statusbar` — the last of which the frame had
      already replaced and whose final live job (typing) moved into the
      thread. Every guarantee re-homed first; re-homing found that
      `ConnectionStateMsg` had no consumer anyone drew, so the connection dot
      only learned the truth twice in a session. It is live now.

      The chat-type sigil moved to `internal/ui/sigil`, shared by the list
      and the thread header, which are peers.

- [x] **Content blocks** — code frames, quotes, list indentation, inline
      entity styling, media cards and spoilers. `internal/render/blocks.go`,
      `internal/render/media.go`, and a rewritten `entities.go`.

      Entities are a per-rune style table now rather than a walk in offset
      order. The old walk printed every overlapped run once per entity
      covering it, so a link inside a bold sentence arrived on screen with
      its text twice.

      Code is truncated horizontally, never wrapped, and the frame caps at
      84 cells with a gutter that compresses on a narrow pane — both forms
      are drawn in the goldens. Media cards collapse to one line below a
      40-cell body, dropping the actions rather than the facts. Spoilers are
      drawn in their own background and `x` toggles them on the cursor
      message; moving the cursor or opening a chat closes them.

      **Fixing this phase found a bug in the last one.** `ansi.Wrap` does not
      reopen a style after a break, so a styled run spanning a wrap painted
      the rest of the row — trailing padding, panel rule and the whole next
      column — in that run's style. `cell.WrapLines` makes each wrapped line
      self-contained, and `cell.OpenStyle` is exported so any component can
      assert it leaves nothing open.

      Both test packages now pin a colour profile in `TestMain`: lipgloss
      resolves to Ascii under `go test`, which makes every styling assertion
      pass whatever the style was — and a hidden spoiler IS its colour.

      One fixture defect found and fixed: `blocks-100x52.txt` drew its list,
      quote, link-preview and poll continuations at column 19 instead of the
      body column, while drawing the code block and media card in the same
      fixture at 24. Recorded as divergence 13.

- [ ] **Content that still has no data source.** Each is a mapping change in
      `internal/telegram`, not a renderer (divergence 16):
      - reactions — `Message.Reactions` is not mapped
      - poll options, counts and closing time — `Poll` carries the question
      - link previews — no web-page type at all
      - voice waveform — `DocumentAttributeAudio.Waveform` is not mapped
      - voice transcript — a premium RPC this client does not make

- [x] **Composer and app modes** — the mode badge, the inline row, the
      expanded Ctrl+P form, per-chat drafts, and the decision 10 config
      migration.

      The badge is **derived, not set**: a focused composer knows whether the
      next printable key will be inserted, so it says so without being told,
      and only COMMAND has to come from the host. It covers emacs, which the
      old `-- INSERT --` indicator never did.

      The composer's rows come out of the thread's budget explicitly now —
      `layout.Compute` takes what it asked for, and a `ResizedMsg` tells the
      host when that changes.

      Drafts park per chat and the map means "unsent work in chats that are
      not open", so restoring consumes the entry. The chat list shows
      `draft: saved locally` in place of the last-message preview.

      `ui.chat_list_width` and `ui.show_avatars` are reported as *removed*
      rather than as unrecognised keys; `ui.inline_images` is added and
      wired, `ui.rail` added and read but not honoured until the rail exists.
      `ui.mode_indicator` is absent, with a test that says so.

      One divergence recorded (17): four view assertions in the composer's
      tests moved, because the view is the thing this phase replaces. Every
      behavioural test and `keys_test` pass unmodified, which is what the
      exit criterion was protecting.

- [x] **Context rail** — pinned messages, members, shared files and links in
      a 30-cell column, toggled with backtick, shown at 118 columns and
      wider. `ui.rail` is the default and survives a terminal too narrow to
      honour it.

      Decision 6 holds: nothing is fetched on chat open, only when the rail
      is opened, cached per chat and generation. A late result for a chat
      that is no longer open is still cached — it is correct for the chat
      that asked, and only the open chat's entry is ever read.

      Every section says which of four states it is in, because "not asked",
      "waiting", "refused" and "empty" would otherwise all be blank space.
      The member remainder counts the chat's total, which costs a second
      call: the participants API returns a page, not a count.

      One adapter, three filters: `telegram.SearchChatMedia` is
      `messages.search` with a server-side filter and no query. That got the
      pinned section for less than the reconciliation costed it, and got all
      the pins rather than just the current one — divergence 18.

      `groupinfo` deleted: built, sized and fed on every message since the
      frame landed, never drawn. `PanelGroupInfo` went with it, and
      `senderColour` moved to `theme.SenderColour` so the rail and the
      thread grid cannot disagree about a person's colour.

- [ ] **Media overlay, yank, and final hardening** ← **next**, phase 8. The
      dismissible full-pane image overlay (Kitty, Sixel, half-blocks, an
      external viewer, then platform open — and never writing graphics into
      scrollback), `y` to copy a message or a code block, `space` to play
      the selected voice note, and `M` to mark read without moving. OSC 8
      hyperlinks belong here too, with the grapheme-aware wrapper that would
      make them safe (divergence 14).

- [ ] **Byte equality against the goldens.** Only width is asserted today.
      The fixtures are renders of a *finished* TUI 2.0, so string equality
      cannot pass until the blocks that are waiting on data land, and the
      media overlay with them. That separation is deliberate and is why
      `golden.Compare` reports width and content as different `DiffKind`s.

### Documentation debt that comes due when code ships

- [x] README no longer advertises Message Bubbles or Profile Avatars, its
      screenshot is drawn as the grid, and the architecture diagram labels
      the message panel `(grid)`.
