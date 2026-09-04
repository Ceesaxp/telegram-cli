# Handoff: teletui TUI redesign

> **Archive.** This is the brief the TUI 2.0 redesign was written from, kept
> unedited as the record of what was asked for. It is not current: the
> keymap it assumes was rewritten by the 2026-09-04 interaction review, so
> where it names a binding — `alt+1/2/3`, `}`/`{`, `M` — read
> [interaction-model.md](../interaction-model.md) instead.

## Overview

A redesign of the `teletui` terminal client's entire visual layer: chrome, chat
list, message rendering, composer, and two new surfaces (a right context rail
and a `:` command palette). The goal is a client that reads as a native
terminal tool — columnar, dense, keyboard-first — rather than a chat GUI
transcribed into a terminal.

Target repo: `github.com/ceesaxp/telegram-cli` (`cmd/teletui`, Bubble Tea v2 +
Lipgloss). The redesign is **presentation-layer only**. No changes to
`internal/telegram`, `internal/store`, `internal/restapi`, or `internal/mcpserver`.

## About the design files

`Telegram TUI.dc.html` in this bundle is a **design reference built in HTML** —
a prototype of intended look and behavior, not code to port. It exists because
HTML iterates faster than a TUI does. Everything in it is expressed in
monospace type on a fixed grid precisely so it translates to cells.

**Implement it in the existing Go codebase**, using Bubble Tea v2 components and
Lipgloss styles as the repo already does. Do not introduce a new rendering
library. Do not port CSS.

Read this README as the spec; open the HTML when you need to see a detail this
document didn't nail down. Keyboard interaction in the prototype is partial
(`j/k/g/G`, `i`, `:`, `esc`) — the authoritative keymap is
`internal/app/keymap.go`, and this redesign **preserves it**. Only the additions
in §7 are new bindings.

## Fidelity

**High-fidelity.** Colors, column arithmetic, glyphs, and copy are final.
Reproduce the layout cell-for-cell. Where the prototype's pixel measurements and
this document's cell counts disagree, **the cell counts here win** — they are the
translation, and they are what a terminal can actually render.

Translation constant used throughout: the prototype is 13px JetBrains Mono,
cell ≈ 7.8px × 19.5px. 308px chat list → **38 cols**; 244px rail → **30 cols**.

---

## 1. Frame geometry

Full-screen layout, five regions. All dimensions in cells.

```
┌──────────────────────────────────────────────────────────────────────┐
│ top bar                                                        1 row │
├────────────────┬────────────────────────────────────┬────────────────┤
│ chat list      │ thread header               1 row  │ context rail   │
│ 38 cols        ├────────────────────────────────────┤ 30 cols        │
│                │ message scroller           flexes  │                │
│                ├────────────────────────────────────┤                │
│                │ reply bar (conditional)     1 row  │                │
│                │ composer            1 row / 8 rows │                │
├────────────────┴────────────────────────────────────┴────────────────┤
│ hint bar                                                       1 row │
└──────────────────────────────────────────────────────────────────────┘
```

**No box borders anywhere.** Panels are separated by single-cell rules:
vertical `│` in the rule color, horizontal `─` full width. This replaces
`theme.PanelNormal` / `theme.PanelFocused` (rounded borders), which are
deleted — see §8.

Focus is **not** shown by a border. Focus is shown by (a) the selection bar in
the focused panel becoming the cyan accent instead of dim, and (b) the mode
badge in the composer.

### Responsive rules — replaces `layout.Compute`

| Terminal width | Behavior |
|---|---|
| ≥ 118 cols | Three columns: 38 / flex / 30 |
| 90–117 | Rail hidden. Two columns: 38 / flex |
| 72–89 | Rail hidden, chat list narrows to 30 |
| < 72 | Single panel: chat list **or** thread, `tab` swaps (existing `SinglePanel` path) |

| Terminal height | Behavior |
|---|---|
| ≥ 20 rows | Full frame |
| < 20 | Hint bar hidden; expanded composer forced back to inline |
| < 12 | Top bar hidden too; thread + inline composer only |

