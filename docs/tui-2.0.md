# TUI 2.0 design record and delivery plan

Status: **contracted.** All thirteen decisions are resolved (see
[Decisions](#decisions)); implementation may begin. What remains before
release is engineering. The top-bar placeholders decision 7 deferred are
retired: the device count is wired and the transport cell is deleted.

This document is the repository-native record of the supplied TUI 2.0
handoff, reconciled against the code. **Where this document and the handoff
prose disagree, this document wins** — see
[Divergences from the handoff prose](#divergences-from-the-handoff-prose)
for the list and the reasoning behind each. The original reference bundle is
intentionally a visual aid, not a source-code dependency:

- Archive: Telegram CLI TUI design.zip
- Received: 2026-08-29
- SHA-256: 84e805684c493e7ce928ac4b8adef06a7767c6ec43318c47c1edd6f82d66f8a7
- Contents: the written handoff, an HTML visual reference, and its support
  script.

The written handoff is archived verbatim at
[docs/handoff/tui-redesign-handoff.md](handoff/tui-redesign-handoff.md); see
[docs/handoff/README.md](handoff/README.md) for provenance and checksums. The
HTML reference is only for visual details omitted here. Implement in the
existing Bubble Tea v2 and Lipgloss codebase; do not port the HTML or CSS and
do not add another rendering library.

The cell-exact golden renderings that answer decision 11 live in
[docs/fixtures/](fixtures/). They, not the HTML, are the acceptance artifact
for frame integrity and column alignment.

## Product intent

TUI 2.0 changes the client from a terminal-shaped chat GUI into a dense,
keyboard-first terminal tool:

- Use a borderless, columnar frame. Panel boundaries are one-cell rules, never
  rounded boxes.
- Replace message bubbles with a fixed time / sender / body grid.
- Keep the existing keymap and focus model, while making NORMAL, INSERT, and
  COMMAND legible.
- Add a persistent context rail and a command palette.
- Make graphics opt-in. Media normally appears as metadata; terminal graphics
  render only after an explicit open action unless configured otherwise.

The following are explicit non-goals: avatars, decorative boxes, bubbles,
per-language syntax highlighting, animation other than a network spinner, and
mouse-only workflows.

## Non-negotiable visual contract

### Frame and responsive layout

The full frame has a one-row top bar and a one-row hint bar. Between them are a
chat list, a thread header and scroller, an optional reply row, a composer,
and (when enabled) a context rail.

| Terminal width | Layout |
| --- | --- |
| 118 columns or wider | chat list 38, rule 1, thread flex, rule 1, rail 30 |
| 90–117 columns | chat list 38, rule 1, thread flex; rail hidden |
| 72–89 columns | chat list 30, rule 1, thread flex; rail hidden |
| below 72 columns | existing single-panel behaviour; Tab swaps chat list and thread |

At 20 rows or more, render the full frame. Below 20 rows, hide the hint bar
and force an inline composer; the width-based column layout **continues** at
12–19 rows, top bar included. Below 12 rows, hide the top bar and render only
the thread with its inline composer. Rows must never wrap or exceed the
terminal width.

The width table above is a precedence, not just a set of thresholds: **the
thread is the region that must survive.** Give up the rail first, then narrow
the chat list, and let the thread flex last. When the terminal is narrow
enough that only one panel fits and no chat has been selected yet, show the
chat list — there is nothing to draw in a thread pane before a chat is
chosen.

Focus is not a border. It is represented by the cyan selection bar in the
focused panel and the composer mode badge. The rail may be disabled by width
or manually with the grave-accent binding; width can force it off but cannot
override a user's explicit off choice.

### Palette

Build the dark theme from the following semantic roles. Use true colour when
available and the listed xterm-256 fallback otherwise. Determine the colour
capability once during startup. The existing DarkTheme signature remains; the
hard-coded 256-colour body is replaced.

| Role | Hex | 256 | Primary use |
| --- | --- | --- | --- |
| bg | #0b0d10 | 232 | app background and scroller |
| panel | #0e1116 | 233 | list, rail, headers, composer |
| chrome | #12151a | 234 | top and hint bars |
| sel | #171d24 | 235 | selected chat |
| curline | #12171d | 234 | selected message row |
| rule | #1f242b | 236 | panel separators |
| rule-soft | #1a1f26 | 235 | in-panel dividers |
| border | #262d36 | 237 | attachment, code, and poll frames |
| ghost | #3f4750 | 239 | separators and inert glyphs |
| faint | #465059 | 240 | timestamps and byte counts |
| dim | #5c666e | 243 | secondary copy |
| fg | #c9ced4 | 252 | message body |
| bright | #e2e7ec | 255 | active titles and bold spans |
| cyan | #6fb8c9 | 73 | sole focus and key accent |
| amber | #d1a86a | 179 | commands, attachments, channels, inline code |
| green | #86b57a | 108 | insert mode, online, own messages, sent |
| mauve | #b58ac9 | 139 | groups, italics, sender colour |
| blue | #8aa8d0 | 110 | DMs, mentions, sender colour |
| red | #c9736a | 167 | errors, failures, removed diff lines |

Amber, green, mauve, and blue are semantic colours, never decorative accents.
The light theme remains an inversion of the same roles; dark is the
high-fidelity reference.

### Top bar, chat list, and hint bar

The top bar is one chrome row. It starts with cyan bold tg and a ghost rule,
then compact numbered folder labels from the current folder model. The active
folder is bright; inactive folders are dim. Keep existing 1–9, h/l, and
bracket folder navigation, but move folder rendering out of the chat list.

The right side contains a connection dot, a compact connection description,
device count, transport, and clock. The dot is green when ready, amber while
connecting, and red when disconnected. It degrades from the left: truncate
the status description, then drop device count, then transport; never drop
the clock.

The chat list is 38 columns at normal width, panel background, and contains:

- A one-row filter header: amber slash, filter query or dim placeholder, and
  ghost matching/total count.
- Two rows per chat. Row one is selection bar, type sigil, title, and
  right-aligned relative time. Row two is the selection bar again, a
  two-cell indent, the preview, and a right-aligned unread badge — the bar
  spans the whole chat, not its first line (see
  [divergence 22](#22-a-selection-is-marked-down-its-whole-height)).
- Sigils: @ DM (blue), # group or supergroup (mauve), ! channel (amber), and
  ~ saved messages (green).
- Selected titles are bright; unread/unmuted titles are fg; other titles use
  #98a1a9. Muted chats include the literal dim word muted after the truncated
  title and use a subdued badge. Unread counts retain the existing 999+ cap.
- A one-row local footer with the j/k, g/G, and unread hints.

The bottom hint bar replaces the existing status/help composition. It has
context-sensitive key hints at the left and index/message/buffer/unread
statistics at the right. Drop whole hints from the right as space shrinks;
never wrap or truncate in the middle of a hint. Connection status moves to the
top bar. A transient error or progress notice owns this row for four seconds.

The exact arithmetic, derived from the goldens: one leading space, then hints
joined by two spaces, then padding, then the right group and one trailing
space. Hints are taken in order from a fixed set and the bar keeps the longest
prefix that leaves **at least five columns** of gap before the right group.
The chat-view NORMAL set is:

    q quit · i compose · : command · r reply · y yank · e edit · ? keymap

which yields four hints at 80 columns, six at 100, and all seven at 118 and
above. Because the set is a prefix, removing one hint lets the next one in at
narrow widths — that is why deferring threads gained `e edit` at 100 columns
rather than simply widening the gap.

### Thread grid

The thread header is a one-row panel surface. The left side is sigil, bright
bold title, ghost separator, and dim subtitle. The right side is fixed-width
buffer number, scroll position, and optional bot mark. Measure the right group
first; only the subtitle is elided.

Messages use a fixed 24-column gutter:

| Columns | Field |
| --- | --- |
| 1 | leading space |
| 1 | cursor bar on the selected message, on EVERY row of it — cyan while the thread has focus, ghost when it does not |
| 1 | leading space |
| 5 | faint HH:MM |
| 2 | spacing |
| 12 | deterministic sender name, right-aligned and elided |
| 2 | spacing |
| remaining | body |

**Narrow-pane amendment (forced by the fixtures).** A fixed 24-column gutter
does not survive a narrow thread pane: at 120x40 with the rail on, the thread
is 50 columns, leaving a 25-column body that cannot be read. When
`threadWidth - 24 - 1 < 32`, the sender column compresses from 12 to 8 and
the gutter to 20. This triggers at 80x24 and at 120x40 — both are in
[docs/fixtures/](fixtures/), so the arithmetic is pinned by a golden rather
than by prose.

The body starts at the same column on every message continuation and all
content blocks. Wrap words, only hard-breaking an unbroken URL. The selected
message has a curline background across the full thread width.

Sender colours are a deterministic user-ID hash into mauve, cyan, blue, and
amber. The local user is always green and named you; their body is #aab3bb.
Date and sticky unread dividers are left labels with a rule running to the
right edge. Unread uses amber and remains at its open position until the
buffer is left or explicitly marked read. Replies render as a single
body-aligned quoted row. Reactions render below the body with display-width
aware padded frames. Outgoing state uses faint dot, one check, two checks, or
a red failure mark.

The typing indicator occupies the final scroller row above the composer. Its
marker begins in the sender column, so it does not shift message layout.

### Rich text and blocks

Telegram entities are rendered as entities, never as raw Markdown or HTML.
Use bright bold, mauve italic, amber padded inline code, muted strike,
self-background spoilers, underlined cyan links with OSC 8 where supported,
and blue mentions. On the selected message, x reveals every spoiler.

All blocks begin at the body column and are capped to the smaller of body
width and 84 columns:

- Code blocks are border-framed, contain a language tag and line numbers, and
  only colour diff-like plus/minus lines and comments. A line wider than the
  pane is **truncated horizontally, never wrapped**, ending in an arrow; the y
  action still copies the original, untruncated block.
- Quotes use a ghost left rule and dim italic text. Lists use cyan bullets or
  ordinals and a two-column hanging indent.
- Images, videos, and documents default to a compact metadata card with an
  IMG, VID, or DOC badge, filename, dimensions/duration/page count where
  known, size, and explicit open/save actions. The three-row framed card needs
  a 40-column body; **below that it collapses to a single line** of the form
  `IMG auth-p95-260...  184 KB - png`. Both forms are in the fixtures
  (`frame-80x24.txt` and `frame-120x40.txt` show the collapsed one).
- Voice notes show a 24-cell waveform, duration, playback state, and
  transcript affordance. Space toggles the selected voice note.
- Link previews use a cyan left rule, host, title, and at most two description
  lines.
- Polls are framed, with option state, scaled bars, percentages, and a
  metadata footer.

Image rendering is governed by ui.inline_images: never, on_open (the default),
or always. On open, choose Kitty graphics, Sixel, half-blocks, configured image
viewer, then platform open. A usable inline backend opens a dismissible
full-pane overlay; it must not write graphics into scrollback. Always may use
an eight-row card preview.

### Composer, modes, and palette

NORMAL (cyan), INSERT (green), and COMMAND (amber) are explicit app modes.
NORMAL is the default. i, c, a, or a composer click enters INSERT; escape
leaves it. Colon from NORMAL opens COMMAND. This is an expression of the
current focus model, not a removal of Tab, Shift-Tab, or Alt/F-key focus
bindings. The far-left composer badge is the always-visible mode statement.

The default composer is one row: badge, coloured prompt, draft or hint, and a
right-side markdown label when empty or a character count when non-empty.
Reply/edit adds a one-row, no-wrap quote bar above it. Expanded composer is
eight rows: source with line numbers and dim Markdown markers on the left,
faithful rendered preview on the right, staged attachment chips, and a compact
chord footer. The existing textarea and both emacs and vi editing behaviours
continue within it.

#### Mode integration

Resolved by decision 3 and binding. Keep panel focus and input mode separate,
but make the displayed mode a derived, impossible-to-contradict state:

- COMMAND exists only while the palette owns input.
- INSERT means the composer has focus and the active composer editor will
  insert printable text.
- NORMAL covers chat-list/chat-view focus and a vi composer that has returned
  to its command state. In the latter case the composer still owns its vi
  commands; the NORMAL badge truthfully says that the next letter will not be
  inserted as text.

Tab, F-key/Alt focus, i/c/a, and a composer click select the composer and put
the vi editor in insert state, so they visibly enter INSERT. In emacs mode
there is no nested editor state, so the first Escape performs the
reply/edit/attachment cancellation directly. In vi mode, the first Escape
leaves vi insert and changes the badge to NORMAL; the *next* Escape follows
today's reply/edit/attachment cancellation or focus-back behaviour. In either
editor, Escape from an ordinary draft leaves the composer without discarding
the draft. For an expanded composer, Escape both returns to the inline
composer and exits INSERT; Ctrl-P is the non-destructive inline/expanded
toggle.

This is today's ladder, unchanged. The badge is additive: it reports which
state the ladder is already in, rather than altering how many Escapes anything
costs. That is what lets the existing `keys_test` suite pass unmodified and
keeps the README's documented Escape behaviour true.

This requires a small root-level interaction-mode resolver, not a second
independent boolean beside FocusPanel. The resolver must be the source for the
badge, key routing, and the context-sensitive hint bar. Colon is routed to the
palette only when this resolver reports NORMAL.

**Shipped** as `internal/app/mode.go`. `Model.Mode()` derives the mode from
focus, the composer's vi submode, and which overlay owns the keyboard; there
is no mode field to set, so nothing can contradict what `Update` does with a
key. Two consequences worth knowing before building on it:

- **It is not a drop-in for the existing focus guards.** NORMAL includes a vi
  composer in its command state, so a guard rewritten as "mode is NORMAL"
  would let `?` open the help overlay while the composer holds a draft, where
  today's "focus is not the composer" correctly does not. Decision 3 requires
  the badge to describe key routing, not alter it, and a test pins exactly
  that case.
- **COMMAND is specified and tested but not yet reachable.** The palette does
  not exist; wiring it means setting `paletteOpen` in the single place that
  fills the resolver's input struct.

New bindings proposed by the handoff are:

| Binding | Context | Action |
| --- | --- | --- |
| colon | NORMAL | command palette |
| 1–9 | chat view NORMAL | jump to current-folder buffer |
| grave accent | any | toggle context rail |
| Ctrl-P | composer | inline/expanded toggle |
| x | chat view | reveal selected-message spoilers |
| y | chat view | copy selected message or selected code |
| space | chat view | play/pause selected voice |
| M | chat view | mark read while preserving scroll |

The command palette is a 60-column dimmed-frame overlay positioned about
eight rows from the top. It supports fuzzy prefix matching, arrow-key
movement (see [divergence 9](#9-the-palette-navigates-with-arrows-not-jk) for
why not j/k), Tab completion, Enter execution, and Escape cancellation. A single registry
supplies the command, argument shape, description, command constructor, help,
and palette display.

The first-release registry, per decision 8, is: mark-read, cross-buffer
search, date jump, keymap, theme, quit (read-only or local), plus pin, unpin,
mute duration, unmute, and reload-config (authorised to make their Telegram
or config changes). **Secret chat and Markdown export are deferred** and must
not appear in the palette until they are authorised — a listed command that
cannot run is worse than an absent one.

The configuration additions are **ui.inline_images and ui.rail**, plus the
newly documented keys. `ui.mode_indicator` is deliberately not added: the mode
badge is the only always-visible statement of whether the next letter types or
navigates, so a switch to hide it would let a user configure away the thing
that makes the modal design legible. `ui.chat_list_width` and
`ui.show_avatars` are removed rather than kept as no-ops. See decision 10.

### Context rail

At 30 columns wide, the rail replaces the group-info overlay. It has
chat-specific sections with faint letter-spaced headings:

- Groups show pinned messages, up to eight members plus a remainder, and up
  to six files.
- DMs show shared files and shared links, without members.
- Channels show pinned messages and files.

Pinned rows have amber bullets, muted content, and ghost authors. Members use
online/offline state, deterministic sender colours, and right-aligned role or
last seen. File rows use amber type glyphs, elided name, and size.

## Reconciliation with the current client

This is a structural redesign, not a theme swap. The current implementation
uses rounded PanelNormal/PanelFocused panels, folder tabs inside chatlist,
message bubbles rendered by render.MessageRenderer and Glamour, a three-panel
focus model, a group-info overlay, a two-part status/help footer, one staged
attachment, and eager inline image rendering for configured media backends.

Some visual work is presentation-only. The full handoff, however, requires
data and behaviour that the current domain model and Telegram wrapper do not
provide. The handoff's statement that internal/telegram and internal/store
need no changes cannot hold if every named feature is shipped faithfully.

| Handoff area | Existing capability | Gap / required decision |
| --- | --- | --- |
| Frame, rules, palette, list, header, hint bar | Bubble Tea, Lipgloss, layout and widgets are suitable | presentation work only |
| Grid, dividers, sender colours, local read action | history, sender IDs, reply IDs, ViewMessages, and terminal focus events exist | replace bubble cache/index with grid-line cache; retain exact scroll semantics |
| Reply quote | only reply message ID is guaranteed | quote text/sender requires lookup from loaded history or an explicit fetch policy |
| Reactions | `Message` has no reaction data | extend Telegram mapping/domain, or omit |
| Per-message outgoing read state | `Chat.LastReadOutboxMessageID` is already mapped | presentation only: dot/check/double-check is derivable today. Only the red failure mark needs new plumbing — there is no send-failure representation in the domain |
| Entity styling and code/quote blocks | formatted-text entities exist **and `render.EntitiesToANSI` already renders them** (`internal/render/entities.go`); Glamour is only reached for text containing a fence (`markdown.go`) | smaller than it looks: extend the existing entity renderer to the block set and retire the Glamour path, rather than building an engine from scratch |
| Media metadata cards | file metadata, image backends, downloader, and external open exist | no in-TUI overlay lifecycle; current default can emit inline images while rendering |
| Voice waveform and pause | external player can play or stop audio | no amplitude extraction, progress, pause, or transcript data |
| Link previews and poll results | text and poll question only | preview metadata and poll options/results are not represented |
| Context rail — members | group-info can fetch members | current member query is limited |
| Context rail — pinned | `ChannelsGetFullChannel` is **already called** in `internal/telegram/groups.go` and already returns `pinned_msg_id`; the mapping into `SupergroupFullInfo` simply drops it | cheaper still, as built: `messages.search` with `InputMessagesFilterPinned` returns ALL pins in one request, and is the same call the files and links sections needed anyway — see divergence 18 |
| Context rail — shared files/links | no data path | genuinely new: needs a `messages.search` call with a media filter, capped and cached per chat |
| Top-bar transport/devices | connection state is available | no transport version or device/session count in the UI state |
| Expanded composer | textarea and outgoing Markdown parser exist | only one attachment; preview semantics conflict with optional parse_markdown |
| Per-chat draft indicator | composer holds a single draft, discarded on chat switch | the goldens show `draft: saved locally` as a chat-list preview; needs per-chat draft storage and a chat-list preview state — decision 13 |
| Command palette | global and in-chat search exist | mute, pin, secret chat, export, config reload, and theme switching do not all have application services |
| Yank | clipboard package imports attachable clipboard content | no text-copy implementation or OSC 52 output path |

### Rail data mechanics

The rail section names describe presentation categories, not data that can be
derived from filenames or chat names. The existing group-info overlay can load
a group description, member count, and a limited member list. It cannot return
the current pinned message, a complete shared-files list, or shared links.

In particular, a folder's PinnedChatIDs describe chats pinned in a folder,
not messages pinned inside an open chat. A loaded history page may include an
old pin service event, but that neither identifies the current pin nor supplies
the pinned message when it is outside the local page. Similarly, inspecting
loaded messages can produce a recent-files sample but cannot truthfully claim
to be the chat's shared files or shared links.

The pinned section is, however, cheaper than the handoff's "no new API calls"
claim and cheaper than an earlier draft of this document assumed:
`ChannelsGetFullChannel` is already called by `GetSupergroupFullInfo`, and its
result already carries `pinned_msg_id` — the mapping into `SupergroupFullInfo`
just discards it today. Recovering the current pin is one struct field plus the
`GetMessage` call that already exists. Shared files and shared links are the
genuinely new work.

Policy, resolved by decision 6 — **fetch is deferred until the rail is
opened**, never done on chat open:

1. Opening a chat costs no rail request at all. The primary history paint
   never competes with rail work, and a user who keeps the rail off never
   pays for it.
2. When the rail opens, render it immediately with available chat/member data
   and explicit loading rows, then asynchronously fetch the current pin and
   capped recent file/link results for that chat type. Cache each section by
   chat and load generation so switching chats drops stale results.
3. Update the cached section opportunistically from incoming messages while
   the rail stays open; provide an honest unavailable/error row when Telegram
   does not return that category.

This is a small, purpose-built rail data adapter, not a broad content index.
It is required to meet the handoff's persistent, chat-type-aware rail
faithfully.

## Divergences from the handoff prose

The handoff README predates several review decisions recorded here, and the
fixtures were generated after it. Where they disagree, **this document and the
fixtures win**. Each divergence below is a place the handoff prose should be
read as superseded, not as an instruction.

### 1. Spoiler reveal is `x`, not `s`

Handoff sections 6 and 7 assign `s` to "reveal spoilers in the cursored
message". `s` is already the media save/download binding in the chat view, and
the fixtures themselves keep it: every media card in
[docs/fixtures/](fixtures/) renders `o open - s save`. Decision 2 here resolved
this as `x`, which is unused in chat-view NORMAL mode. **The fixtures agree
with this document; the handoff prose is stale.** `x` retains its ordinary
deletion meaning in the vi composer, which is a different mode.

### 2. Resolved: threads are deferred and `t thread` is removed

Every frame fixture's hint bar originally read
`q quit  i compose  : command  r reply  t thread  y yank  e edit  ? keymap`.
`t thread` appeared in no binding table — not the handoff's list of new
bindings, not `internal/app/keymap.go`, not this document — and there is no
threads feature in the client to bind it to.

**Threads are deferred out of TUI 2.0.** `t thread` is removed from the hint
set and the five affected goldens were regenerated. `t` is consequently free,
which resolves the collision in divergence 3 below. Should threads arrive
later, they get a fresh binding decision rather than inheriting one that was
never specified.

### 3. Resolved by 2 — `t` is no longer claimed twice

The fixtures showed `transcript: t` on the voice-note block while the hint bar
advertised `t thread`: two meanings for one key in one mode. With threads
deferred, `t` belongs to the voice-note transcript affordance alone.

### 4. The expanded composer's chord footer collides with both editing keymaps

The handoff's expanded composer footer reads
`newline  ^d send  ^a attach  ^p preview  ^e $EDITOR  ^s save draft`, while the
same section promises that "both vi and emacs `composer.EditingMode()` keymaps
continue to work unchanged inside it". Both cannot hold:

| Handoff chord | Already claimed by |
| --- | --- |
| `Ctrl+A` attach | emacs start-of-line |
| `Ctrl+D` send | emacs delete-char-under-cursor (and vi insert-state delete) |
| `Ctrl+E` `$EDITOR` | emacs end-of-line |
| `Ctrl+S` save draft | unclaimed, but flow control on many terminals |

The existing bindings are `Ctrl+T` attach and `Ctrl+O` `$EDITOR`; `Enter`
sends. The composer's promise that no line-editing chord is stolen is a
load-bearing property of the current design and is not reopened here. This
footer is prose only — no fixture contains it — so the correction is free:
the expanded composer advertises the bindings that already exist, plus
`Ctrl+P` for the inline/expanded toggle.

### 5. The redesign is not presentation-only

The handoff's overview states there are "no changes to `internal/telegram`,
`internal/store`, `internal/restapi`, or `internal/mcpserver`", and section 8
states the rail "needs no new API calls beyond what `groupinfo` already
fetches". Neither holds if the named features ship faithfully — see
[Reconciliation with the current client](#reconciliation-with-the-current-client)
and [Rail data mechanics](#rail-data-mechanics). Decision 1 here resolved the
scope question in the other direction: narrowly scoped Telegram and store
additions are permitted, each with a concrete UI consumer and tests.
`internal/restapi` and `internal/mcpserver` are genuinely untouched.

### 6. Retired: the top-bar placeholders, one wired and one deleted

Four frame fixtures rendered the top bar with a transport version and a
device count, neither of which had a source. Decision 7 deferred the
functions while keeping the cells, so the layout and the shrink order were
pinned by the goldens, and made shipping them a **release blocker**.

The blocker is discharged, and the two cells were not the same problem.

**The device count is real.** `account.getAuthorizations` returns every
session authorised on the account, and its length is the number Telegram's
own clients show under "Devices". It is worth the cell for the reason
Telegram gives it a screen: a count higher than the user expects is how an
unauthorised login gets noticed. One RPC, asked once when the connection
becomes ready and held — sessions are created and revoked by hand, on the
scale of days, so polling would spend requests watching a number that does
not move. Zero means "not answered" and drops the cell, because every
account has at least the session doing the asking.

**The transport cell is deleted.** There was nothing to wire it to: gotd
speaks MTProto 2.0 and nothing else, so the cell could only ever have shown
one string. A constant in a status area is decoration wearing the clothes of
information, and the honest form of "always 2.0" is to say nothing.

The right group's shrink order is therefore two steps rather than three:
the device count goes, then the status description, and the clock never
does.

**A finding, made while regenerating the rows.** `frame-80x24.txt` drew all
five folder tabs and the degraded `connected │ 21:04` form, and the renderer
has never produced that: this document specifies that the right group claims
its space BEFORE the tabs, so at 80 columns the implementation kept the full
group and dropped two tabs instead. The fixture was drawn under the opposite
rule and nothing compared them, because the frame tests assert width and not
content. The row is regenerated from the renderer, which is the only source
that can be checked. With the transport cell gone all five tabs fit at 80
alongside `connected · 1 device`, so the fixture's intent survives the
correction.

### 6a. The chrome rows had nothing to make them tick

Found while wiring the device count, and the same class of defect the
placeholders were.

`refreshChrome` sets the top bar's clock, and it ran on a window resize, on
authentication, and on a folder-tab click. Nothing else. On a terminal
nobody resized, **the clock showed the time the client started, for the
whole session** — a cell that looks like live status and is not, which is
exactly what decision 7 refused to ship.

The same absence had a second victim. This document gives the hint bar's
transient notice four seconds, and `hintbar.ClearNotice` existed with **no
caller anywhere in the program**: a notice owned the row until something
else replaced it. Both are one missing piece — the program had no periodic
tick at all — and both are fixed by one: a one-second pulse that refreshes
the chrome and expires a notice that has had its four seconds.

### 7. Width is measured with `x/ansi`, not `uniseg`

`docs/fixtures/README.md` specifies `rivo/uniseg` as the measuring rule. Every
line of every fixture was verified to measure identically under
`uniseg.StringWidth` and under `ansi.StringWidth` from
`github.com/charmbracelet/x/ansi` — including the ZWJ family-emoji row, where
both return 2. `x/ansi` is already a direct dependency and is already the basis
of `widgets.FitLine`; `uniseg` is only an indirect dependency and nothing
imports it. **Use `ansi.StringWidth` and do not promote `uniseg` to a direct
dependency.** The naive alternative — summing `runewidth.RuneWidth` per rune —
returns 8 for that same emoji and is what produced the one defect found in the
fixtures on review (since corrected).

### 8. Resolved: per-chat drafts are adopted, not regenerated away

All six frame fixtures render the `~ wire notes` chat's preview row as
`draft: saved locally` — a per-chat draft indicator standing in for the
last-message preview. The client has no such feature: the composer holds one
draft, and switching chats discards it along with any staged attachment.

Neither the handoff prose nor an earlier draft of this document mentions
drafts at all; the requirement is visible only in the goldens. It is a
genuine scope addition — per-chat draft storage and a chat-list preview state
— and decision 13 resolved it **in scope**: the goldens stand, drafts survive
a chat switch, and the chat list shows the draft state. Persistence across
restarts is explicitly excluded.

### 9. The palette navigates with arrows, not `j`/`k`

The handoff specifies "`j/k` or arrows move" inside the command palette, and
this document repeated it. That cannot work: the palette is a text surface
whose query is a command name, and `:jump`, `:keymap`, and `:mark-read` all
contain a `j` or a `k`. Binding those letters to movement would make three
commands impossible to type.

**Resolved:** every printable key goes into the query. Movement is the arrow
keys, plus `ctrl+n`/`ctrl+p` for hands that do not want to leave the home row.
Tab completes, Enter runs, Escape cancels — all unchanged from the handoff.

This is the same class of conflict as divergence 1 (`s` for spoilers versus
`s` for save): a binding specified without checking what else already claims
that key in the same mode.

### 10. The thread header omits the buffer number and the bot mark

The goldens draw the header's right group as `buf 2 │ ln 214/214  bot`. Only
`ln 214/214` is rendered.

`buf N` is a chat's index among the open buffers, which the thread panel does
not know — the app does. `bot` needs a flag on the chat that the client does
not map today. Both are omitted rather than filled with a plausible number,
for the same reason the top bar's placeholders were a recorded release
blocker: a false fact stated in fixed-width type is worse than a missing one.

They come back when the data reaches the panel, and the right group is
measured first, so widening it will not cost the position cell.

### 11. Outgoing state has three marks, not four

The design record lists "faint dot, one check, two checks, or a red failure
mark". The first three are drawn. Nothing in the client reports a send
failure, so the fourth would be a glyph for a state that cannot be reached —
decoration pretending to be information, and the one kind of UI element that
teaches a user to distrust the rest.

The three that ship are real: pending is a message with no server ID, and the
difference between sent and read comes from the chat's
`LastReadOutboxMessageID`. The bubble renderer this replaces drew two checks
on everything it had sent, which told people their messages had been read
whenever they had merely left.

### 12. A reply to an unloaded message says so in words

The goldens show reply quotes citing messages that are in the loaded window.
When the cited message is not loaded — which happens constantly, since a
reply can point at anything in the chat's history — the row reads
`↳ earlier message` rather than showing the message ID.

The bubble renderer showed `┃ reply #4412`. An ID is not something a reader
can act on or recognise; the relationship is, and that is what is left.

### 13. The block gallery drew continuations at the wrong column

`blocks-100x52.txt` put the continuation rows of its list, quote, link
preview and poll at column 19 rather than at the body column, 24 — while
drawing the code block, the media card and the reactions in the same fixture
at 24.

Nineteen is where `you` starts when it is right-aligned in a twelve-cell
sender field. That is not a rule: `nadia` would start at 17. The gallery was
hand-drawn and this is a drawing error of the same kind as the ZWJ padding
defect found in decision 11, and it disagrees with all three frame goldens as
well as with this document, which says the body starts at the same column on
every continuation and all content blocks.

**Fixed in the fixture**, which now draws every continuation at 24. Eight
rows moved; each stayed exactly 100 cells.

### 14. Resolved: OSC 8 hyperlinks, without the wrapper they were costed at

**Superseded by phase 8.** This entry recorded why terminal hyperlinks were
absent, and the reason held for four phases: `ansi.Wrap` breaks a line
between a link's opening and closing sequences and repairs neither, so the
rest of that row — its trailing padding and the panel rule beside it — became
part of the link. The same class of fault as the SGR leak `cell.WrapLines`
fixes.

It was costed here at a **grapheme-aware wrapper**, on the reasoning that
reopening a hyperlink means knowing which runes belong to it after wrapping,
which means wrapping before styling, which means replacing the tested wrapper
with one of ours.

That reasoning has a false premise. Reopening a link does not require
knowing its runes: **the URI is carried in the opening sequence**, so the
sequence is its own answer. `cell.OpenLink` recovers it from a wrapped line
exactly as `OpenStyle` recovers an SGR run, and `WrapLines` closes and
reopens both. Twenty lines beside the ones that already existed, rather than
a wrapper.

What shipped with it: link entities carry their destination (`inlineStyle.uri`),
which is part of the style rather than a parallel table so that two adjacent
links to different places are never merged into one run. The destination can
only come from the entity's own field or the text it covers — a hyperlink
whose target disagrees with its visible text is the shape of a phishing
link. `ui.hyperlinks` gates it, `auto` is an allowlist, and tmux is excluded
whatever runs under it: OSC 8 needs `allow-passthrough`, which is off by
default and invisible to the environment.

The composer's preview emits none. It draws what WILL be sent, and a
clickable link in a draft invites a click on something that does not exist.

### 15. Inline code is coloured, not padded

The design record specifies "amber padded inline code". The padding is not
drawn: a code span is amber on the selection background, with no inserted
spaces.

Padding a chip means putting characters into the message that the sender did
not send. That is a small thing on a badge in the chat list, where the
content is a number this client computed, and a different thing inside
somebody's sentence — where `use ` foo ` here` is not what they wrote. The
background is what separates the chip; the spaces only widen it.

### 16. Four blocks the goldens draw have no data behind them

Reactions, poll results, link previews, and a voice note's waveform and
transcript are all in the fixtures and none of them are rendered. Each is
waiting on a data source, not on a renderer:

| Block | What is missing | What it would take |
| --- | --- | --- |
| Reactions | no field on `telegram.Message` | map `Message.Reactions` from gotd, and a domain type |
| Poll options, counts, closing time | `Poll` carries the question only | map `MessageMediaPoll`'s answers and results |
| Link preview | no web-page type at all | map `messageMediaWebPage` |
| Voice waveform | no amplitude data | map `DocumentAttributeAudio.Waveform` |
| Voice transcript | no transcription call | a Telegram premium RPC this client does not make |

What IS rendered from each is the part that exists: a poll shows its
question, a voice note its duration. A poll drawn with empty bars would state
a result, and a waveform drawn from nothing would be the one part of the card
that looks like measurement and is not.

### 17. The composer's view tests moved; its behaviour tests did not

Phase 5's exit criterion asks that "all existing composer tests pass
**unmodified** — a diff there means the badge changed behaviour instead of
describing it".

`keys_test` and every behavioural composer test do. Four assertions about the
composer's *rendering* could not, because the rendering is the thing the
phase replaces: a one-row inline composer cannot show both lines of a
two-line draft or a five-chord hint list, and `-- INSERT --` is a badge now.

Each moved rather than being dropped — the multi-line draft and the chord
list to the expanded form, the vi indicator to the badge — and each carries a
note saying where its guarantee went. The criterion was protecting the Escape
ladder, and the ladder is untouched.

The badge is also strictly more than the indicator was: `-- INSERT --` only
appeared in vi mode, leaving emacs users with nothing on screen saying whether
the next letter would be typed.

### 18. The rail's pinned messages cost less than the reconciliation assumed

The reconciliation table costs the rail's pinned section at "one field on the
`SupergroupFullInfo` mapping plus the existing `GetMessage` call" — recovering
`pinned_msg_id` from a `ChannelsGetFullChannel` result that is already
fetched.

That returns exactly one pinned message. The goldens draw two, and a chat can
have many.

`messages.search` with `InputMessagesFilterPinned` returns all of them in one
request, and it is the same call shape the section already needed for files
and links. So the pinned section is one filter value on an adapter that had to
exist anyway, rather than a field on a different mapping plus a second call —
cheaper than the estimate and more complete than what the estimate would have
bought.

`SupergroupFullInfo` is still consulted, for the member TOTAL rather than the
pin: `ChannelsGetParticipants` returns a page and not a count, and a
remainder row computed from the page misstates the size of any group larger
than one page.

### 19. The frame owns each column's surface, not the panels

The palette assigns a background to every region — panel for the chat list and
the rail, bg for the thread, chrome for the two bars — and this document reads
as though each panel paints its own. Every panel did, and every one of them
was wrong.

The spelling they all used was to assemble a row out of styled spans and then
wrap the finished string in a background style:

    lipgloss.NewStyle().Background(r.Panel).Render(cell.Fit(line, width))

That emits the background once, at the front. Each span inside the line closes
itself with `ESC[0m`, and a reset clears the background along with the
foreground, so the surface survived only as far as the first span. A chat row
showed no fill on its title line at all and a fill on its preview line that
stopped where the text stopped. The thread's selected-message band — specified
here as "a curline background across the full thread width" — was a single
cyan cell in the gutter. The top bar was the only continuous surface in the
app, and only because it repeated `.Background(chrome)` on all seven of its
styles.

It is the same family of defect as the one that produced `cell.WrapLines`: SGR
is a mode, and a reset is not a scope. The fix is `cell.Fill`, which reopens
the background after every reset in the line.

Where it is applied is the divergence. Painting stays out of the panels and
moves to `frame.Column.Surface`, because a panel can only paint the rows it
drew and the frame owns the ones it did not — the blank padding under a short
chat list, the centred empty states that go through Lipgloss's own `Height`,
and the whole column in single-panel mode. Those were the widest unpainted
bands on screen, and no amount of per-panel filling reaches them.

Panels now paint only their exceptions: the selected chat row (sel), the
selected message (curline), the unread badge (cyan), the thread header and the
composer (panel, inside a column whose surface is bg). Those win, because
`cell.Fill` reopens the surface **before** each span's own sequences rather
than after, so a nested fill overrides an outer one.

The assertion is `cell.PaintedWidth`, which counts the leading cells of a row
drawn with any background in effect. It returns 0 under a colour profile that
emits nothing, which is deliberate: a package asserting on it has to pin a
profile in `TestMain`, and four of them — `cell`, `chatlist`, `frame`,
`internal/app` — did not, which is how this shipped through four phases under
a green suite.

### 20. The overlay is photos only, and the capability order is not a ladder

The plan for phase 8 asks for "an explicit open flow with capability order
Kitty, Sixel, blocks, configured viewer, platform open", and a dismissible
overlay for the in-terminal modes. What shipped is narrower in one direction
and simpler in another.

**Photos only.** A video, a document or a voice note has no in-terminal
representation this client can draw, so `enter` on one keeps the existing
behaviour of handing the file to the platform. An overlay that opened and
said "cannot draw this" would be worse than the thing that already works.

**Not a ladder.** The three drawing protocols are not tried in turn until one
succeeds: `media.image_protocol` resolves to exactly one, at startup, from
the environment, and the overlay draws with it. A ladder implies a runtime
failure to fall off, and there is none — a terminal either understood the
protocol or silently did not, and this client has no way to find out which.
The external viewer and the platform open are not lower rungs either; they
are `o`, a separate key, available from inside the overlay and out of it.

**Kitty images are deleted by id.** Sixel and half-blocks are cell contents
and the next frame overwrites them. A kitty image belongs to the terminal and
survives any number of text redraws, so closing the overlay emits an explicit
delete — `a=d,d=I,i=<id>`, never the bare `a=d`, which kitty reads as "every
placement on screen" and which would take the thread's inline art with it.
`renderKitty` therefore places under an id of this client's choosing, which
it previously did not.

That transmission also gained `q=2`, which suppresses the terminal's `OK` and
error replies. Those come back on **stdin**, and under Bubble Tea's raw-mode
input loop anything on stdin is a keystroke: an unsuppressed transmission
types `_Gi=31;OK\` into whatever has focus. It is the OSC 11 hazard
(see `theme.SupportsTrueColor`) arriving by a different door, and it was
live for every inline photo this client has ever drawn on kitty.

The kitty and sixel paths are written from the protocol specifications and
are **not verified against a real terminal** in this pass. The half-block
path is the one the tests cover, and it is what `media.image_protocol =
"blocks"` selects.

### 21. No OSC 52 clipboard fallback

The phase 8 plan allows one "only if approved". It has not been approved, and
is not here.

`y` drives the platform helpers — `pbcopy`, `wl-copy`, `xclip`, `xsel`,
`clip.exe` — and reports "no clipboard tool found" when there is none, which
over ssh is the honest answer. OSC 52 would work there, and it is also the
one clipboard path a user cannot see, cannot bound, and cannot decline:
terminals accept it silently and some multiplexers forward it onward. A
feature that writes to the user's system clipboard from a remote host is a
decision for them to make, not a fallback to reach for.

### 22. A selection is marked down its whole height

Two amendments to this document, made together because they are the same
mistake seen twice.

The chat list spec gave row one "selection bar, type sigil, title, time" and
row two "a three-cell-indented preview". The grid table gave column 1 to "the
cyan cursor bar on the selected message" and the renderer read that as its
first row. Both were drawn that way and both were wrong in use: a mark on the
first line of a two-line chat marks the title, not the chat, and a five-line
message with one marked line does not read as five selected lines. The bar
now runs the full height of whatever is selected, and the goldens are redrawn
to match — the first deliberate geometry change to a fixture, which is
otherwise forbidden ("a geometry diff is a bug in the layout, not a stale
fixture"). It is recorded here so that rule keeps its force.

Dividers are excluded, on the same reasoning as the curline band: the day
divider above a message is the boundary, not the message, and a lit divider
says the cursor is on a row it cannot be on.

**The bar's colour is the focus cue, and the thread never implemented it.**
The chat list has always dimmed its bar to ghost when the panel is unfocused
— "focus is the cyan bar and nothing else", since TUI 2.0 has no
focused-panel border to carry it. The thread drew its cursor cyan
unconditionally, so both panels claimed the keyboard at once and neither one
answered "where do my keystrokes go". The thread now follows the same rule.

### 23. `}` and `{` move between messages

The design record gives `j`/`k` to line scrolling and says nothing about
moving the cursor, because it had none to move: the cursor was derived from
the scroll position and anchored back into the visible window. In use that
means "reply to the message above this one" is done by scrolling until the
right message happens to sit at the edge of the window.

`}` and `{` are vi's paragraph motions and a message is a paragraph. Both
were free — `n`/`N` cycle search hits and `[`/`]` switch folders.

The interesting part is the tail rule. At the bottom of the history the
cursor IS the newest message and follows arrivals, which is what a live chat
needs — `r` has to reply to what just came in, not to whatever the cursor was
resting on. That rule would also swallow a deliberate motion, so an explicit
one PINS the cursor and the tail rule stands down until the reader gives it
back: `G`, opening another chat, or scrolling the pinned message off screen.

A motion scrolls the minimum needed to keep its message whole on screen,
where a jump centres its target. A jump is a teleport and centring orients
the reader afterwards; stepping between adjacent messages is reading, and
re-centring the history under every press would move the text they are
looking at on every keystroke.

### 24. One spelling per action

Three actions had two or three bindings each: `ctrl+c`, `ctrl+q` and
`keys.quit` (whose default was `ctrl+c`) all quit; the palette took `up` and
`ctrl+p`, `down` and `ctrl+n`; `i` and `c` both opened the composer.

Kept: `ctrl+q` (and `keys.quit`, now defaulting to it), the arrows, and `i`.
`ctrl+c` goes because it is the chord a terminal user presses to abandon a
command rather than to close an application, and it was reachable by accident
from every panel. The arrows stay because [divergence 9](#9-the-palette-navigates-with-arrows-not-jk)
already settled that the palette is a text surface. `i` stays because it is
what the hint bar advertises and what the badge answers for.

### 25. The active folder is marked by a background, not a brighter foreground

The spec says "The active folder is bright; inactive folders are dim", and
that is what shipped. It marked nothing.

Telegram folder names are very often a single colour emoji, and **a colour
emoji ignores the foreground it is given** — the glyph carries its own
colours. On a folder row made of pictures, "bright versus dim" changed the
digit and the colon and left the thing the user actually looks at identical.
The active tab now also carries a `sel` background, which sits behind the
glyph rather than inside it.

The package had no `TestMain` pinning a colour profile, so every assertion it
made about a colour passed against an Ascii profile that emitted none. That
is the fourth package this has been true of.

### 26. Folder tabs reserve more room than they measure

The top bar drew `nnected` where it meant `● connected`: the folder tabs ran
past their budget and overwrote the connection group beside them.

**Emoji width is not a property of the string.** A terminal decides it, and
terminals disagree — a regional-indicator pair is one flag in some and two
letter-boxes in others; a base character followed by U+FE0F is narrow where
the presentation selector is ignored and wide where it is not. `ansi` and
`uniseg` agree with each other on all of these, which is what makes the
disagreement invisible from inside the program: there is nothing to compare
against and no environment variable that answers.

So the tabs now reserve **pessimistically**, in one direction only: their
measured width plus one cell for every grapheme carrying a composition rule
(U+FE0F, ZWJ, regional indicator). Over-reserving costs a wider gap or one
tab dropped early — visible, harmless, self-evident. Under-reserving corrupts
the row. Given the choice, this takes the gap.

Reservation and geometry are kept apart, which cost a bug on the way in: the
reservation decides how many tabs FIT, and their measured width decides where
they are DRAWN. Using the reservation for both put every click-target to the
right of the tab it named.

This does not make the row immune. A terminal that draws a glyph at a width
no table predicts will still shear it, and the only real fix is a terminal
that can be asked. What changed is that the failure is now bounded on the
side that mattered.

### 27. `on_open` means the overlay, not art in the history

The spec: "Image rendering is governed by ui.inline_images: never, on_open
(the default), or always. **On open**, choose Kitty graphics, Sixel,
half-blocks, configured image viewer, then platform open. A usable inline
backend opens **a dismissible full-pane overlay**... **Always may use an
eight-row card preview.**"

The implementation drew full-size art in the history for any photo whose
thumbnail had been downloaded, at both `on_open` and `always` — a different
feature wearing the same name, and unbounded where the spec bounds it.

The height was the damage. A message that grows from one line to twenty when
a thumbnail lands **invalidates the chat view's line index under the reader**,
and the scroll arithmetic and the `}`/`{` motions are computed from it. A
photo arriving mid-scroll made the next motion jump somewhere unrelated.

Now: `never` and `on_open` draw the metadata card in the thread, and the
picture appears in the full-pane overlay `enter` opens — which is what
`on_open` says. `always` draws art bounded to eight rows. The bound is
applied where the renderer is BUILT, not to its output, so the picture is
scaled to fit rather than cropped to it, and `media.max_image_height` is
deliberately overridden: that setting sizes the picture a user opens, not one
sitting inside the history.

### 28. vi's two cursors are drawn differently

In INSERT the caret is a GAP between characters — that is where typing lands,
so a block belongs before the character at the cursor. In NORMAL the cursor
sits ON a character: it is why Escape appears to step back one, why `x`
deletes "the character under the cursor", and why `i` inserts before it.

Both were drawn as a gap. So typing `12345678` and pressing Escape looked as
though the caret had jumped between the 7 and the 8; it was on the 8, drawn
as though it were before it. Normal mode now draws the caret as reverse video
on its character, and both modes fall back to the block at the end of a line,
where there is no character to sit on.

### 29. One palette, and a guard that keeps it one

The palette section says to build the theme from the semantic roles and that
"the existing DarkTheme signature remains; the hard-coded 256-colour body is
replaced". Both halves happened, and the second one only for the surfaces
that were redesigned. Six components — the palette, help, search, dialogs,
auth and contacts — kept drawing from `theme.Theme`: 268 lines of pre-built
lipgloss styles carrying their own bright blue `39` and green `42`. Every
overlay was therefore a different palette from the frame beneath it.

`theme.Theme` is deleted. What replaces it is `theme/overlay.go`: a title, a
body, a muted line, a key, a selected row, an input, an error and a success,
each a **function of `Roles`** rather than a field of a struct. The
distinction is the whole point. A table of pre-built styles has to be
constructed somewhere, so it acquires a lifecycle, a constructor, and a copy
in every component that holds one — which is exactly how the second palette
came to exist and then to drift.

`TestNoColourLiteralsOutsideThePalette` scans `internal/ui` for
`lipgloss.Color("…")` and fails on every one it has no documented reason for.
It found four more the first time it ran. Its scanner is tested separately
against a purpose-built tree, because a guard that quietly stops reporting
looks exactly like a codebase that is clean.

### 30. The avatars were still being downloaded

TUI 2.0 replaced the chat list's avatar with a type sigil (decision 10 removed
`ui.show_avatars` outright). The sigil shipped. The avatars did not leave.

The chat list kept a `media.Cache`, an `ImageRenderer`, an `avatarsLoadedMsg`,
and a command that ran on **every chat-list load** — walking the chat list and
issuing a `DownloadFileSync` per chat that had a photo, then rendering each
one to a 4×2 block — to populate a `ListItem.Avatar` field that its own
`renderRow` has never read. `widgets.List` still drew a coloured initials
block for the two surfaces with no custom row renderer.

This is the sixth time functionality moved and the old implementation stayed
alive. It is the first time the leftover was spending network requests.

### 31. Three components ignored the palette they were handed

`chatview`, `chatlist` and `composer` all took a `theme.Roles` argument in
their constructors and then installed `theme.DarkRoles(false)` — the
256-colour fallback — instead of using it. On a terminal reporting truecolour
the thread, the list and the composer would have drawn from a different
palette than the frame around them.

It was invisible because the app also called `SetRoles` at startup, which
overwrote the wrong value with the right one. Removing that redundant call —
redundant because the constructors now take the palette — is what exposed it,
and is why the three `SetRoles` methods are gone rather than kept: a setter
whose only caller existed to undo a constructor's mistake is not an API.

Runtime theme switching, which is what a `SetRoles` would be for, is still
absent and still recorded as such under phase 7. When it is built, the cache
invalidation it needs is part of building it.

### 32. The help card capped its own height and hid what it knew

`} / {` was reported as missing from the keymap. It was in the keymap. The
card had a hard 28-row height cap, so on a 60-row terminal it drew 28 rows,
left 26 empty, and put the other 58 rows of keybindings behind a scroll — and
its footer, which advertised the scroll keys, never said there was anything
to use them on. A binding below the fold is indistinguishable from a binding
that does not exist.

Three changes, all of them about the same thing:

- **The height cap is gone.** Width is still capped, because a keymap read
  across 200 columns is a keymap nobody reads; there is no such thing as a
  card too tall to read.
- **The footer says what is hidden** — `↓14 more`, or `↑6 ↓8` in the middle.
- **Two columns** when the width affords two readable ones, which halves the
  scrolling on a terminal 110 columns or wider. The split falls on a section
  boundary, so a heading is never orphaned at the foot of the left column
  with its bindings in the right.

### 33. `9{` — the vi count prefix

Message-wise motion (divergence 23) shipped without one, and a reader who
wants to go back nine messages should not press `{` nine times.

Digits are free in the thread: the chat list binds 1–9 to its folder tabs,
but that is the chat list. The prefix therefore costs no binding.

It applies to the MOTIONS and nothing else — `}`/`{`, `j`/`k`, `ctrl+d`/`u`,
the page keys. A count on a motion means "again, this many times", which is
what someone typing it expects; a count on `r` or `y` or `enter` would have
to mean something invented. Any other key clears the pending count rather
than carrying it forward, and the count is **shown in the thread header while
it is pending** — a digit that changes nothing on screen cannot be told from
a key the surface ignores, which is what a digit was here before.

A bare `0` is deliberately not a count, as in vi. It has no binding here yet,
so the guard changes nothing today; it exists so that `0` stays available.

### 34. Divergence 19, a second time — in the overlays

The panels' surfaces were fixed when the defect was found: a row assembled
out of styled spans and handed to a background style loses that background at
the first span's `ESC[0m`, because SGR is a mode and a reset is not a scope.

The overlays were still drawing from the pre-2.0 theme at the time. They were
migrated to the palette afterwards, and did not get the fix that went with
it — so the help card was painting **23 of 112 cells** on a binding row, and
every other overlay the same.

Worse, the surround: an overlay replaces the frame, and `lipgloss.Place` pads
what is left with **plain space**, so the screen around a centred card showed
the terminal's own background and the frame's surfaces went with it.

Both are fixed through `cell.FillRows`, which is `Fill` for a slice — one
function rather than a call site per surface, so the next surface written
gets it by using it. The card fills with panel; the app fills the placed
result with bg, and the card still wins inside itself because `Fill` reopens
a surface BEFORE each span's own sequences.

`TestAnOpenOverlayIsPaintedEdgeToEdge` covers all four overlays at once. The
help card additionally asserts **panel** rather than "some colour": the app's
bg fill satisfies a paint check while leaving a card's interior the wrong
colour, so the weaker assertion would have passed a half-fixed card.

`help` had no `TestMain` pinning a colour profile — the sixth package. The
rule is now worth stating plainly: **a component package that renders
anything needs one on the day it is created**, or its colour assertions pass
against an Ascii profile that emits nothing.

### 35. `quit = "ctrl+c"` is a retired default, not a choice

Removing `ctrl+c` as a quit chord (divergence 24) changed the default for
`keys.quit` from `ctrl+c` to `ctrl+q`. A config file written before that
still holds the old value, and since a configured key is shown alongside the
hardcoded one, the help card read `ctrl+q / ctrl+c` — advertising exactly the
duplicate the change removed.

`ctrl+c` joins `staleKeyDefaults`, which exists for this: a field holding a
value that was once shipped as a default was never chosen, so `-migrate-config`
replaces it rather than preserving it. The mechanism was already there; it
just had not been told about this field.

## Decisions

**All thirteen are resolved.** Decisions 1, 2, 4, and 5 were settled when this
record was written; 3 and 6 through 13 were settled on 2026-08-29. Each entry
below states what was decided and, where the choice was contested, why the
alternative was declined.

One resolution still defers work rather than removing it, and it is tracked
in TODO.md so the deferral cannot quietly become permanent: decision 5 defers
multi-attachment albums, which blocks the multi-file paste item. Decision 7's
deferral — the top-bar placeholders — has been discharged.

1. **Resolved — scope versus data model:** TUI 2.0 may add narrowly scoped
   internal/telegram and internal/store types, mappings, and RPC calls for
   design-required data and actions. Each addition must have a concrete UI
   consumer and tests; it is not a general Telegram-client expansion.
2. **Resolved — spoiler reveal binding:** x is currently unused in chat-view
   NORMAL mode, so it reveals all spoilers in the selected message. Existing
   s remains the media save/download binding. This does not affect x in the
   vi composer, where it retains its normal deletion meaning.
3. **Resolved — mode model.** Application mode is **independent of the
   composer's own emacs/vi editing submode**, and the badge states only the
   application mode. The composer's editing keymap (which `ui.compose_editing`
   infers from `$VISUAL`/`$EDITOR`) is a separate axis that the badge does not
   report; a vi composer sitting in its command state shows NORMAL, because
   the next letter will not be inserted as text, which is exactly what the
   badge promises.

   **The Escape ladder in vi mode is preserved as it is today.** The first
   Escape leaves vi insert and flips the badge to NORMAL. The second Escape
   performs the existing reply/edit/attachment cancellation, then leaves the
   composer. Escape from an ordinary draft still leaves the composer without
   discarding the draft.

   The rejected alternative was to make Escape exit INSERT first in every
   editing mode, so vi and emacs users would get an identical first Escape.
   That was declined because it would cost vi users one extra keystroke for
   every cancellation relative to today's behaviour, to buy a consistency
   that the badge already provides visually. The chosen reading also keeps
   `keys_test` passing unchanged and matches what the README documents, so
   no existing user has to relearn a ladder they already have in their
   fingers.

   In emacs mode there is no nested editor state, so the first Escape does
   the cancellation directly — unchanged from today.
4. **Resolved — Markdown send and read contract:** The supplied subset is
   sufficient. Read views style Telegram formatted-text entities in place:
   bright bold, mauve italic, amber inline code, dim strike, cyan underlined
   links, blue mentions, solid spoilers, and bordered fenced code. They never
   parse or honour HTML. The current ui.parse_markdown setting remains the
   outgoing switch: false previews and sends literal source, true previews and
   sends the existing safe subset. This retains the promise that preview is
   exactly what will be sent without silently changing existing users'
   messages.
5. **Resolved — attachments:** TUI 2.0 retains one staged attachment. The
   plural chip wording is treated as a generic visual example, not new
   multi-attachment functionality. A future album feature would require a
   slice-based composer state, ordering/caption rules, and Telegram
   multi-media upload/send support.
6. **Resolved — rail data policy:** Fetch is **deferred until the rail is
   opened**, not done on chat open. Opening a chat costs no rail request at
   all, which keeps the primary history paint free of competing work and
   means users who keep the rail off never pay for it. On open, fetch the
   current pin and the capped recent file/link results for that chat type,
   cache per chat and load generation so switching chats drops stale results,
   and update opportunistically from incoming messages while the rail stays
   open. Sections render immediately with explicit loading rows; an honest
   unavailable/error row is shown where Telegram returns nothing.
7. **Discharged — one cell wired, one deleted:** this deferred the
   *functions* behind the transport version and the device count while
   keeping both cells with placeholder text, so the layout and the shrink
   order stayed settled, and made shipping the placeholders a release
   blocker.

   The blocker is gone. The device count is wired to
   `account.getAuthorizations` and is the number Telegram's own clients show;
   the transport cell is deleted, because gotd speaks MTProto 2.0 and nothing
   else and the cell could only ever have shown one string. The shrink order
   is two steps now. See
   [divergence 6](#6-retired-the-top-bar-placeholders-one-wired-and-one-deleted),
   which also records the `frame-80x24.txt` row that the renderer had never
   produced.
8. **Resolved — command authority:** Of the commands that are not pure
   presentation, the first release ships **pin/unpin, mute/unmute, and
   reload-config**, and these are authorised to make their corresponding
   Telegram changes. **Secret chat and Markdown export are deferred.**

   `unpin` and `unmute` ship alongside `pin` and `mute` rather than being
   counted as separate commands: a pin with no unpin is a trap, and the
   inverse action shares its implementation and its authority.

   The read-only and local commands were never in question and stay in the
   first release as the plan already had them: mark-read, cross-buffer
   search, date jump, keymap, theme, and quit.

   Confirmation behaviour: pin/unpin and mute/unmute are reversible and
   execute directly, reporting the result in the hint bar's transient notice
   row. reload-config confirms first when the composer holds an unsent draft
   or a staged attachment, matching the existing quit-confirmation rule.
   Every command reports failure in that same row rather than silently
   no-opping.
9. **Resolved — responsive precedence:** At 12–19 rows the **width-based
   column layout continues**, with the top bar retained, the hint bar hidden,
   and the composer forced inline. Only below 12 rows does it collapse to the
   thread-plus-inline-composer view.

   Width narrowing has an explicit precedence, because the thread is the most
   important region: **drop the rail first, then narrow the chat list, and
   let the thread flex last.** That is what the existing threshold table
   already encodes (rail off below 118, chat list 38→30 below 90, single
   panel below 72) and it is now stated as the rule rather than left implicit
   in the numbers.

   Narrow single-panel initial state with no chat selected: show the **chat
   list**. There is nothing to render in a thread pane before a chat is
   chosen, and it puts the cursor where the next action has to happen.
10. **Resolved — configuration migration:** `ui.chat_list_width` and
    `ui.show_avatars` are **removed**, not retained as legacy no-ops. Fixed
    38/30 columns and the avatar non-goal make them meaningless, and a
    setting that silently does nothing is worse than one that is gone.
    `-migrate-config` drops both and reports each as a removed field, the way
    it already reports keys this version no longer recognises; the
    unconditional backup keeps the old values recoverable.

    `ui.mode_indicator` is **not added at all.** It was proposed to toggle the
    mode badge, but the badge is the only always-visible statement of whether
    the next letter types or navigates — a switch to hide it would let a user
    configure away the thing that makes the modal design legible. The
    remaining new keys are `ui.inline_images` and `ui.rail`.
11. **Resolved — visual sign-off:** [docs/fixtures/](fixtures/) supplies
    cell-exact goldens at 80×24, 100×30, 120×40, 137×29, and 200×60, plus a
    CJK/emoji/RTL/combining-mark/ZWJ fixture and a block gallery. All seven
    were verified line-by-line: every line is exactly its stated display width
    under both `uniseg.StringWidth` and `ansi.StringWidth`, and every file has
    exactly its stated row count. One defect was found and fixed on review —
    the ZWJ family-emoji row in `wide-runes-120x40.txt` had been padded as if
    that cluster were 8 cells wide (the per-rune sum) rather than the 2 cells
    every grapheme-aware measurement reports.

    The fixtures also forced three amendments to this document, all now
    incorporated: the narrow-pane gutter compression, the media card's
    single-line collapse, and horizontal truncation in the code pane.

    Treat the fixtures as read-only until the Go renderer exists; at that
    point the renderer becomes the generator (a `-update` flag on the test
    that writes `Model.View()` back out with ANSI stripped). The width
    assertion must pass from day one. String equality is the design contract:
    regenerate when copy changes, never when geometry does — a geometry diff
    is a layout bug, not a stale golden.

12. **Resolved — threads deferred:** `t thread` was baked into five hint-bar
    goldens but specified nowhere, and collided with the voice block's
    `transcript: t`. Threads are deferred out of TUI 2.0 entirely: the hint is
    removed, the five goldens are regenerated, and `t` belongs to the
    voice-note transcript. A future threads feature gets its own binding
    decision. See
    [divergence 2](#2-resolved-threads-are-deferred-and-t-thread-is-removed).

13. **Resolved — per-chat drafts are in scope:** The goldens stand as drawn.
    The composer keeps a draft per chat instead of one global draft, switching
    chats preserves rather than discards it, and a chat holding an unsent
    draft shows `draft: saved locally` in place of its last-message preview in
    the chat list.

    This supersedes today's behaviour, where changing chats clears the draft
    and any staged attachment. The staged attachment follows the draft it
    belongs to, since discarding one but not the other is the confusing case.

    Persistence across restarts is **not** included: drafts live in memory for
    the session. Syncing them to Telegram's own draft storage is a separate
    feature with its own sync and conflict rules, and nothing in the goldens
    asks for it. See
    [divergence 8](#8-resolved-per-chat-drafts-are-adopted-not-regenerated-away).

## Implementation plan

No phase begins until its predecessor has its agreed render tests and visual
review. Every phase remains buildable and usable.

**Two sequencing caveats the phase numbering hides.**

First, phase 1's plan to "keep existing inner chat list, chat view, and
composer rendering temporarily so this phase isolates frame risk" does not
work as written. Those components render into bordered Lipgloss panels
(`app.go`'s `renderMainScreen` uses `PanelNormal.Width(w-2)` plus
`JoinHorizontal`) which absorb width slop; none of them guarantees exact-width
lines today. Remove the borders and they shear. **Phases 1, 2, and the frame
half of 3 are one atomic change** and should be branched, reviewed, and landed
as one.

Second, phase 7 is not actually downstream of the visual work. The
interaction-mode resolver, the typed command registry, and the palette overlay
have no dependency on the frame redesign; they can be built against the
current bordered UI and be useful the day they land, and the registry is what
phase 2's context-sensitive hint bar should read from. **Start phase 7 in
parallel with phase 1**, and let phase 2 consume its registry rather than
hardcoding hints twice.

### 0. Resolve contracts and create fixtures

- Record answers to the open decisions in this document and create a concise
  ADR if the Telegram/domain scope expands.
- Define a feature matrix that marks each rail/block/command as local,
  server-backed, deferred, or removed.
- ~~Create representative deterministic fixtures.~~ **Done** —
  [docs/fixtures/](fixtures/) supplies them (decision 11), covering normal
  messages, replies, dividers, unread boundary, CJK, combining marks, ZWJ
  sequences, emoji reactions, RTL names, media, polls, code, voice, and the
  connection-state degradation ladder.
- Build the golden harness **first**: a fixture loader, an ANSI stripper, a
  per-line display-width assertion, and a row-count assertion. It is roughly
  sixty lines, it exists before the code it judges, and it turns phases 1–3
  from eyeball review into red/green. See `docs/fixtures/README.md` for the
  assertion pattern.
- Standardise on `ansi.StringWidth` from `github.com/charmbracelet/x/ansi` for
  all layout geometry, and promote `widgets.FitLine` to a shared package.
  **Do not add a display-width utility and do not promote `uniseg` to a direct
  dependency** — the helper already exists, `x/ansi` is already direct, and it
  measures every fixture identically to `uniseg`. Prohibit rune-count geometry
  in new layout code; `runewidth.RuneWidth` summed per rune is specifically
  wrong for ZWJ sequences.

Exit criterion: the golden harness runs green against the existing fixtures
for a trivial renderer, and every data-dependent feature has signed-off
behaviour.

### 1. Rendering foundations and frame

Primary files: internal/ui/theme/theme.go, internal/ui/layout/layout.go,
internal/app/app.go, internal/ui/widgets/style.go, new layout/theme tests.

- Replace theme roles and border-dependent styles; construct true-colour or
  256-colour roles once at startup while retaining a usable light inversion.
- Redesign Layout as explicit top bar, hint bar, rule, rail, header, scroller,
  reply, and composer budgets. Remove percentage list sizing from the frame.
- Build the main frame directly from width-budgeted lines and rule cells,
  instead of relying on Lipgloss borders. Preserve a safe single-panel path.
- Reuse `widgets.FitLine` as the exact-width pad/truncate helper rather than
  writing a new one; its doc comment records the four bugs it already fixed.
- Keep existing inner chat list, chat view, and composer rendering temporarily
  so this phase isolates frame risk.

Exit criterion: the five target frame sizes have exact display width on every
row, no accidental extra rows, and deterministic 256-colour output.

**Shipped.** `internal/ui/theme/roles.go` (semantic palette, truecolour and
256 resolved once at startup from the environment only), `internal/ui/layout`
(the responsive budget, pinned against the goldens' measured region widths),
`internal/ui/frame` (borderless assembly), plus `topbar` and `hintbar`.

One correction to this document's own reasoning. It claimed phases 1–3 could
not be separated, because the existing components rely on Lipgloss borders to
absorb width slop. The premise was right and the conclusion was wrong: the
fix is not to land them together but to make **the frame fit panel output
rather than trust it**. `frame.Render` fits every line to its region, so a
panel that is not yet exact-width is padded or clipped instead of shearing.
That is what lets the chat list rows and the thread grid follow separately,
and it is why phase 1 shipped alone.

`frame.Render` later took a second responsibility for the same reason: each
column's **surface**. Fitting a panel's lines and painting the region they sit
in are the same job seen twice, and only the frame can do the second one for
the rows a panel never drew. See
[divergence 19](#19-the-frame-owns-each-columns-surface-not-the-panels).

Width is asserted; byte equality is not yet. The fixtures are renders of a
finished TUI 2.0, so string equality cannot pass until the chat list rows,
the thread grid and the rail land. Separating the two assertions by lifetime
is exactly what `golden.Compare`'s DiffKind split was for.

### 2. Top bar, hint bar, and chat list

Primary files: new internal/ui/components/topbar, internal/ui/components/hintbar,
internal/ui/components/chatlist/model.go, internal/ui/widgets/tabs.go,
internal/app/app.go, and component tests.

- Move folder rendering to topbar while leaving folder selection and key
  handling in chatlist. Remove underline/padded tab treatment.
- Implement top-bar shrinking order and connection states from current events.
  Render the transport and device cells with the decision 7 placeholder text so
  the geometry matches the goldens, and record the placeholder as a release
  blocker — it must be wired or removed before shipping. **Both are settled
  now:** the device count is wired to `account.getAuthorizations` and the
  transport cell is deleted
  ([divergence 6](#6-retired-the-top-bar-placeholders-one-wired-and-one-deleted)).
- Convert chat list data to two-line rows, sigils, selection bars, muted text,
  strict title/preview/time/badge budgets, and filter header/footer.
- Render the per-chat draft state in the preview row (decision 13); the
  storage behind it lands with the composer in phase 5, so this phase can read
  an empty draft map and still match the goldens.
- Replace the status bar with a width-aware contextual hint bar, and retain
  transient notice routing. (Typing moved to the thread grid in phase 3, and
  the status bar was deleted there once nothing read it.)

Exit criterion: folder selection, filtering, mouse hit-testing, unread badges,
and every existing list/folder key continue to work with exact row widths.

**Shipped.** The rows are on the grid measured out of the goldens, not the
prose: at 38 cells the sigil sits at column 1, text starts at 3, and the
relative time occupies a FIXED five-cell field at 32. Fixed rather than
right-aligned, which is what the fixtures show and what keeps the times
aligned with each other down the list — the point of giving them a column.

`widgets.List` gained a pluggable `RenderRow`, so the bespoke row did not
require forking the cursor, scroll and hit-test machinery that already has
careful tests.

The folder tabs finished moving: `topbar.TabAt` recomputes the drawn spans
for the hit-test (so it cannot drift from what was painted) and
`chatlist.SelectFolderIndex` is the half that knows what a tab means. The old
tab bar, its filter chip and its hit-test are deleted, along with the seven
tests that were keeping them alive — each guarantee re-homed first.

### 3. Thread-grid renderer and navigation preservation

Primary files: internal/ui/components/chatview/model.go, new
internal/ui/components/chatview/grid.go, internal/render/markdown.go,
internal/app/keymap.go, internal/app/keys_test.go, and chatview tests.

- Replace bubble cache entries with immutable grid entries and exact rendered
  line counts. Keep current history paging, jump-to-message, lazy metadata
  fetch, terminal-focus read receipts, and search behaviour.
- Implement header measurement, fixed gutter/body arithmetic, sender hashing,
  local-message styling, current-line background, day/unread dividers, reply
  lookup strategy, outgoing state, and typing row.
- Make cursor selection an explicit message identity, not the current
  bottom-visible approximation. Map r/e/d/o/save/y/voice/spoiler actions to
  that identity after decision 2.
- Replace Markdown/Glamour output with entity-aware ANSI spans that wrap
  without losing styles and without interpreting HTML.

Exit criterion: a 50-message fixture aligns sender and body columns on every
line and preserves search, reply, edit, delete, read, scroll, and file actions.

**Shipped.** The gutter arithmetic is pinned against the thread widths the
five goldens actually draw at, two of which are on the narrow side of the
compression threshold — which is why that rule exists and why both sides of
it are tested.

The scroll machinery survived because dividers are attached to the message
below them rather than being separate history entries: the line index stays
one count per message, and `scrollToMessage`, `sliceLines` and
`visibleMessages` are unchanged. Selection is deliberately **not** part of the
render cache key — `View` redraws the one selected message over the cached,
unselected ones — because caching it would make the line index depend on
where the cursor is, and every scroll, jump and hit-test is built on that
index.

The cursor landed first, on its own. It is now a message identity that
sticks while its message is on screen, clamps to the nearest visible message
when a scroll carries it off, and follows the newest message while the view
is pinned to the bottom. The old rule — "the message containing the last
visible line" — was a position, and positions move on their own here: a
photo below the fold finishing its download changed which message `r` would
reply to without the user touching anything.

Three things in this section were **not** built, because the data for them
does not exist:

- **Reactions** have no field on `telegram.Message`. Phase 4 adds them with
  the mapping.
- **The red failure mark** has no source: nothing in the client reports a
  send failure. Pending, sent and read are real and are drawn; a glyph for
  an unreachable state would be decoration pretending to be information.
- **Spoiler reveal** (`x`) needs the spoiler entity, which arrives with the
  content blocks.

Two checks now mean what they say. The bubble renderer drew `✓✓` on every
message it had sent; the grid reads the chat's `LastReadOutboxMessageID`, so
one check means sent and two mean read.

Glamour is gone from `go.mod`. Entities are rendered directly as ANSI spans,
which also removes the `WithAutoStyle` hazard — the OSC 11 background probe
whose reply Bubble Tea delivered to the composer as keystrokes is now
impossible rather than guarded. The rule it taught is restated on
`theme.SupportsTrueColor`.

The typing indicator moved into the thread as the bottom row of the
scroller, with its marker in the sender column. That took the last live
responsibility off `internal/ui/components/statusbar`, which the frame had
already replaced and which is now deleted — every one of its guarantees
re-homed to `hintbar` or the top bar first. Re-homing found a real hole:
`ConnectionStateMsg` had exactly one consumer, that unrendered bar, so the
connection dot only learned the truth twice in a session. It handles the
message directly now.

### 4. Content blocks and supporting data

Primary files: new internal/ui/components/chatview/blocks.go, internal/render,
internal/media, and, if approved, internal/telegram/types.go plus mappings and
store updates.

- Implement code, quote, list, inline span, and metadata-card renderers first.
- Add reactions, poll, link, reply, and voice capabilities only where phase 0
  designated a real data source. Do not fabricate unavailable values.
- Add measured emoji/reaction framing and OSC 8 capability gating.
- Make media rendering metadata-only by default before adding any image
  overlay work.

Exit criterion: each supported block starts in the body column, respects the
84-column cap, is ANSI/wide-rune safe, and has a fixture test.

**Shipped.** `internal/render/blocks.go` (the block splitter, code frames,
quotes and list indentation), `internal/render/media.go` (the metadata
cards), and a rewritten `internal/render/entities.go`.

Entities are now a per-rune style table rather than a walk in offset order.
The old walk emitted "the gap before this entity, then this entity", which
printed every overlapped run once per entity covering it — a link inside a
bold sentence arrived on screen with its text twice. Formatting is a set of
overlapping ranges, so modelling it as "what is true of this rune" makes
nesting fall out rather than needing a case.

Code blocks are truncated horizontally, never wrapped: code is a grid whose
indentation carries the structure, and a wrapped line puts a fragment at
column zero where a new statement belongs. The frame caps at 84 cells and its
gutter compresses from `" 1   "` to `"1  "` on a narrow pane — both forms are
drawn in the goldens, at 137 and 120 columns respectively.

Lists get a hanging indent. The marker is recognised, never rewritten:
Telegram has no list entity, so anything more would be this client deciding
what somebody meant, and the recogniser is conservative enough that a
sentence with a dash in it is not a bullet.

Media cards collapse to one line below a 40-cell body, dropping the ACTIONS
rather than the facts — a reader who cannot see "enter open" can press enter
and find out, while a reader who cannot see the size has no way to learn it
from this screen.

Spoilers are drawn in their own background and `x` opens them on the message
under the cursor, as a toggle. Moving the cursor or opening a chat closes
them; a spoiler left open after the reader scrolled away is revealed to
whoever looks at the screen next.

**Fixing this phase found a bug in the previous one.** `ansi.Wrap` does not
reopen a style after a break, so a styled run spanning a wrap left the rest
of the row — trailing padding, panel rule, and the whole next column —
painted in whatever the run's style was. `cell.WrapLines` makes each wrapped
line self-contained. It is invisible in a single-column dump, which is why
`cell.OpenStyle` is exported: "this row leaves no style open" is an invariant
every component drawing into a column has to hold.

Both `internal/render` and `internal/ui/components/chatview` now pin a colour
profile in `TestMain`. lipgloss probes the output for a terminal, finds none
under `go test`, and resolves to Ascii — `Render` becomes the identity
function and every style disappears, so an assertion on styled output passes
whatever the style was. For a hidden spoiler, which IS its colour, that
assertion could only ever pass, on a screen showing the text in plain sight.

Reactions, poll results, link previews and voice waveforms are not here
because their data is not; see divergence 16 for what each would take. Media
rendering is metadata-only for everything except a photo whose thumbnail has
already been downloaded, which still draws — `ui.inline_images` (decision 10)
lands with the rest of the config migration in phase 5.

### 5. Composer and app modes

Primary files: internal/ui/components/composer/model.go, editor.go, editing.go,
new composer preview helpers, internal/app/app.go, keymap.go, config.go, and
composer/app tests.

- Add a separate app-mode state and visible badge without breaking the
  composer’s emacs/vi editing state. Per decision 3 the badge is **additive**:
  it reports the state the existing Escape ladder is already in and must not
  change how many Escapes any action costs.
- Ship the inline composer/reply bar first, then the eight-row split source and
  preview view.
- Reuse the same entity/block renderer for preview and sent-message
  presentation, governed by the resolved Markdown decision.
- Implement Ctrl-P sizing constraints, no-wrap reply/edit bars, and attachment
  display.
- Move the draft from one global buffer to one per chat (decision 13):
  switching chats preserves the draft and its staged attachment instead of
  discarding them, and the chat list preview reads from that map. In-memory
  for the session; no Telegram draft sync.
- Configuration migration per decision 10: remove `ui.chat_list_width` and
  `ui.show_avatars`, reporting each as a removed field; add `ui.inline_images`
  and `ui.rail`. Do not add `ui.mode_indicator`.

Exit criterion: one glance determines whether printables navigate or type;
`keys_test` and all existing composer tests pass **unmodified** — a diff there
means the badge changed behaviour instead of describing it — and new tests
cover the emacs and vi mode transitions, including the two-step vi Escape with
a reply, an edit, and an attachment pending.

**Shipped.** `internal/ui/components/composer/view.go` (the inline row and
the expanded form) and `drafts.go` (decision 13), plus the decision 10
configuration migration.

The badge is **derived, not set**. A focused composer knows whether the next
printable key will be inserted — that is precisely its vi state — so it says
so without being told, and a Model constructed directly by a test reports the
truth rather than whatever the host last set. COMMAND is the one exception:
the palette owns the keyboard over everything, and only the host can see it
is up. The host projects `Model.Mode()` in the same call that fills the hint
bar, so the two surfaces cannot disagree.

It covers emacs too, which the old indicator did not. `-- INSERT --` only
ever appeared in vi mode, so an emacs user had nothing on screen saying
whether the next letter would be typed. That is the same question in both
keymaps, and it is the one this phase exists to answer at a glance.

**On the exit criterion's "unmodified".** `keys_test` and every behavioural
composer test do pass unmodified. Four VIEW assertions did not, and could
not: the view is the thing being replaced. Both lines of a multi-line draft
and the Ctrl+J chord list live in the expanded form now, and the vi indicator
became the badge. Each moved with a note saying where it went, and none of
them was about the Escape ladder — which is what the criterion was protecting.

The composer's rows come out of the thread's budget explicitly:
`layout.Compute` takes what the composer asked for rather than assuming one.
A reply bar, an attachment chip and the expanded form each change that
number, and the composer emits a `ResizedMsg` rather than resizing itself,
because only the host knows what the rest of the screen is doing.

The expanded form's preview goes through `telegram.PreviewMarkdown` — the
same `parseMarkdown` the send path uses, through the same entity mapping,
rendered by the same block renderer that draws received messages. A preview
with its own parser is one that is right until the day it is not.

Drafts are parked per chat and the map means "unsent work in chats that are
not open", so restoring **consumes** the entry. Leaving a copy behind would
make the map disagree with itself the moment the draft was sent, and the chat
list reads the map.

`ui.chat_list_width` and `ui.show_avatars` are reported as **removed** rather
than as unrecognised keys — different news: an unrecognised key reads as a
typo the user should fix, a removed one is a setting that used to work.
`ui.inline_images` is added and wired; `ui.rail` is added and read but not
honoured, because asking for the rail today would reserve thirty columns and
draw nothing in them. `ui.mode_indicator` is absent, with a test that says so.

### 6. Context rail

Primary files: new internal/ui/components/rail, internal/app/app.go,
groupinfo migration/removal, and data adapters approved in phase 0.

- Port available group-info member data into non-modal rail sections.
- Add pinned/shared sections with the decision 6 policy: **no fetch on chat
  open**, fetch when the rail is opened, cache per chat and load generation,
  and show explicit loading/empty/error states rather than guessing.
- Implement width and manual visibility precedence, chat-type section sets,
  row caps, and no-wrap truncation.
- Remove groupinfo only once the rail preserves its supported information and
  interaction path.

Exit criterion: rail toggling never corrupts the frame at any threshold and
member/file data does not block chat opening.

**Shipped** as `internal/ui/components/rail`, plus one Telegram adapter.

**Nothing is fetched until the rail is opened**, and a chat whose data is
already cached is not refetched — toggling it off and on is free. Every
result carries the generation it was started for, and the cache entry's own
generation is what decides staleness. A late result for a chat that is no
longer open IS cached, deliberately: it is still correct for the chat that
asked, and `Sections` only ever reads the open chat's entry, so it cannot
appear under the wrong heading.

**Every section says what state it is in.** Not asked, waiting, refused, and
genuinely empty would otherwise all render as blank space — and only the last
of them means "this chat has no files". Every section is present in every
state, because one that vanished while loading and reappeared when it
finished would make the rail jump under the reader.

The member remainder counts the chat's total, which costs a second call:
`ChannelsGetParticipants` returns a page, not a count, and "+24 more"
computed from the page is a lie about a group of two hundred. A heading's
count appears only when rows were actually elided.

`groupinfo` is deleted. Like the status bar before it, it had been built,
sized and fed on every message since the frame landed and never drawn —
neither its `View` nor its `OpenGroupInfo` had a caller. Its one guarantee is
what the rail's members section does. `PanelGroupInfo` went with it.

`senderColour` moved to `theme.SenderColour`: the rail names the same people
the thread grid does, and a person shown mauve in one and blue in the other
is two people as far as the reader is concerned.

### 7. Command palette and command services

Primary files: new internal/ui/components/palette, new internal/app/commands
registry, internal/app/app.go, config/keymap/help integration, and tests.

- Implement the overlay, fuzzy prefix match, selection, completion, and
  palette-specific hint bar.
- Register every command in one typed table with argument validation,
  description, key equivalent, confirmation requirement, and tea.Cmd factory.
- Start with the read-only/local commands (mark-read, search, date jump,
  keymap, theme, quit), then add the three authorised by decision 8:
  pin/unpin, mute/unmute, and reload-config. Secret chat and Markdown export
  are deferred and must not be registered at all.

**Shipped**: `internal/ui/components/palette` (the overlay) and
`internal/app/commands.go` (the registry), with `mark-read`, `search`,
`keymap`, and `quit`. `:` is routed through `Model.Mode()`, so a focused
emacs composer types a colon while a vi composer in command state opens the
palette.

Still to register, each blocked on a service this build does not have rather
than on authority: `pin`/`unpin` and `mute`/`unmute` need Telegram RPCs,
`reload-config` needs runtime config reload, `theme` needs every component to
accept a new theme at runtime, and `jump` needs history-by-date. They are
absent rather than stubbed, because a palette entry that cannot run teaches
the user a command that does not exist.
- Derive keymap/help content from the registry where possible, so it cannot
  drift.

Exit criterion: palette owns input while open; invalid arguments are clear;
Escape is lossless; help and palette expose the same bindings.

### 8. Media overlay, copy, and final hardening

Primary files: internal/media, chatview blocks/model, clipboard, config,
app, documentation, and end-to-end rendering tests.

- Add explicit open flow with capability order Kitty, Sixel, blocks, configured
  viewer, platform open. Create a dismissible overlay for in-terminal modes.
- Guarantee no terminal graphic escape sequence is emitted at the default
  on_open setting before the open action.
- Add a safe text/code copy abstraction for y, including platform helpers and
  an OSC 52 fallback only if approved.
- Implement waveform/progress/pause only with a real media-player and amplitude
  design; otherwise retain a truthful play/stop card.
- Update README, config example, migration documentation, help, and project
  structure. Remove obsolete components only after their replacements ship.

Exit criterion: media works without polluting scrollback; copy has clear
failure feedback; no regression in non-graphics terminals.

**Shipped.** `internal/ui/components/mediaview` (the full-pane overlay),
`internal/clipboard`'s write side, `chatview`'s `y`/`space`/`M`, and OSC 8
hyperlinks on message bodies.

The scrollback guarantee holds by construction rather than by care: the
overlay draws into the alternate screen the app already owns, and emits no
graphics sequence of any kind until `Show` has been given a downloaded file —
which only happens after the key that asked for it.
`TestNoGraphicsBeforeAnOpen` asserts that for all three protocols, including
the "still downloading" and "download failed" states that a reader reaches
without ever seeing a picture.

Three findings, recorded as [divergence 14](#14-resolved-osc-8-hyperlinks-without-the-wrapper-they-were-costed-at),
[20](#20-the-overlay-is-photos-only-and-the-capability-order-is-not-a-ladder)
and [21](#21-no-osc-52-clipboard-fallback): the hyperlinks cost twenty lines
rather than a wrapper, the kitty transmission was replying onto stdin, and
the OSC 52 fallback stays unbuilt because it was never approved.

Not shipped, and named rather than quietly dropped: the waveform, progress
and pause controls this section makes conditional on "a real media-player and
amplitude design". There is neither. A voice note keeps the truthful play/stop
card, and `space` plays it.

## Verification matrix

Every implementation phase adds focused unit tests and preserves the existing
suite. The final integration gate is:

| Area | Required proof |
| --- | --- |
| Frame | Byte-equal to `docs/fixtures/frame-{80x24,100x30,120x40,137x29,200x60}.txt` after ANSI stripping, plus a sub-72 single-panel test; each rendered line's display width equals the frame width under `ansi.StringWidth` |
| Grid | `docs/fixtures/blocks-100x52.txt` — equal sender/body boundaries across wrapped text and every block type |
| Unicode | `docs/fixtures/wide-runes-120x40.txt` — CJK, combining marks, emoji, ZWJ sequences, and RTL sender names with no shear |
| Colour | true-colour and xterm-256 expected styles; xterm/no-italic fallback remains distinguishable |
| Interaction | existing keys_test passes unchanged except approved additions; x is reserved in chat-view NORMAL mode and s still saves media (see [divergence 1](#1-spoiler-reveal-is-x-not-s)) |
| Modes | NORMAL/INSERT/COMMAND transitions are tested with both emacs and vi composer editing; the vi Escape ladder still takes two presses to cancel a pending reply/edit/attachment and one to leave a plain draft, exactly as before the badge existed |
| Media | default on_open emits no graphics sequence until explicit open; overlay Escape/q always restores frame |
| Degradation | tmux, 256-colour, narrow width, and short height paths stay bounded and usable |

Run the complete Go suite after every phase:

    go test ./...

The width assertion must pass from day one; string equality is the design
contract. Regenerate a golden when copy changes, never when geometry does — a
geometry diff is a bug in the layout, not a stale fixture.

The design is complete only when this matrix and the handoff's visual
acceptance criteria both pass.
