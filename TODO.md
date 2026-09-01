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
command palette, the thread grid, the content blocks, the composer and the
context rail have all landed; bubbles, avatars, Glamour, the status bar and
the group-info overlay are gone with them.

Every phase in the plan has now shipped, both release blockers are
discharged, and the four content blocks that were waiting on Telegram data
are mapped and drawn. What is left is byte equality against the goldens: the
blocks render, but reconciling them with the fixture cell for cell is a pass
of its own — see "Byte equality against the goldens" below. The goldens stay
asserted on width until it lands.

Field feedback has its own item below: the compose line's editing keymaps are
thinner than either convention implies.

- [x] **Panel surfaces are painted** — every panel wrapped its assembled row
      in a background style, which a styled span's own `ESC[0m` cleared, so
      the surface died at the first span: chat rows with an unpainted title
      line, a selected-message band one cell wide, and the terminal's own
      background showing through every row a panel did not draw. `cell.Fill`
      reopens the background after each reset and `frame.Column.Surface`
      moves the painting to the one place that can cover the padding rows.
      Asserted with `cell.PaintedWidth`, which is worthless without a pinned
      colour profile — four packages did not have one, which is why this ran
      four phases under a green suite. See
      [divergence 19](docs/tui-2.0.md#19-the-frame-owns-each-columns-surface-not-the-panels).
- [x] **`r` and `e` typed into nothing, and `s` saved into the cache** —
      two field reports, one of them four symptoms of a single cause.

      Reply, edit, `i` and `ctrl+j` all appeared broken, and `ctrl+o` was
      the only way to write anything: the composer's vi state was
      initialised once and remembered, and a vi user always leaves through
      normal mode, so it was always in normal mode by the time it was next
      entered. Typing "abc" after `r` gave "bc" — the `a` was vi's append.
      `SetFocused` now resets to insert on the unfocused→focused
      transition, and only on that transition (divergence 36). Editing
      somebody else's message is still refused, but says so now: a silent
      refusal is indistinguishable from a key that does not work, which is
      how `e` came to be reported as broken for a rule the client is right
      to enforce.

      `storage.download_dir` (default `~/Downloads`) is where `s` now puts
      a copy, under the sender's filename, never overwriting and with the
      name confined to that directory. `files_dir` stays the media cache
      it always was; the two were one setting (divergence 37).

- [x] **The three-cell gap after the clock** — `ui.emoji_width`, the
      declaration divergence 26 said would be needed. `auto` (default) keeps
      today's pessimism, `composed` and `separate` state what the terminal
      does with U+FE0F, ZWJ sequences and flags. It is in `cell` rather than
      `topbar` because every panel measures the same emoji.

      Two bugs found writing it. "Narrow" and "wide" cannot name the modes:
      a selector sequence is drawn *narrower* than the tables say and a
      joined or paired one *wider*, by the same terminal. And the pessimism
      was under-reserving — one cell per composition rune is 4 for a
      three-person family that a non-composing terminal draws in 6, which is
      exactly the corruption the reservation exists to prevent. The bound is
      now the wider of the two renderings (divergence 38).

- [x] **Missing chat titles, and notifications that ignored mute** — two
      field reports, one cause. `ChatUpdateMsg` carried both a dialog (which
      knows mute, pin, unread and the read marker) and a peer (which knows
      none of them), and both went through `ChatStore.Set`, which replaces.
      So opening a chat cleared its mute flag — and cleared
      `LastReadInboxMessageID`, which is where the unread divider goes.
      `Set` is now for dialogs, `Merge` for peers, with an explicit field
      list rather than zero-value guessing. `GetChat` reads
      `account.getNotifySettings`, which is the only way a chat outside the
      loaded dialog page learns it is muted.

      The titles were the other half: a message from beyond the first dialog
      page made the store invent an entry with an id and nothing else, and
      opening it was the only thing that ever fetched the chat. Invented
      entries are marked, fetched once, and read `loading…` meanwhile
      (divergence 39).

- [x] **Notifications no longer come from "Script Editor"** —
      `notifications.method`. osascript posts as Script Editor because the
      process is Script Editor, and a CLI binary cannot post under its own
      name on macOS without an app bundle. The terminal can, and does:
      OSC 777 where it carries a title, OSC 9 where it does not, allowlisted
      the way hyperlinks are because a terminal that does not understand the
      sequence prints it. Emitted through `tea.Raw` rather than written
      directly — Bubble Tea owns the descriptor. Message bodies and chat
      names are sanitised, since both go inside the sequence and both come
      off the wire (divergence 40).

- [x] **Three review findings on the two above** — all the same shape as the
      bug they were fixing (divergence 41). The notification was decided
      before the mute answer arrived, so the first message from a muted chat
      below the dialog page still rang; it now waits for the fetch, with a
      four-second backstop and a bounded queue. The OSC escaping was applied
      to the system path too, mangling `Meet at 6; bring food` for a syntax
      `notify-send` does not use; split into `sanitizeText` and
      `sanitizeSequence`. And `CreatePrivateChat` built its chat without
      asking about mute, so opening a muted contact unmuted it — divergence
      39's own bug, one layer down. `resolvedChat` is now the only place a
      peer becomes a `Chat`, held by an AST test.

- [ ] **Text cannot be selected with the mouse.** Raised in field use. The
      cause is known: `View` sets `MouseMode = tea.MouseModeCellMotion`, so
      the terminal hands every drag to the application instead of running its
      own selection. That mouse reporting is not decoration — it is what
      makes clicking a chat row, a folder tab and the mouse wheel work.

      Three ways out, and the choice is a decision rather than a fix:

      - **Hold a modifier.** Most terminals (iTerm2, Ghostty, GNOME Terminal,
        Windows Terminal) bypass mouse reporting while Option/Alt or Shift is
        held, so selection already works today if you know the gesture. This
        may be entirely a documentation item.
      - **A toggle.** A binding that drops mouse reporting until pressed
        again, so a drag selects. Cheap, but adds a mode with no visible
        state unless the hint bar says so.
      - **Yank instead of select.** `y` already copies the cursored message,
        and the media overlay could copy a code block. Selection-by-mouse is
        then a thing you do not need — which is the honest answer for a
        terminal app, and the least likely to satisfy someone who wanted to
        grab half a sentence.

      Check the modifier gesture on the terminals in use before building
      anything: if it works everywhere that matters, the whole item is a
      README paragraph.

- [ ] **Compose-line editing is thinner than the keymap it advertises** ←
      **next**. Raised in field use, and largely a rendering problem that is
      now fixed: the caret was drawn as a gap in vi's normal mode too, so
      every motion read as off-by-one even though `ctrl+a`, `ctrl+e`, `0` and
      `$` had always worked (divergence 28).

      What is genuinely missing, now that the caret is legible:

      - **vi normal:** `^` is unbound — only `0` and `home` reach the line
        start. No `e`, no `W`/`B`, no `cw`/`ciw`, no `p`, no counts.
      - **emacs:** no `alt+b`/`alt+f` (word motion), no `alt+d` (kill word
        forward), no `ctrl+y` (yank back what `ctrl+k`/`ctrl+w` killed).

      Worth a scoped pass rather than an accretion of keys: a kill ring is a
      design decision, `cw` needs an operator-pending state the composer does
      not have, and counts need a prefix register. Decide the shape first.

- [x] **Six components rendered from the legacy `theme.Theme`** — migrated,
      and the legacy theme is deleted. `palette`, `help`, `search`, `dialog`,
      `auth` and `contacts` now draw from `theme.Roles` through a shared
      overlay vocabulary (`theme/overlay.go`), so a title is one colour in
      the app rather than six.

      `theme.Theme` was 268 lines of pre-built lipgloss styles carrying its
      own bright blue `39` and green `42`. The vocabulary that replaces it is
      functions of `Roles`, not a second struct: a table of styles has to be
      constructed somewhere, so it acquires a lifecycle and a copy in every
      component that holds one, which is how it drifted in the first place.

      `TestNoColourLiteralsOutsideThePalette` scans `internal/ui` for
      `lipgloss.Color("…")` and fails on any it does not have a documented
      reason for. It found four more on its first run.

      Three things fell out. The chat list ran a whole **avatar subsystem** —
      cache, renderer, a per-chat photo download on every chat-list load —
      feeding a `ListItem.Avatar` field its own row renderer never reads;
      TUI 2.0 replaced avatars with the type sigil and nobody removed the
      machinery. `widgets.List` still drew the initials block for the two
      surfaces that had no custom row renderer. And `chatview`, `chatlist`
      and `composer` **ignored the palette their constructors were handed**,
      installing the 256-colour default instead — masked by a redundant
      `SetRoles` call at startup, and exposed by removing it.

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
- [x] **D7 — top-bar facts**: deferral discharged. The device count is wired
      to `account.getAuthorizations`; the transport cell is deleted, because
      gotd speaks MTProto 2.0 and nothing else and the cell could only ever
      have shown one string.
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

- [x] **Top-bar placeholders must not ship** — discharged. The device count
      is real (`account.getAuthorizations`, asked once when the connection
      becomes ready; zero drops the cell, because every account has at least
      the session doing the asking). The transport cell is gone rather than
      wired: there was nothing for it to vary with.

      Two things fell out of doing it. `frame-80x24.txt` drew a top row the
      renderer has never produced — the fixture gave the folder tabs priority
      over the right group and the spec gives it the other way round, and
      nothing compared them because the frame tests assert width, not
      content. And the chrome rows had no periodic tick at all, so the clock
      showed the time of the last window resize and `hintbar.ClearNotice` had
      no caller, leaving a "transient" notice up until something replaced it.
      Both fixed; see divergence 6 and 6a.

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

- [x] **The four content blocks** — reactions, poll results, link previews
      and a voice note's waveform, mapped in `internal/telegram/blocks.go`
      and drawn by `internal/render/{poll,preview,reactions}.go` plus the
      waveform on the media card. See divergences 16, 16a and 16b.

      Two things the mapping had to get right. **Tallies are keyed, not
      zipped**: `PollResults.Results` arrives in its own order, addressed by
      each answer's opaque option bytes, and is empty entirely for a poll
      that hides its results — pairing it with the answers by position would
      attach the right numbers to the wrong answers in exactly the case
      nobody checks. **Percentages are shares of different things in the
      two kinds of poll**: a single-choice poll's answers partition its
      voters, so its shares are of the votes cast and apportioned by
      largest remainder to sum to exactly 100 — three equal thirds round to
      33 each and a reader adds them up. A multiple-choice poll's do not
      partition anything, so its shares are of the VOTERS and are not meant
      to sum to anything; three people who each pick both answers have
      chosen each unanimously, and 50% there is the opposite of what
      happened. `TotalVoterCount` counts people for the same reason.

      Reactions and poll votes arrive as their own updates rather than as
      edits, and both are routed through the refetch an edit already takes —
      the tallies come back attached to the message they belong to, so
      nothing has to merge a partial update into a message it cannot see.
      `updateMessagePoll`'s peer is optional; without one there is no chat to
      refetch from and the update is dropped rather than aimed at chat zero.

      Reaction chips are budgeted with `cell.Reserve`, not `cell.Width` — an
      emoji is the one string the tables and the terminal disagree about, and
      a row budgeted on the smaller of the two runs off the pane on the
      terminal that draws the larger.

      A voice note's **transcript** is still absent: a Telegram premium RPC
      this client does not make.

      Review found two things. The multiple-choice denominator above, and
      three blocks drawing past a one-cell body column — the preview's rule,
      the code frame's four-cell guard, and prose reaching `cell.WrapLines`,
      which emits a rune wider than the column whole. Each has a fallback
      form now and the whole-body invariant test sweeps from one cell rather
      than fourteen, which is the gap that let them through. Divergence 16c.

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

- [x] **Media overlay, yank, and final hardening** — phase 8, the last one.
      `internal/ui/components/mediaview` draws a photo full-pane and `esc`
      puts it away; `y` copies the selected message's text through the
      platform clipboard helpers; `space` plays a voice note; `M` clears the
      unread badge without moving the scroll or the divider. `y yank` rejoins
      the hint set it was cut from while it did nothing.

      The scrollback guarantee is structural: the overlay emits no graphics
      sequence until it has a downloaded file, which only happens after the
      key that asked for it. Asserted for all three protocols, including the
      loading and failed states.

      **OSC 8 hyperlinks shipped without the wrapper they were costed at**
      (divergence 14). Reopening a link across a wrap does not need to know
      which runes belong to it — the URI is in the opening sequence, so
      `cell.OpenLink` recovers it the way `OpenStyle` recovers an SGR run.
      Twenty lines beside the ones that existed, not a grapheme-aware
      wrapper. Gated by `ui.hyperlinks`, whose `auto` is an allowlist and
      excludes tmux.

      Two live defects found on the way. `renderKitty` had no `q=2`, so every
      inline photo on a kitty terminal made it reply `_Gi=..;OK\` **onto
      stdin** — keystrokes, under Bubble Tea's raw-mode loop, typed into
      whatever had focus. And it placed images with no id, so nothing could
      delete one without deleting every placement on the screen. Both fixed;
      divergence 20.

      No OSC 52 clipboard fallback: the plan gates it on approval it has not
      been given, so ssh gets an honest "no clipboard tool found" rather than
      a silent write to the user's local clipboard. Divergence 21.

- [x] **Top bar placeholders** — discharged. `mtproto 2.0` and `devices 1`
      were literal strings with no data behind them (decision 7): the last
      thing on screen that stated something the client did not know. The
      device count is wired to `account.getAuthorizations`, and the transport
      cell is deleted: gotd speaks MTProto 2.0 and nothing else, so the cell
      could only ever have shown one string. See D7 above.

- [ ] **Byte equality against the goldens.** Only width is asserted
      today. The four content blocks now render, so the last structural
      blocker is the media overlay; what remains beyond it is reconciliation
      rather than absence. Known differences, each deliberate and recorded in
      divergences 16a and 16b: the poll's percentage field is four cells
      wide rather than three (the gallery never draws a 100% option), a voice
      note's row carries its file size, and neither a transcript affordance
      nor `transcript: t` exists to draw. That separation is why
      `golden.Compare` reports width and content as different `DiffKind`s.

### Documentation debt that comes due when code ships

- [x] README no longer advertises Message Bubbles or Profile Avatars, its
      screenshot is drawn as the grid, and the architecture diagram labels
      the message panel `(grid)`.