The rail is also toggled manually by `` ` `` (§7) — the width rule only forces
it off, never back on against the user's choice.

---

## 2. Palette

True color, with an xterm-256 fallback index for terminals that report ≤256.
Detect once at startup (`lipgloss.ColorProfile()` / `COLORTERM`) and build the
theme from whichever table applies. `theme.DarkTheme()` currently hardcodes
256-index colors — replace its body, keep its signature and struct shape.

| Role | Hex | 256 | Used for |
|---|---|---|---|
| `bg` | `#0b0d10` | 232 | app background, scroller |
| `panel` | `#0e1116` | 233 | chat list, rail, header, composer |
| `chrome` | `#12151a` | 234 | top bar, hint bar |
| `sel` | `#171d24` | 235 | selected chat row |
| `curline` | `#12171d` | 234 | cursored message row |
| `rule` | `#1f242b` | 236 | panel separators |
| `rule-soft` | `#1a1f26` | 235 | in-panel dividers, date rules |
| `border` | `#262d36` | 237 | attachment / code / poll frames |
| `ghost` | `#3f4750` | 239 | `│` separators, inert glyphs |
| `faint` | `#465059` | 240 | timestamps, byte counts |
| `dim` | `#5c666e` | 243 | subtitles, secondary copy |
| `fg` | `#c9ced4` | 252 | message body |
| `bright` | `#e2e7ec` | 255 | active chat title, bold spans |
| `cyan` | `#6fb8c9` | 73 | **accent**: cursor, keys, focus, unread badge |
| `amber` | `#d1a86a` | 179 | command mode, attachments, channels, inline code |
| `green` | `#86b57a` | 108 | INSERT mode, online, own messages, sent |
| `mauve` | `#b58ac9` | 139 | groups, italic spans, sender color |
| `blue` | `#8aa8d0` | 110 | DMs, mentions, sender color |
| `red` | `#c9736a` | 167 | errors, failed sends, diff removals |

Two rules that the current theme violates and this one enforces:

1. **No background fills on message text.** `MessageBubbleOwn` /
   `MessageBubbleOther` (backgrounds 24 and 237) are deleted. Authorship is
   carried by the sender-name color and body brightness, never by a bubble.
2. **Accent is cyan and only cyan.** Amber/green/mauve/blue are semantic, not
   decorative. Nothing is colored to be pretty.

Light theme: keep `LightTheme()` as an inversion of the same roles. Not
specified further; dark is the reference.

---

## 3. Top bar (1 row, `chrome` bg)

```
 tg │ 1:all 2:unread 3:work 4:channels 5:archive        ● connected · mtproto 2.0 · 3 devices │ 21:04
```

- `tg` in cyan bold, then a `│` in ghost.
- Folder tabs from `internal/telegram/folders.go`, numbered from 1. Active tab
  `bright`, others `dim`. **No underline, no padding boxes** — `widgets.Tabs`'
  `TabActive` underline style is dropped; digit-prefix + brightness is the
  affordance. `widgets.Tabs` keeps its `1-9` / `h` `l` / `[` `]` Update logic.
- Right group, right-aligned, elides left-to-right as width drops: the clock
  never elides, the status line truncates with `…`, then the device count is
  dropped, then the transport.
- `●` is green when connected, amber while connecting, red when down (drive
  from the same state `statusbar` reads today).

Tabs move **out of** `chatlist` and into this bar; chatlist keeps the key
handling and emits a folder-change message.

---

## 4. Chat list (38 cols, `panel` bg)

### Header row (1 row)

```
 / filter chats…                                  9/9
```

`/` in amber. Placeholder in dim. Right side is `matching/total`, ghost. When a
filter is active the placeholder is replaced by the live query in `fg` and the
count updates.

### Chat rows (2 rows each)

```
▌# infra-oncall                            2m
   nadia: rebased, CI green                 4
```

Column arithmetic for a 38-col list:

| Cols | Content |
|---|---|
| 1 | selection bar: `▌` cyan when selected **and** panel focused, `▌` ghost when selected and unfocused, space otherwise |
| 1 | sigil |
| 1 | space |
| flex | name, truncated with `…` |
| 1 | space |
| ≤5 | relative time, right-aligned, `faint` |

