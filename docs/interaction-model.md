# Interaction model

The rules the keyboard follows, stated once. This document is the authority
on *what a key means and why*; [keys.md](keys.md) is the table of *which*
key, checked against the running app by test. Where this document and an
older record disagree — the keymap prose in `internal/app/keymap.go`, the
"Mode integration" section and decision 3 of [tui-2.0.md](tui-2.0.md),
[`KEYMAP-REVIEW.md`](../KEYMAP-REVIEW.md) — this one wins. It came out of
the 2026-09-04 interaction review, whose work order is
[`INTERACTION-REVIEW.md`](../INTERACTION-REVIEW.md); the numbered
decisions below are cross-referenced from there.

The review's verdict, for the record: the paradigm is right. Modal, three
panels, typing entered on purpose, bare letters safe wherever they are not
text. Nothing here reopens that. What it does is remove the places where
two rules coexisted for one thing, and the places where a surface described
a keymap it did not have.

## Vocabulary

- **Panel** — a region that can hold keyboard focus: the chat list, the
  chat view, the composer. The rail is a reading surface, not a panel.
- **Browsing panels** — the chat list and the chat view: the two places a
  bare letter is a command, never text.
- **Overlay** — something drawn over the frame that owns the keyboard while
  it is up: the palette, the attach picker, search, contacts, a dialog, the
  help card, the media overlay, the reaction row.
- **Surface** — a panel or an overlay: the thing whose keymap is live right
  now. Every hint the app draws is keyed by surface (decision I-6).
- **Mode** — the badge's answer to one question: does the next printable
  key type or act? Derived, never stored (decision 3 of TUI 2.0 stands).
- **Cursor** — in the chat list, the highlighted row; in the chat view, the
  message the action keys act on. Distinct from the **open chat**, which is
  the one whose history the chat view shows.

## The six rules

1. **Typing is entered on purpose.** `i`, a click on the composer, `Tab`
   onto it, or an action that needs text (`r`, `e`). Nothing is forwarded to
   the composer implicitly. This is what makes every bare-letter binding in
   the browsing panels free. Unchanged.

2. **One spelling per action, and no modifier that a terminal might eat.**
   A binding is a bare key, a `ctrl` chord, `Tab`, `Enter`, `Esc`, an arrow
   or a page key. **`Alt` and the function keys are gone** (decision I-1).
   They were never reliable, they are not vi, and every one had or now has a
   plain spelling. Where two spellings survive, one is vi's and one is the
   arrow-key form of the same motion, and that is the whole allowance.

3. **`Esc` steps back and never discards.** One step per press: leave vi
   insert, cancel a reply or edit target, unstage an attachment, close an
   input line, close an overlay, leave a panel toward the chat list. Typed
   text survives every rung (decision I-3). Work is only ever lost through a
   key that says so — `d` behind a confirm, `q` behind a confirm when a
   draft exists, `ctrl+q` which is the documented exception.

4. **A motion moves the cursor; the viewport follows.** In the chat view
   `j`/`k` step between messages, which is what every action key targets;
   `ctrl+e`/`ctrl+y` scroll the buffer by lines, as in vi (decision I-4).
   In the chat list `j`/`k` move the cursor and open nothing; `l`, `Enter`
   and `i` act on the cursored chat (decision I-2).

5. **Every hint surface reads from one table.** The help card, the hint
   bar, a dialog's own hint line and the palette's key column are all built
   from the same resolved bindings, per surface, and a test walks every
   surface (decision I-6). A hint that names an inert key
   is a defect, not a nit — it is how `u unread` sat in the chat list footer
   with nothing bound to `u`.

6. **`q` is quit and nothing else.** It quits from the browsing panels,
   confirming when a draft or attachment exists; it closes no overlay
   (decision I-8). Overlays close with `Esc`. `ctrl+q` quits from anywhere
   without asking, because a modal must never trap someone. `:quit` follows
   the `q` rule, not the `ctrl+q` one (decision I-5).

## The keymap, by surface

The complete set after the work order lands. Rows marked *new* or *moved*
are the changes; everything else is the current binding restated so that
the table can be read on its own.

### Everywhere the composer is not focused

