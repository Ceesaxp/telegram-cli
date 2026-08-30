# TUI 2.0 design record and delivery plan

Status: **contracted.** All thirteen decisions are resolved (see
[Decisions](#decisions)); implementation may begin. What remains before
release is engineering and one deferral to retire — the top-bar placeholder
described in decision 7.

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
  right-aligned relative time. Row two is a three-cell-indented preview plus a
  right-aligned unread badge.
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
| 1 | cyan cursor bar on the selected message |
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
| Context rail — pinned | `ChannelsGetFullChannel` is **already called** in `internal/telegram/groups.go` and already returns `pinned_msg_id`; the mapping into `SupergroupFullInfo` simply drops it | cheap: one field on the mapping plus the existing `GetMessage` call |
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

### 6. Resolved: the goldens carry deliberate top-bar placeholders

Four frame fixtures render the top bar with a transport version and a device
count, neither of which has a source in the current connection state.
Decision 7 resolved this by **deferring the functions while keeping the
cells**: the goldens read `connected · mtproto 2.0 · devices 1`, so the
layout and the shrink order are pinned, and `frame-80x24.txt` shows the
degraded `connected │ 21:04` form after both cells drop.

The strings are placeholders and are recorded as such. Shipping them as
though they were live status would be a lie in the UI, so wiring them to a
real source — or removing the two cells and regenerating those rows — is a
**release blocker**, tracked in TODO.md rather than left as a design
question.

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
for the same reason the top bar's placeholders are a recorded release
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

## Decisions

**All thirteen are resolved.** Decisions 1, 2, 4, and 5 were settled when this
record was written; 3 and 6 through 13 were settled on 2026-08-29. Each entry
below states what was decided and, where the choice was contested, why the
alternative was declined.

Two resolutions defer work rather than removing it, and both are tracked in
TODO.md so the deferral cannot quietly become permanent: decision 7 leaves
placeholder text in the top bar that must not ship, and decision 5 defers
multi-attachment albums, which blocks the multi-file paste item.

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
7. **Resolved for now — top-bar facts deferred:** The *functions* that would
   source the transport version and device count are deferred; the **cells
   stay in the design** with placeholder text, so the layout, the shrink
   order, and the goldens are all settled. The goldens read
   `connected · mtproto 2.0 · devices 1`, with `frame-80x24.txt` showing the
   degraded `connected │ 21:04` form after both cells are dropped.

   **These are placeholders and must not ship as if they were real.** Before
   release, either wire the values to a real source or drop the two cells and
   regenerate the affected top-bar rows; a hard-coded `mtproto 2.0` presented
   as live status would be a lie in the UI. Tracked as a release blocker in
   TODO.md, not as a design question.
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
  blocker — it must be wired or removed before shipping.
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