Line 2 is indented 3 cols (under the name), carries the preview text in
`faint`, and right-aligns the unread badge.

**Sigils** — the type of a chat is one glyph, not an icon:

| Glyph | Type | Color |
|---|---|---|
| `@` | direct message | blue |
| `#` | group / supergroup | mauve |
| `!` | channel (read-only) | amber |
| `~` | saved messages | green |

**Name color:** `bright` if selected · `fg` if unread and unmuted · `#98a1a9`
otherwise. A muted chat gets the literal word `muted` in `faint` after the
name (truncate the name first, never the marker).

**Unread badge:** count on cyan with `bg`-colored text, `999+` above 999
(`widgets.RenderBadge` already does the clamp). A **muted** chat's badge is
`#39424b` — present, not shouting.

### Footer (1 row)

`j/k move  g/G ends  u unread` — keys in cyan, labels in faint.

---

## 5. Thread pane

### Header (1 row, `panel` bg)

```
 # infra-oncall │ group · 24 members · 6 online          buf 2 │ ln 214/214  bot
```

Left: sigil, name in `bright` bold, `│`, subtitle in dim. Right: buffer number
and scroll position, both `faint`. **The right group never wraps and never
elides; the subtitle absorbs all shrink** and truncates with `…`. (This was a
real bug in the prototype — the fixed-height row wrapped and clipped. In a
terminal the equivalent is a corrupted frame, so build the header by measuring
the right group first, then giving the subtitle `width - used`.)

`buf N` is the chat's slot in the current folder — it makes `2` (§7) a
predictable jump, and it is why the design numbers buffers at all.

### Message rows — the core change

Messages are a **fixed four-column grid**, not bubbles. The eye scans one
column for time, one for sender, one for content.

```
 ▌ 21:01          nadia  Rebased onto main, CI is green now. 4412 ready for
                         the second approval.
   21:02            sam  Approved. Merging behind the flag.
                         🚀 4
```

| Cols | Content |
|---|---|
| 1 | space |
| 1 | cursor: `▌` cyan on the message under the cursor, else blank |
| 1 | space |
| 5 | `HH:MM`, `faint` |
| 2 | space |
| 12 | sender, **right-aligned**, truncated with `…`, per-sender color |
| 2 | space |
| flex | body |

Gutter = **24 cols**. Body width = `threadWidth - 24 - 1`.

**Narrow-pane amendment.** When `threadWidth - 24 - 1 < 32` the sender column
compresses from 12 to 8 and the gutter to **20**. Without this, a 120-col
terminal with the rail on leaves a 25-col body, which is unreadable. Triggers
at 80×24 and 120×40 — see `fixtures/`. Wrapped lines and
every block element align to the body column — the gutter is empty on
continuation rows. Word-wrap; never hard-break a word except a URL.

The cursored row gets a `curline` background across the **full pane width**.

**Sender color** is assigned deterministically: hash the user ID into
`[mauve, cyan, blue, amber]`. Your own messages are always green and named
`you`, with body text one step dimmer (`#aab3bb`) than others' `fg` — the
asymmetry that a bubble used to carry.

**Date dividers** — a left-aligned label followed by a rule to the right edge:

```
 TODAY ─────────────────────────────────────────────────────────
```

`faint` label, `rule-soft` line. The **unread divider** is the same shape in
amber with an amber rule and reads `4 NEW`. It is sticky: it stays where it
was when the buffer was opened until the buffer is left or `M` is pressed.

**Reply quote** — one line above the body, ghost `↳`, then the replied-to
sender in that sender's color, then the quoted text truncated to the body width:

```
                         ↳ ivo 4412 ready for the second approval?
```

**Reactions** — a row under the body, each `emoji count` in a 1-cell-padded
box drawn with `border` color. Terminals disagree about emoji width; measure
with `uniseg`/`runewidth` and pad to the measured width, or the whole grid
shears.

**Message state** for your own messages: a single glyph appended after the
body, `faint` — `·` sending, `✓` sent, `✓✓` read, `✗` failed (red, and the
row keeps the retry affordance the app already has via `SendFailedMsg`).

### Typing indicator