| Key | Action | |
|---|---|---|
| `ctrl+q` | Quit, no confirm | |
| `q` | Quit, confirm on a draft (browsing panels only) | |
| `?` | Help card | |
| `:` | Command palette | also from a vi composer in its command state |
| `` ` `` | Toggle the context rail | |
| `ctrl+g` | Global search | |
| `ctrl+v` | Paste a clipboard image | also from the composer |
| `Tab` / `Shift+Tab` | Cycle panel focus, wrapping | the one cycle; `h`/`l` are edges — see I-9 |
| `Esc` | Step back | rule 3 |
| `c` | Contacts overlay | *new*, replaces `alt+c`/`F4` |
| `J` / `K` | Open the next / previous chat | *new*, replaces `alt+j`/`alt+k` |
| `u` | Open the next chat with unread messages | *new* |
| `[` / `]` | Previous / next folder | *moved* to both browsing panels, replaces `alt+h`/`alt+l` |

Gone: `alt+1/2/3`, `F1`–`F3` (panel focus — `h`, `l`, `i`, `Esc` and `Tab`
cover it), `alt+j/k`, `alt+h/l`, `alt+c`, `F4`, and `alt+enter` as a
newline chord.

### Chat list

| Key | Action | |
|---|---|---|
| `j` / `k`, `↓` / `↑` | Move the cursor | opens nothing |
| `g` / `G`, `Home` / `End` | First / last chat | |
| `Enter`, `l` | Open the cursored chat and focus the chat view | *changed*: `l` opens, see I-2 |
| `i` | Open the cursored chat and focus the composer | *changed*, see I-2 |
| `h` | No-op (left edge) | |
| `←` / `→` | Previous / next folder | arrow form of `[` / `]` |
| `1`–`9` | Jump to folder N | documented departure from lazygit, stands |
| `/` | Filter this list live | `Esc` clears, `Enter` keeps |
| `u`, `J`, `K` | As above | |
| click, wheel | Select and open; scroll | |

### Chat view

| Key | Action | |
|---|---|---|
| `j` / `k`, `↓` / `↑` | Cursor to the next / previous message | *changed*, see I-4; count prefix applies |
| `ctrl+e` / `ctrl+y` | Scroll the buffer one line down / up | *new*, see I-4; count applies |
| `ctrl+d` / `ctrl+u` | Half page | cursor follows the viewport |
| `PgDn` / `PgUp` | Page, keeping a line of context | cursor follows |
| `g` / `G`, `Home` / `End` | Top / bottom; `G` also hands the cursor back to the tail rule | |
| `1`–`9` | Count prefix for the motions | |
| `h` | Focus the chat list | `l` is the right edge, a no-op |
| `/`, `ctrl+f` | Find in this chat; `n` / `N` step through hits | |
| `r` / `e` / `d` | Reply / edit / delete the cursored message | `d` confirms, and says for whom — I-7 |
| `y` | Copy the cursored message's text | |
| `+` | React | |
| `p` | Pin / unpin | |
| `f` | Forward the cursored message to another chat | *new*, see I-13 — the one removed field that came back |
| `t` | Open the discussion under a channel post | |
| `m` | Mark this chat read without moving | *moved* from `M`, see I-10 |
| `x` | Reveal / hide spoilers | |
| `Enter` | Open the attachment (photo in the terminal, else externally) | documented departure, stands |
| `o` | Open the attachment externally | |
| `s` | Save the attachment | |
| `space` | Play a voice note | |
| `i` | Focus the composer | |
| `Esc` | Close the find input, else focus the chat list | |
| click | Focus the panel **and move the cursor to the clicked message** | *changed*, see I-11 |

Retired: `}` / `{`. They were the message-wise motion while `j`/`k`
scrolled lines; with `j`/`k` message-wise they would be a second spelling.
Freed rather than reassigned.

### Composer

The shared chords, then the editing keymap. `Esc` is rule 3: in vi insert
it leaves insert; otherwise it cancels a reply or edit target, or unstages
an attachment, or leaves the composer — one thing per press, text kept.

| Key | Action |
|---|---|
| `Enter` | Send |
| `ctrl+j`, `Shift+Enter` | Newline (`alt+enter` is no longer accepted) |
| `ctrl+t` | Attach a file |
| `ctrl+v` | Paste a clipboard image |
| `ctrl+o` | Edit the draft in `$VISUAL`/`$EDITOR` |
| `ctrl+p` | Expand / collapse the split preview |
| `Tab` / `Shift+Tab` | Leave the composer with reply, edit and attachment intact |

Emacs and vi line editing are unchanged; see [keys.md](keys.md).

### Overlays

| Surface | Keys |
|---|---|
| Palette | `↑`/`↓` move, `Tab` complete, `Enter` run, `ctrl+u` clear, `Esc` cancel. Printables type |
| Attach picker | `↑`/`↓`, `Tab` complete, `Enter` enter / attach, `←` and `Backspace` up, `ctrl+t` photo/document, `ctrl+u` clear, `Esc` cancel. Printables type |
| Search | printables type, `Tab` scope, `Enter` search / open, `j`/`k` in the results, `Esc` close |
| Contacts | `j`/`k`, `Enter` open chat, `/` filter the list, `c` or `Esc` close. `Esc` clears an applied filter before it closes the panel |
| Confirm dialog | `←`/`→` choose, `Enter` accept the highlighted button, **`y` / `n` answer directly** (*new*, I-7), `Esc` cancel. Any button may carry its own accelerator letter, shown in its label |
| Help | `j`/`k`, `PgUp`/`PgDn`, `g`/`G` scroll; `Esc` or `?` close. **`q` no longer closes it** (I-8) |
| Media | `Esc` close (**not `q`**, I-8), `s` save, `o` open externally |
| Reaction row | `←`/`→` or `h`/`l`, `1`–`9` and `0` pick the first ten, `Enter` pick, `Esc` cancel |

## Modes and the badge

Four labels, one more than TUI 2.0 shipped (decision I-12):

| Badge | Colour | When | Next printable key… |
|---|---|---|---|
| `NORMAL` | cyan | a browsing panel, a navigating overlay | acts on a chat or message |
| `INSERT` | green | the composer, editor accepting text; a text overlay | is typed |
| `VI` | mauve | the composer, vi editing, in its command state | runs a vi command on the draft |
| `COMMAND` | amber | the palette | is typed into the query |

`VI` exists because the composer's command state shared a badge with the
browsing panels while sharing none of their keys: `q`, `r`, `y`, `e`, `?`
all inert there, `i` and `h`/`l` meaning something else. A badge whose
job is "what does the next key do" cannot honestly say `NORMAL` for two
keymaps that agree on nothing. The label is short because the badge column
is seven cells; the colour is a second channel, not the only one.

`:` opens the palette from `NORMAL` **and** from `VI` — that is vim's own
muscle memory and stays.

## Hints

One registry, per surface, ordered by priority, built from resolved keys.
The hint bar keeps the longest prefix that fits, so order is what survives
on a narrow terminal. The sets:

| Surface | Hints, in order |
|---|---|
| Chat list | `j/k move · l open · / filter · [ ] folder · u unread · i compose · q quit · ? keymap` |
| Chat view | `j/k message · r reply · y yank · e edit · / find · h chats · i compose · q quit · ? keymap` |
| Composer INSERT | `enter send · esc leave · ctrl+j newline · ctrl+t attach · ctrl+o editor` |
| Composer VI | `i insert · esc leave · o open line · dd delete line · : command` |
| Palette | `enter run · tab complete · esc cancel` |
| Attach picker | `enter attach · tab complete · esc cancel` |
| Contacts | `j/k move · enter open · / filter · c close` — led by `esc clear · enter keep` while a query is being typed, and by `esc clear filter` once one is applied |
| Search | `enter search · tab scope · esc close` |
| Dialog | `y/n answer · ←/→ choose · enter accept · esc cancel` |
| Help | `esc close · j/k scroll` |

There is **one** hint row on screen: the bar. The chat list used to draw a
second one along the foot of its own column; it is gone (see the amendment
to I-6 below). The chat list's row leads with the way out of a filter while
one is applied — `esc clear · enter keep` while the input is open, `esc
clear filter` once it is applied and closed — because that is state the
panel owns and a narrowed list with nothing saying how to widen it reads as
chats going missing.

The reaction row and the media overlay draw their own strip because they own
their row or the whole screen; those strings are still read from the
registry. A dialog's own line is built from its button set, which is the
nearer authority on which letters answer — and the bar reads the same
buttons, so the two cannot name different letters.

## Configuration

`[keys]` shrinks to one semantic: **a value replaces the default, and a
value that collides with anything already bound is refused with a warning
at startup**, not only under `-migrate-config` (decision I-13). The help
card shows a refused action as `(unbound)`, as it does today.

| Field | Default |
|---|---|
| `quit` | `ctrl+q` — a bare printable is refused |
| `quit_browsing` | `q` |
| `help` | `?` |
| `search` | `/` |
| `global_search` | `ctrl+g` |
| `contacts` | `c` |
| `compose` | `i` |
| `next_chat` / `prev_chat` | `J` / `K` |
| `next_unread` | `u` |
| `next_folder` / `prev_folder` | `]` / `[` |
| `reply` / `edit_message` / `delete_message` | `r` / `e` / `d` |
| `mark_read` | `m` |

Removed, and reported by `-migrate-config` as removed the way
`ui.chat_list_width` was: `focus_chat_list`, `focus_chat_view`,
`focus_composer`, `contacts_alt`, `forward`, `scroll_up`, `scroll_down`,
`page_up`, `page_down`. The motions are not configurable; they are vi's.
Old config files still load — the decoder ignores unknown fields — so
nothing breaks on upgrade, and the report says what was dropped.

The macOS Option-key section of keys.md goes with the Alt bindings.

## Decisions

Numbered I-n to keep them apart from TUI 2.0's 1–13.

- **I-1 — No Alt, no function keys.** They work only where the terminal is
  configured to report them, which on macOS is the minority; the failure is
  silent and undetectable; and none of them is a vi idiom. Every one had a
  plain spelling or gets one above. The alt-free fallback machinery, the
  macOS notes and `contacts_alt` go with them. Nobody depends on them.

- **I-2 — In the chat list, `l` and `Enter` mean the same thing, and `i`
  opens before it composes.** The cursor was decoupled from the open chat
  so that `j` would not load a history per press, which stands. What did
  not stand was `l` and `i` acting on the *open* chat while the cursor sat
  elsewhere: `jjjl` landed in the wrong conversation. Now every key that
  leaves the list rightward takes the cursored chat with it.

- **I-3 — `Esc` never discards text.** Cancelling a reply or edit target
  keeps what was typed; cancelling an edit restores the draft the edit
  displaced (the composer parks it on `e` and un-parks it on cancel or
  send). Unstaging an attachment stays on `Esc` because the chip says so
  and re-attaching is one chord. The old behaviour — `Reset` on cancel,
  wiping the textarea — was an artefact of `Reset` being the only cancel
  primitive, not a choice. The ladder's *shape* (decision 3 of TUI 2.0) is
  unchanged: vi still costs one more `Esc` than emacs.

- **I-4 — `j`/`k` step messages; `ctrl+e`/`ctrl+y` scroll lines.** Nine
  action keys target the cursored message and the primary motion did not
  move it. vi's own split — `j`/`k` for the unit you act on, `ctrl+e`/`y`
  for the viewport — resolves it without inventing anything. The tail rule
  from divergence 23 stands: an explicit `j`/`k` pins the cursor, `G`
  hands it back to the newest message. `}`/`{` retire.

- **I-5 — One quit policy for `q` and `:quit`; `ctrl+q` is the exception.**
  `:quit` ran unconditionally and was reachable from a vi composer holding
  a draft. It now confirms on a draft or attachment exactly as `q` does.
  `ctrl+q` keeps its no-questions behaviour on purpose: it is the way out
  of any state, including a broken one.

- **I-6 — Hints are keyed by surface, from one table.** The hint bar was
  keyed by mode, so the chat-view set showed in the chat list, under
  contacts, under a confirm dialog and in a vi composer — four places
  where it named inert keys. A per-focus generator (`helpLine`) existed,
  was tested and was never rendered. One registry, every surface, one
  drift test.

  *Amendment, recorded on implementation:* **the chat list's footer row is
  removed rather than derived.** This decision originally had it draw its
  first three or four hints from the same row of the table, and that is
  what shipped — at which point the frame showed two hint rows stacked, and
  the second one was wrong as often as it was redundant. With the bar keyed
  by the live surface there is no state in which a panel-local hint row is
  right: while the chat list has focus the bar already shows that set, so
  the footer repeated its first three hints verbatim; while any other panel
  has focus the footer went on advertising `l open` and `/ filter`, which
  from the chat view mean a no-op and in-chat find. A hint naming keys that
  are not live on the surface holding the keyboard is precisely what this
  decision exists to remove, so deriving the row correctly was not enough —
  it had to go. Its one irreplaceable string, the way out of a filter, moved
  into the chat list's own set in the registry. The row went back to the
  list.

- **I-7 — A confirm says what it confirms, and answers to `y`/`n`.** The
  delete dialog said "Are you sure?" and deleted for everyone. It now names
  the consequence and offers Telegram's real choice: *Cancel · For me · For
  everyone*, Cancel highlighted, each button with an accelerator. `y`/`n`
  answer a two-button confirm directly; they are not held keys, so the
  reason `j`/`k` were removed from dialogs does not apply.

- **I-8 — `q` closes no overlay.** `q` closed the help card and, one
  keystroke later, quit the app; the same double-press exits from the media
  overlay. Overlays close on `Esc` (and the help card on `?`). `q` has one
  meaning.

- **I-9 — `Tab` cycles and wraps; `h`/`l` are edges.** Two lateral
  policies were found; they are kept as two *kinds* rather than aligned.
  `h`/`l` are directions and stop at the edge like the `Esc` ladder. `Tab`
  is a cycle, and its job is specific: it is the only way out of the
  composer that leaves reply, edit and attachment context untouched. A
  non-wrapping `Tab` would be inert from the composer, which is the one
  place it earns its keep.

- **I-10 — `m` marks read.** `M` was an orphan capital; `m` was free and
  is the vi mnemonic for "mark". `M` is freed, not reassigned.

- **I-11 — A click in the thread moves the cursor.** Mouse users had no
  way to choose a target for `r`, `y`, `+`. Clicking a message now sets the
  cursor to it as well as focusing the panel.

- **I-12 — A fourth badge, `VI`.** See "Modes and the badge".

  *Amendment, recorded on implementation:* the badge column is padded to
  **six** cells, not seven. `NORMAL` and `INSERT` are six and `COMMAND` is
  seven, so six is the width the column already had wherever it can change
  under the reader's eyes — a vi user pressing `Esc` sees `VI` where
  `INSERT` was, and the prompt after it must not move. Padding to seven
  would fix the column at `COMMAND`'s width and shift every existing frame
  by one cell, which is a geometry change, and the golden fixtures exist to
  refuse those (TUI 2.0 decision 11).

- **I-13 — `[keys]` has one semantic: replace, refuse collisions, warn at
  startup.** Three semantics (replace / add alongside / accepted-but-inert)
  needed a page to explain and let `forward` sit in the example file bound
  to nothing. `quit` additionally refuses a bare printable, closing the
  documented foot-gun where `quit = "x"` quit from the composer.

  `forward` is back, under this same rule and with a dispatcher behind it
  (issue #39). That is not a reversal: what I-13 removed was an *inert*
  field, and the objection was that it was accepted, saved and never
  consulted. A config that still carried `forward = "f"` from before the
  cut was told the key had been removed; on the next migration it is told
  it has been added, which is more churn than either message alone
  deserves — and the honest sequence, because both were true when they
  were said.

- **I-14 — Accepted as they are.** Digits jump folders in the chat list and
  count motions in the chat view: adjacent panels, different meanings,
  documented, kept because both are the right vi-shaped answer in their
  panel. `Enter` opens an attachment in the chat view: kept, there is
  nothing to drill into. The reaction row has twelve entries and ten
  digits: the arrows reach the last two, and trimming Telegram's default
  set to fit a keyboard would be the wrong trade.

- **I-15 — One prose copy fewer.** The keymap prose at the top of
  `internal/app/keymap.go` is deleted. It was the copy nothing checked, and
  it was already wrong (it listed `c` for compose after `c` was removed).
  The help card and keys.md remain, with the drift test between them; this
  document holds the reasoning.
