# Architecture

How the program is put together, and where to look for a given thing.

The design record for the interface is [tui-2.0.md](tui-2.0.md) — thirteen
closed decisions, a divergence log explaining every point where the
implementation departs from the design and why, and the verification matrix.
It is a contract rather than a proposal; read it before changing how anything
is drawn.

## The shape

```
┌─────────────────────────────────────────────────────────────┐
│                       Bubble Tea v2                          │
│                                                              │
│   top bar          folder tabs · connection · clock          │
│  ╭──────────╮  ╭────────────────────────╮  ╭─────────────╮   │
│  │   chat   │  │      thread grid       │  │   context   │   │
│  │   list   │  │   time · sender · body │  │     rail    │   │
│  ╰──────────╯  ╰────────────────────────╯  ╰─────────────╯   │
│                ╭────────────────────────╮                    │
│                │  composer  (mode badge)│                    │
│                ╰────────────────────────╯                    │
│   hint bar         what to press · how much there is         │
├─────────────────────────────────────────────────────────────┤
│                 store — thread-safe caches                   │
│            chats · messages · users · files                  │
├─────────────────────────────────────────────────────────────┤
│              gotd/td — pure Go MTProto client                │
│           update dispatcher → p.Send(tea.Msg)                │
└─────────────────────────────────────────────────────────────┘
```

No panel borders and no status bar: the frame is assembled from exact-width
rows with single-cell rules, and the two chrome rows carry what a status bar
used to. Every row is exactly the terminal width, asserted against the
goldens in [fixtures/](fixtures/) at five sizes plus a wide-rune stress
fixture.

## Layout

```
cmd/
  teletui/          the TUI, plus the first-run credential wizard
  telegram-mcp/     MCP server over stdio
  telegram-api/     REST server

internal/
  app/              root Bubble Tea model: key routing, layout, chrome,
                    the command registry, and the interaction-mode resolver
  config/           TOML loader, defaults, and -migrate-config
  keys/             key-press matching and binding normalisation
  version/          what a binary reports for -version
  clipboard/        platform clipboard readers and the paste spool

  telegram/         gotd/td wrapper and the domain types
    types.go        Chat / Message / User / File and friends
    auth.go         phone, code, 2FA, QR
    listener.go     update dispatcher → tea.Msg bridge
    chats.go        dialogs, history, search
    messages.go     send, edit, fetch
    blocks.go       reactions, polls, link previews, waveforms
    files.go        file registry and downloader
    state_store.go  update-sequence state and the peer cache (bbolt)

  store/            thread-safe in-memory caches the UI reads
  render/           message content → terminal output (entities, blocks,
                    media cards, timestamps)
  media/            image rendering: kitty, sixel, Unicode half-blocks
  notification/     desktop notifications and sound
  tgjson/           JSON projections of the domain types
  mcpserver/        MCP tool surface
  restapi/          HTTP handlers, auth, and limits

  ui/
    cell/           terminal geometry — the ONLY place text is measured,
                    truncated, padded or wrapped
    theme/          semantic colour roles, dark and light
    sigil/          chat-type marks, shared by the list and the thread
    layout/         responsive panel budget
    frame/          borderless assembly; owns each column's surface
    golden/         fixture harness for docs/fixtures
    widgets/        list, textarea, spinner, tabs, QR
    components/
      chatlist/     two-line rows, folder state, the local filter
      chatview/     thread grid, scrolling, media playback
      composer/     draft, reply/edit modes, drafts, the mode badge
      rail/         pinned, members, shared files and links
      topbar/       folder tabs, connection dot, device count, clock
      hintbar/      contextual hints, counters, transient notices
      palette/      the `:` command overlay
      attach/       the ctrl+t file picker
      reactionpicker/ the `+` one-row reaction picker
      mediaview/    full-pane photo overlay
      search/       cross-chat search overlay
      contacts/     contact list overlay
      help/         the `?` keymap card
      auth/         sign-in screens
      dialog/       confirm and alert modals
```

## Rules worth knowing before changing anything

**Geometry lives in one package.** `internal/ui/cell` is the only place that
measures, truncates, pads or wraps text. Rune counts are not display cells,
and every panel that measured its own text is a panel that sheared the frame
on a CJK title or an emoji. `cell.Reserve` exists because an emoji's width is
a declaration rather than a measurement — see `ui.emoji_width`.

**Views are pure.** `View` renders the model and nothing else; the chrome
rows are refreshed by `refreshChrome` on a tick and on layout changes, not
rebuilt inside `View`.

**State is derived, not stored, wherever it can be.** There is no mode field
— `Model.Mode()` computes NORMAL/INSERT/COMMAND from focus and the composer's
own state, so nothing can contradict what `Update` does with a key. The
layout reconciles by comparing what the composer asks for against what it was
granted rather than trusting a notification. A component that already owns a
fact exposes it rather than announcing changes to it.

**The terminal is never asked anything at runtime.** An OSC reply arrives on
stdin and is typed into whatever has focus. Colour depth, image protocol and
hyperlink support are all resolved from environment variables only.

**The peer cache is a map that a file outlives the process for.** Access
hashes are read from `state.db` once at open and answered from memory
afterwards, because gotd consults them on the update path. Writes are
coalesced: an unchanged hash costs nothing, and changed ones go out together
a quarter-second after the burst stops. Losing the tail on a crash is
acceptable — the server hands them out again, which is also why `bindOwner`
can drop the whole namespace when a session file turns out to belong to a
different account.

**The open chat batches what updates ask it to do.** An arrival, an edit, a
reaction and a poll tally each used to cost their own RPC — one
`readHistory` per message, one `getMessages` per change — which in a busy
group is one request per update and the pattern Telegram answers with
`FLOOD_WAIT`. Both are accumulated over a 300ms window now
(`internal/ui/components/chatview/coalesce.go`): a read receipt is
cumulative, so a burst becomes one call carrying the highest ID, and the
refetches become one `getMessages` with the deduplicated list, minus any ID
the store no longer holds. The tick carries the chat it was scheduled for,
so one that outlives a chat switch is dropped rather than fired against the
new chat. This is the same coalescing the peer cache does above, for the
same reason.

Note what is NOT here: a `FLOOD_WAIT` back-off. Errors from these
background calls are discarded rather than shown, which is right — a
receipt the reader never asked for should not interrupt them — but nothing
retries. gotd's floodwait middleware is not installed on the client either,
so a rate limit is currently an error every caller swallows differently.
That belongs in one middleware covering every RPC rather than at these two
call sites, and it is a behaviour change (waits in place of errors) that
deserves its own decision.

**Only the TUI subscribes to updates.** `telegram-api` and `telegram-mcp` run
in-memory with no update stream, so they never contend for the exclusive
bbolt lock on `state.db`.

## Testing

`go test ./...` runs everything. Three kinds of test carry more weight than
their size suggests:

- **Goldens** — `internal/app` renders a fixed scene at each fixture's size
  and compares every cell. `-update` regenerates one, and prints the rows it
  changed, because a regeneration is how a copy change lands and also how a
  layout bug becomes the expected output.
- **Keymap drift** — [keys.md](keys.md)'s tables are compared against the
  bindings the app dispatches on, in both directions and per section.
- **Colour-profile pinning** — every package that asserts on styling pins a
  profile in `TestMain`. lipgloss resolves to Ascii under `go test`, which
  makes styling assertions pass whatever the style was; four packages once
  ran a whole redesign phase green without it.