Bottom of the scroller, in the sender column position: `···` ghost, then
`nadia is typing…` in dim. Never shifts message layout — it occupies the row
above the composer whether or not anyone is typing.

---

## 6. Content blocks

`internal/render/markdown.go` already parses Telegram entities. This section
defines what each parsed thing **looks like**. All blocks start at the body
column and are capped at `min(bodyWidth, 84)` cols.

**Inline spans** (rendered in place — the reader never sees raw syntax):

| Entity | Rendering |
|---|---|
| bold | `bright`, bold attr |
| italic | mauve, italic attr |
| code | amber on `#161b21`, one space of padding each side |
| strikethrough | `#6f7982` + strikethrough attr |
| spoiler | text painted in its own background color; `s` on the cursored message reveals every spoiler in it |
| link | `#8fd0df` underlined; OSC 8 hyperlink when the terminal supports it |
| mention | blue, medium weight |
| pre / code block | see below |

Anything not in that table renders as plain text. **HTML is never interpreted.**

**Code block** — a framed pane, `border` color, `bg` interior:

```
 ┌ sql ──────────────────────── 4 lines · y to yank ┐
 │  1  SELECT s.* FROM sessions s                   │
 │  2    JOIN devices d ON d.sid = s.id             │
 │  3  - WHERE s.touched > now() - '7d'::interval   │
 │  4  + WHERE s.touched > now() - '1h'::interval   │
 └──────────────────────────────────────────────────┘
```

Language tag amber, line numbers `#333b45`, code `#9aa4ac`. Diff-ish lines get
green (`+`) / red (`-`), comment lines `faint`. **No syntax highlighting beyond
that** — a TUI chat client is not an IDE, and per-language lexers are scope
creep. Lines longer than the pane are horizontally truncated with `→` at the
edge; `y` yanks the block verbatim via `internal/clipboard`.

**Blockquote** — a `ghost` `│` bar in the first body column, text in `dim`
italic.

**Lists** — `·` bullets (cyan) or `1.` ordinals, hanging indent of 2.

**Media — the important decision: never render a picture inline by default.**

```
 ┌──────┐  ▣ auth-p95-2608.png
 │ IMG  │  1440×720 · 184 KB · png
 └──────┘  o open · s save
```

A metadata card: type badge in a small frame, filename in `fg`, dimensions and
size in `faint`, and the actions with keys in cyan. This is deliberate — an
image dumped into a scrollback destroys the scroll model, and half the world's
terminals can't draw one anyway.

`internal/media` already has sixel, kitty, and block-glyph backends. Wire them
to **`o`, on demand only**, resolving in this order: kitty graphics → sixel →
half-block glyphs → `$IMAGE_VIEWER` → `xdg-open`/`open`. When an inline backend
is available, the image opens as a full-pane overlay dismissed with `esc` or
`q`, not as scrollback. Add `ui.inline_images = "never" | "on_open" | "always"`
to config; default `on_open`. `always` renders a ≤8-row preview in the card's
place, for people with kitty who want it.

Videos and documents are the same card with a `VID` / `DOC` badge and duration
or page count where known.

**Voice notes** — the one place a graphic earns its keep:

```
 ▶ ▁▃▅▇▅▃▂▄▆█▆▄▃▅▇▄▂▃▅▆▃▁  0:47  transcript: t
```

Waveform from the amplitude data `internal/media/voice.go` extracts,
downsampled to 24 cells, drawn with `▁▂▃▄▅▆▇█`. Played portion green,
unplayed `#39424b`. Space plays/pauses on the cursored voice note.

**Link preview** — a cyan `│` bar, then host (`faint`), title (`#8fd0df`),
description (`dim`, 2 lines max).

**Poll** — framed, question in `fg`, then per option `◉`/`○`, label, a 1-row
bar of `█`/`░` scaled to the pane, and right-aligned percent. Voted option in
green. Footer line: `11 votes · anonymous · closes 23:00` in ghost.

---

## 7. Composer and modality

The redesign makes the client **modal**, which is the single biggest behavioral
change. Justification: every other keystroke in this app is already a vi-ish
motion; making the mode explicit removes the "will this letter navigate or
type?" hesitation that the current focus model has.

