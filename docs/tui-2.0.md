# TUI 2.0 design record and delivery plan

Status: proposed design, pending the decisions in [Open decisions](#open-decisions).

This document is the repository-native record of the supplied TUI 2.0
handoff. It is the implementation contract for tele-tui once the open
decisions are resolved. The original reference bundle is intentionally a
visual aid, not a source-code dependency:

- Archive: Telegram CLI TUI design.zip
- Received: 2026-08-29
- SHA-256: 84e805684c493e7ce928ac4b8adef06a7767c6ec43318c47c1edd6f82d66f8a7
- Contents: the written handoff, an HTML visual reference, and its support
  script.

The written handoff is authoritative. The HTML reference is only for visual
details omitted here. Implement in the existing Bubble Tea v2 and Lipgloss
codebase; do not port the HTML or CSS and do not add another rendering
library.

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
and force an inline composer. Below 12 rows, hide the top bar and render only
the thread with its inline composer. Rows must never wrap or exceed the
terminal width.

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
| --- | --- | --- |
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
  only colour diff-like plus/minus lines and comments. Long lines end with an
  arrow. The y action copies the original block.
- Quotes use a ghost left rule and dim italic text. Lists use cyan bullets or
  ordinals and a two-column hanging indent.
- Images, videos, and documents default to a compact metadata card with an
  IMG, VID, or DOC badge, filename, dimensions/duration/page count where
  known, size, and explicit open/save actions.
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

#### Proposed mode integration

Keep panel focus and input mode separate, but make the displayed mode a
derived, impossible-to-contradict state:

- COMMAND exists only while the palette owns input.
- INSERT means the composer has focus and the active composer editor will
  insert printable text.
- NORMAL covers chat-list/chat-view focus and a vi composer that has returned
  to its command state. In the latter case the composer still owns its vi
  commands; the NORMAL badge truthfully says that the next letter will not be
  inserted as text.

Tab, F-key/Alt focus, i/c/a, and a composer click select the composer and put
the vi editor in insert state, so they visibly enter INSERT. In emacs mode
there is no nested editor state. In vi mode, the first Escape leaves vi insert
and changes the badge to NORMAL; the next Escape follows today's
reply/edit/attachment cancellation or focus-back behaviour. In either editor,
Escape from an ordinary draft leaves the composer without discarding the
draft. For an expanded composer, Escape both returns to the inline composer
and exits INSERT; Ctrl-P is the non-destructive inline/expanded toggle.

This requires a small root-level interaction-mode resolver, not a second
independent boolean beside FocusPanel. The resolver must be the source for the
badge, key routing, and the context-sensitive hint bar. Colon is routed to the
palette only when this resolver reports NORMAL.

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
eight rows from the top. It supports fuzzy prefix matching, j/k/arrows,
Tab completion, Enter execution, and Escape cancellation. A single registry
supplies the command, argument shape, description, command constructor, help,
and palette display. The initial registry is mark-read, mute duration, unmute,
cross-buffer search, date jump, pin/unpin, Markdown export, secret chat,
keymap, theme, reload-config, and quit.

The proposed configuration additions are ui.inline_images, ui.rail,
ui.mode_indicator, and the newly documented keys. Configuration precedence and
the mode-indicator contradiction are explicitly pending decision 10.

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
| Reactions and per-message read state | Message has no reaction or read-status data | extend Telegram mapping/domain, or omit these features |
| Entity styling and code/quote blocks | formatted-text entities exist | replace Glamour-centric rendering with an ANSI-aware entity/block layout engine |
| Media metadata cards | file metadata, image backends, downloader, and external open exist | no in-TUI overlay lifecycle; current default can emit inline images while rendering |
| Voice waveform and pause | external player can play or stop audio | no amplitude extraction, progress, pause, or transcript data |
| Link previews and poll results | text and poll question only | preview metadata and poll options/results are not represented |
| Context rail | group-info can fetch members | no pinned-message or shared-file/link data path; current member query is limited |
| Top-bar transport/devices | connection state is available | no transport version or device/session count in the UI state |
| Expanded composer | textarea and outgoing Markdown parser exist | only one attachment; preview semantics conflict with optional parse_markdown |
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

Recommended policy, now that narrowly scoped Telegram changes are permitted:

1. Render the rail immediately with available chat/member data and explicit
   loading or empty rows; never delay the primary history paint.
2. When the visible rail needs a section, asynchronously fetch the current pin
   and capped recent file/link results for that chat type. Cache each section
   by chat and load generation so switching chats drops stale results.
3. Update the cached section opportunistically from incoming messages; provide
   an honest unavailable/error row when Telegram does not return that category.

This is a small, purpose-built rail data adapter, not a broad content index.
It is required to meet the handoff's persistent, chat-type-aware rail
faithfully.

## Open decisions

The first two decisions are resolved. The remainder must be resolved before
their implementation phase starts.

1. **Resolved — scope versus data model:** TUI 2.0 may add narrowly scoped
   internal/telegram and internal/store types, mappings, and RPC calls for
   design-required data and actions. Each addition must have a concrete UI
   consumer and tests; it is not a general Telegram-client expansion.
2. **Resolved — spoiler reveal binding:** x is currently unused in chat-view
   NORMAL mode, so it reveals all spoilers in the selected message. Existing
   s remains the media save/download binding. This does not affect x in the
   vi composer, where it retains its normal deletion meaning.
3. **Mode model:** Is application INSERT independent of the composer’s
   emacs/vi submode, with the badge showing only app mode? In particular,
   should Escape always exit INSERT before the current reply/edit/attachment
   cancellation behaviour, including when vi editing is selected?
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
6. **Rail data policy:** Should pinned/shared content be fetched from Telegram
   when a chat opens, derived only from locally loaded history, or deferred
   until the rail is focused/opened? This determines network cost, freshness,
   and whether the claim of no new API calls is retained.
7. **Top-bar facts:** What source and wording should be used for mtproto
   version and device count? They are not available from current connection
   events. A static label would be misleading.
8. **Command authority:** Which palette commands are required in the first
   release, and may they make their corresponding Telegram changes? Pin,
   secret chat, mute duration, export, and reload-config are not presentation
   actions. Define confirmation/error behaviour for destructive or
   privacy-sensitive commands.
9. **Responsive precedence:** At 12–19 rows, should width-based two/three
   column layout continue with a top bar but no hint bar, or should it already
   be the stated thread-plus-inline-composer view? Also define the narrow
   single-panel initial state when no chat is selected.
10. **Configuration migration:** Fixed 38/30 columns and the avatar non-goal
    make ui.chat_list_width and ui.show_avatars obsolete. Should they be
    ignored with a migration notice, removed, or retained as legacy no-ops?
    Similarly, ui.mode_indicator conflicts with the requirement that the
    badge is always visible; may it hide the badge?
11. **Visual sign-off:** Please provide terminal captures or expected rendered
    strings for the specified reference sizes (80×24, 100×30, 120×40, 137×29,
    and 200×60), including a CJK/emoji/RTL fixture. The HTML is useful for
    intent but is not deterministic terminal output.

## Implementation plan

No phase begins until its predecessor has its agreed render tests and visual
review. Every phase remains buildable and usable.

### 0. Resolve contracts and create fixtures

- Record answers to the open decisions in this document and create a concise
  ADR if the Telegram/domain scope expands.
- Define a feature matrix that marks each rail/block/command as local,
  server-backed, deferred, or removed.
- Create representative deterministic fixtures: normal messages, replies,
  same-day and multi-day history, unread boundary, long URLs, CJK, combining
  marks, emoji reactions, RTL names, all chat types, media, polls, and
  connection states.
- Add a shared display-width utility based on the existing uniseg dependency;
  prohibit rune-count geometry in new layout code.

Exit criterion: signed-off behaviour for every data-dependent feature and
golden fixtures that can drive the rendering tests.

### 1. Rendering foundations and frame

Primary files: internal/ui/theme/theme.go, internal/ui/layout/layout.go,
internal/app/app.go, internal/ui/widgets/style.go, new layout/theme tests.

- Replace theme roles and border-dependent styles; construct true-colour or
  256-colour roles once at startup while retaining a usable light inversion.
- Redesign Layout as explicit top bar, hint bar, rule, rail, header, scroller,
  reply, and composer budgets. Remove percentage list sizing from the frame.
- Build the main frame directly from width-budgeted lines and rule cells,
  instead of relying on Lipgloss borders. Preserve a safe single-panel path.
- Introduce frame helpers that pad/truncate ANSI and wide text to exact widths.
- Keep existing inner chat list, chat view, and composer rendering temporarily
  so this phase isolates frame risk.

Exit criterion: the five target frame sizes have exact display width on every
row, no accidental extra rows, and deterministic 256-colour output.

### 2. Top bar, hint bar, and chat list

Primary files: new internal/ui/components/topbar, internal/ui/components/statusbar,
internal/ui/components/chatlist/model.go, internal/ui/widgets/tabs.go,
internal/app/app.go, and component tests.

- Move folder rendering to topbar while leaving folder selection and key
  handling in chatlist. Remove underline/padded tab treatment.
- Implement top-bar shrinking order and connection states from current events;
  implement device/transport only after decision 7.
- Convert chat list data to two-line rows, sigils, selection bars, muted text,
  strict title/preview/time/badge budgets, and filter header/footer.
- Convert statusbar to a width-aware contextual hint bar and retain typing and
  transient notice routing.

Exit criterion: folder selection, filtering, mouse hit-testing, unread badges,
and every existing list/folder key continue to work with exact row widths.

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
  composer’s emacs/vi editing state.
- Ship the inline composer/reply bar first, then the eight-row split source and
  preview view.
- Reuse the same entity/block renderer for preview and sent-message
  presentation, governed by the resolved Markdown decision.
- Implement Ctrl-P sizing constraints, no-wrap reply/edit bars, attachment
  display, and configuration migration.

Exit criterion: one glance determines whether printables navigate or type, and
all existing composer tests plus new emacs/vi mode transition tests pass.

### 6. Context rail

Primary files: new internal/ui/components/rail, internal/app/app.go,
groupinfo migration/removal, and data adapters approved in phase 0.

- Port available group-info member data into non-modal rail sections.
- Add pinned/shared sections only with their approved data policy and explicit
  loading/empty/error states.
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
- Start with read-only/local commands, then add server-mutating commands only
  when explicitly authorised by the resolved scope.
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
| Frame | 80×24, 100×30, 120×40, 137×29, 200×60, and a sub-72 single-panel test; each rendered line has the expected display width |
| Grid | 50-message fixture with equal sender/body boundaries across wrapped text and every block |
| Unicode | CJK, combining marks, emoji, and RTL sender-name fixture with no shear |
| Colour | true-colour and xterm-256 expected styles; xterm/no-italic fallback remains distinguishable |
| Interaction | existing keys_test passes unchanged except approved additions; x is reserved in chat-view NORMAL mode and s still saves media |
| Modes | NORMAL/INSERT/COMMAND transitions are tested with both emacs and vi composer editing |
| Media | default on_open emits no graphics sequence until explicit open; overlay Escape/q always restores frame |
| Degradation | tmux, 256-colour, narrow width, and short height paths stay bounded and usable |

Run the complete Go suite after every phase:

    go test ./...

The design is complete only when this matrix and the handoff's visual
acceptance criteria both pass.