### Modes

| Mode | Badge | Entered by | Left by |
|---|---|---|---|
| `NORMAL` | cyan | default; `esc` from anywhere | — |
| `INSERT` | green | `i`, `c`, `a`, clicking the composer | `esc` |
| `COMMAND` | amber | `:` | `esc`, or running a command |

The badge is `bg`-colored text on the mode color, 1 cell of padding, at the far
left of the composer row. It is the only always-visible statement of mode.

This maps onto the existing focus model rather than replacing it:
`PanelComposer` + focused ⇒ INSERT. `PanelChatList` / `PanelChatView` ⇒ NORMAL.
COMMAND is a new overlay state. **Keep `tab` cycling, keep `alt+1/2/3`, keep
every binding in `keymap.go`** — modes are an additional, more legible way to
express what focus already meant.

### Inline composer (1 row, default)

```
 NORMAL › i to compose · : for commands                          md
```

Badge, then a `›` prompt in the mode color, then the draft or the hint. Right
side: `md` when the draft is empty, character count once it isn't.

### Reply bar (1 row, above the composer, conditional)

```
 reply ↳ nadia: Rebased onto main, CI is green now.       esc to drop
```

Amber label, ghost `↳`, quoted text in `dim` truncated with `…`. Same
no-wrap discipline as the thread header: the label and the right hint are
fixed; the quote absorbs shrink. An edit-in-progress uses the same bar with an
amber `edit` label.

### Expanded composer (8 rows) — `i` on a multi-line draft, or `ctrl+p`

A split pane: source on the left, rendered result on the right.

```
 INSERT  markdown │ wrap 92 │ 187 ch · 1 attachment · draft synced   preview on
 ─────────────────────────────────┬──────────────────────────────────────────
  1  Rolling **0.4.2** tonight —   │ RENDERED
  2                                │ Rolling 0.4.2 tonight — keymap work
  3  Patch attached; it caps the   │ rides 0.5.
  4▌ retry at `30s` with ±20%…     │ Patch attached; it caps the retry at
                                   │ 30s with ±20% jitter.
     ▤ backoff.patch  2.1 KB  ✕    │
 ─────────────────────────────────┴──────────────────────────────────────────
 ⏎ newline  ^d send  ^a attach  ^p preview  ^e $EDITOR  ^s save draft
```

- Left: line numbers (`#333b45`, cursor line cyan), source with markdown
  **markers kept but dimmed to `#39424b`** and the marked-up text already
  styled — you see `**` and bold simultaneously, so nothing is hidden while
  you type.
- Right: the same text rendered exactly as it will appear in the thread.
- Staged attachments are chips under the source, `✕` to drop.
- The existing `widgets/textarea.go` provides the editing buffer; both vi and
  emacs `composer.EditingMode()` keymaps continue to work unchanged inside it.
- Collapses back to inline on `esc` or `^p`.

### New bindings (everything else in `keymap.go` is untouched)

| Key | Context | Action |
|---|---|---|
| `:` | NORMAL | open the command palette |
| `1`–`9` | NORMAL, chat view | jump to buffer N in the current folder |
| `` ` `` | any | toggle the context rail |
| `^p` | composer | toggle expanded/inline |
| `s` | chat view | reveal spoilers in the cursored message |
| `y` | chat view | yank the cursored message (code block if the cursor is on one) |
| `space` | chat view | play/pause a cursored voice note |
| `M` | chat view | mark read, keep scroll position |

`1`–`9` collides with `widgets.Tabs`' folder jump — resolve by scope: digits
switch **folders** while the chat list has focus (unchanged), and switch
**buffers** while the chat view has focus (new).

### Command palette (`:`) — new component `internal/ui/components/palette`

Centered overlay, 60 cols, anchored ~8 rows from the top, over a dimmed frame.

```
 : ma▌
 :mark-read       mark buffer read, keep position                 M
 :mute 8h         mute this chat for 8 hours                     m8
 :search <query>  search across all buffers                       /
 :jump 26aug      jump to date in history                        gd
 :pin             pin message under cursor                        p
 :export md       export buffer as markdown
 ↵ run · ⇥ complete · esc cancel
```

Fuzzy prefix match, selected row on `#181f27` with the command in amber,
descriptions `#6f7982`, the equivalent key binding right-aligned in ghost so
the palette teaches the keymap. `j/k` or arrows move, `⇥` completes, `↵` runs.

Ship this command set: `mark-read`, `mute <duration>`, `unmute`, `search
<query>`, `jump <date>`, `pin`, `unpin`, `export md`, `secret <@user>`,
`keymap`, `theme <name>`, `reload-config`, `quit`. Each is a struct with a
name, an arg spec, a description, and a `tea.Cmd` constructor — registered in
one table so `:keymap` and the help overlay can both read it.

---

## 8. Context rail (30 cols, `panel` bg, right)

Replaces the `groupinfo` **overlay** with a persistent rail. Sections stack;
each has a `faint` letter-spaced header and rows that truncate with `…`.

```
 PINNED
 ▪ Deploy window 14:00–15:30   ivo
 ▪ Runbook: auth p95 spike   nadia

 MEMBERS · 24
 ● ivo                      admin
 ● nadia
 ○ mira                        4h
 · +19 more

 FILES
 ▣ auth-p95-2608.png         184K
 ▤ incident-0812.md            6K
```

- Pinned: amber `▪`, text `#9aa4ac`, author `ghost`.
- Members: green `●` online / `#39424b` `○` offline, name in that member's
  sender color, role or last-seen right-aligned. Cap at 8 rows + `+N more`.
- Files: amber type glyph, name, size. Cap at 6.
- Section set is chat-type dependent: a DM shows `SHARED FILES` and
  `SHARED LINKS`, no members; a channel shows `PINNED` and `FILES` only.

Rows are pure data, so this needs no new API calls beyond what `groupinfo`
already fetches.

---

## 9. Hint bar (1 row, `chrome` bg, bottom)

```
 q quit  i compose  : command  r reply  t thread  y yank  e edit  ? keymap    idx 214 msgs · 9 buffers · 37 unread
```

Keys cyan, labels `faint`, right group ghost. **Context-sensitive**: the set
changes with focus and mode — in INSERT it shows the composer's chords, with
the palette open it shows `↵ run · ⇥ complete · esc cancel`. Drop hints from
the right as width shrinks; never wrap, never truncate a hint mid-word.

This absorbs today's `statusbar` component; connection state moves to the top
bar (§3), transient errors and progress take over the whole row for 4s in
`red`/`amber` and then it reverts.

---

## 10. Files to change

| Path | Change |
|---|---|
| `internal/ui/theme/theme.go` | Replace palette with §2; delete `PanelNormal`/`PanelFocused` borders, `MessageBubbleOwn`/`Other`; add rail, palette, mode-badge, block styles; add truecolor/256 detection |
| `internal/ui/layout/layout.go` | Rewrite `Compute` for §1: add top bar, rail, thresholds table |
| `internal/ui/components/chatlist/model.go` | 2-line rows, sigils, selection bar, muted treatment (§4); hand folder tabs to the top bar |
| `internal/ui/components/chatview/model.go` | The big one: columnar grid, gutter arithmetic, dividers, reply quotes, reactions, state glyphs (§5) |
| `internal/ui/components/chatview/blocks.go` | **New.** Block renderers: code, quote, list, media card, voice, link, poll (§6) |
| `internal/ui/components/composer/model.go` | Mode badge, inline/expanded split, live preview pane (§7) |
| `internal/ui/components/statusbar/model.go` | Becomes the hint bar (§9) |
| `internal/ui/components/topbar/` | **New.** §3 |
| `internal/ui/components/rail/` | **New.** §8; absorbs `groupinfo` |
| `internal/ui/components/palette/` | **New.** §7 |
| `internal/ui/components/groupinfo/model.go` | Delete after the rail lands |
| `internal/ui/widgets/tabs.go` | Drop `TabActive` underline/padding; keep Update |
| `internal/render/markdown.go` | Map entities to the §6 span styles; add the raw-with-dimmed-markers mode the expanded composer needs |
| `internal/media/*` | Wire the backends to on-demand `o` + the overlay viewer; add `ui.inline_images` |
| `internal/app/app.go` | Wire new components, mode state, `:` dispatch, new bindings |
| `internal/app/keymap.go` | Add §7 bindings to the table **and** its prose comment; keep everything existing |
| `internal/config/config.go` | New keys: `ui.inline_images`, `ui.rail` (bool), `ui.mode_indicator` (bool), `[keys]` entries for §7 |

---

## 11. Build order

Each phase leaves the client shippable. Do not start a phase before the
previous one's frames look right.

1. **Palette + frame.** Theme rewrite, `layout.Compute`, borderless rules, top
   bar, hint bar. Everything else keeps rendering as-is inside the new frame.
2. **Chat list.** 2-line rows, sigils, selection bar.
3. **Message grid.** Columnar rendering, dividers, replies, reactions. Delete
   the bubbles. This is where most of the risk is — wrapping, wide runes, and
   emoji width all bite here.
4. **Content blocks.** Code, quote, list, link, poll, then media card, then
   voice waveform.
5. **Composer.** Mode badge and inline row first; expanded split second.
6. **Rail.** Port `groupinfo` content, then delete the overlay.
7. **Command palette.** Registry, fuzzy match, the command set.
8. **Media backends.** On-demand open, overlay viewer, config.

## 12. Acceptance criteria

Per phase, and all of them at once at the end:

- **Frame integrity.** At 80×24, 100×30, 120×40, 200×60, and one non-standard
  size (137×29): no wrapped row anywhere, no line exceeding the terminal width,
  no panel bleeding into another. The repo's `_test.go` convention makes this
  cheap — render `Model.View()` into a string and assert every line's display
  width equals the frame width.
- **Column alignment.** In a thread of 50 messages, the sender column's right
  edge and the body column's left edge are the same for every row including
  wrapped continuations and every block type.
- **Wide runes.** A thread containing CJK text, emoji reactions, combining
  accents, and an RTL name renders with no shear. Measure with
  `rivo/uniseg`, not `len()`.
- **Degradation.** Runs correctly under `TERM=xterm-256color` (256 palette),
  `TERM=xterm` (no truecolor, no italics — spans must stay distinguishable
  without them), and inside `tmux`.
- **Keymap preservation.** Every binding documented in today's `keymap.go`
  still works and still appears in `?`. The existing `keys_test.go` suite
  passes unmodified.
- **Mode legibility.** From any state, one glance at the composer row answers
  "will the next letter type or navigate?"
- **No inline image without consent.** With `ui.inline_images = "on_open"`
  (default), no escape sequence for graphics is emitted until `o` is pressed.

## 13. Explicit non-goals

Say no to these; they will be suggested:

- Mouse-driven everything. Click-to-select and wheel-scroll stay; nothing
  becomes mouse-only.
- Avatars, as blocks, sixel, or ASCII art. The sigil is the identity.
- Bubbles, rounded panel borders, or any decorative box.
- Syntax highlighting beyond the diff/comment coloring in §6.
- Animation. A spinner during a network wait is the entire budget.
- Emoji as UI glyphs. Emoji appear only when a human sent them (message text,
  reactions).

## 14. Visual sign-off — `fixtures/`

Cell-exact golden renderings at 80×24, 100×30, 120×40, 137×29, 200×60, plus a
CJK/emoji/RTL/combining-mark fixture and a block gallery. Every line is exactly
its stated display width, measured with the same `uniseg` rules the
implementation must use. `fixtures/README.md` covers the assertion pattern, the
plain-text substitutions for colored elements, and the three spec amendments
the fixtures forced.

These, not the HTML, are the acceptance artifact for §12's frame-integrity and
column-alignment criteria.

## 15. Files in this bundle

- `Telegram TUI.dc.html` — the design reference. Open in a browser. `j/k`
  switches buffers (buffer 4, `~ wire notes`, is the media and markdown
  showcase; buffer 2 has the image card and code block), `i` opens the
  expanded composer, `:` opens the command palette, `esc` returns to normal.
- `README.md` — this document.
- `fixtures/` — deterministic terminal goldens (§14).
