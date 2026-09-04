# Keybindings

Every key this client honours, and why it is that key.

The tables here are checked against the running app: `TestKeymapDocMatchesHelpSections`
compares them with the bindings `internal/app` actually dispatches on, in both
directions and per section. A binding added without a row fails; a row naming
a key that no longer exists fails too. It is the reason this document can be
trusted after a year of edits.

The `?` overlay shows the same set, filtered to where you are.

Motions follow vi convention: `h`/`j`/`k`/`l`, `g`/`G` jump to the ends,
`Ctrl+U`/`Ctrl+D` move a half page, `/` searches whatever you're looking at
and `n`/`N` step through the matches. In the chat list and chat view — the
two browsing panels, side by side — bare `h`/`l` are lazygit-style panel
movement rather than a motion: `l` from the chat list focuses the chat
view, `h` from the chat view focuses the chat list, and each is a no-op at
its own edge rather than wrapping around.

Typing is always entered on purpose — `i`, `Tab`, a focus key, or a
click on the composer — nothing is forwarded to the composer implicitly, so
no binding below costs you the ability to type that character.

There are no `Alt+…` and no function-key bindings (decision I-1). They only
ever reached the app on a terminal configured to report Option as a
modifier, which on macOS is the minority; the failure was silent and
undetectable, since the composed character arrives with no modifier bit at
all; and none of them was a vi idiom. Every one has a plain spelling now:
`c` for contacts, `J`/`K` for next and previous chat, `[`/`]` for the
folders, and `h`/`l`/`i`/`Esc`/`Tab` between them for panel focus. Bare
`h`/`l` do **not** cycle folders — that role is `[`/`]`, the arrows and the
digits.

`q` quits from the chat list and chat view — lazygit's "q is the way out"
everywhere it can't be mistaken for typing. It asks first if the composer
holds an unsent draft or a pending attachment; `Ctrl+Q` still quits
unconditionally from anywhere, including the composer. `q` closes no
overlay: overlays close with `Esc` (and the help card with `?` as well).

`/` is contextual in both browsing panels now: from the chat list it opens
a live filter over the visible chats (`Esc` clears it, `Enter` keeps it
applied and leaves a `/query` chip showing in the tab bar); from the chat
view it's in-chat find, as it always was. `Ctrl+G` is the panel-independent
global search from everywhere except the composer.

`Esc` steps back, one rung per press, and **never discards typed text**.
Leave vi insert; cancel a reply or edit target; unstage an attachment;
close an input line; close an overlay; leave a panel toward the chat list —
whichever of those applies, the words survive it. Cancelling an edit puts
back the draft the edit displaced. Work is only ever lost through a key that
says so: `d` behind a confirm, `q` behind a confirm when a draft exists, and
`Ctrl+Q`, which is the documented exception.

Press `?` any time outside the composer for a scrollable cheat sheet.
[`internal/app/keymap.go`](../internal/app/keymap.go) is where the keymap
lives in code, and no on-screen surface that names a key holds a literal.
The help card, its footer, the frame's hint bar and the media overlay's
strip are all generated from the resolved bindings
`app.go` dispatches on, through the registry in
[`internal/app/hints.go`](../internal/app/hints.go). A dialog is the one
surface with a nearer authority: its line is built from its own button set,
because the buttons *are* the answer keys — and the hint bar reads those
same buttons rather than guessing, so the two cannot name different
letters. None of them can name a key that does not fire. The prose table that used to head `keymap.go` is gone — it
was the copy nothing checked, and it was wrong by the time it was deleted.
[interaction-model.md](interaction-model.md) holds the reasoning now, and
the tables below hold the keys, with a safety net: a test
(`TestKeymapDocMatchesHelpSections` in `internal/app`) diffs the key
*set* documented here, and which section each key is filed under, against
what the help card advertises, and fails the build on a mismatch — so this
page can lag behind by at most one unrun `go test`, not indefinitely. A
second test walks every hint surface and refuses a hint that names a key
the card does not. Neither can catch a wrong *description* of what a key
does, and neither reads `config.go`'s doc comment or
`config.example.toml`, so drift in those two is still possible — though
`TestExampleConfigKeysMatchDefaults` holds the example's `[keys]` block to
the shipped defaults.

## Global — any panel

| Key | Action |
|-----|--------|
| `Ctrl+Q` | Quit (`keys.quit`, and this is its default) |
| `q` | Quit — chat list / chat view only (`keys.quit_browsing`); confirms first if the composer holds a draft or attachment |
| `?` | Toggle the help overlay |
| `Tab` / `Shift+Tab` | Cycle panel focus (works from the composer too) |
| `Esc` | Close overlay, else step back |
| `J` / `K` | Open the next / previous chat outright — unlike the chat list's own `j`/`k`, which move the cursor and open nothing |
| `u` | Open the next chat with unread messages: down from the cursor within the active folder, wrapping once. Says so rather than moving when nothing is unread |
| `]` / `[` | Next / previous folder — from **both** browsing panels |
| `c` | Toggle the contacts overlay |
| `Ctrl+G` | Search all chats (not while composing) |
| `:` | Command palette (not while composing) |
| `` ` `` | Toggle the context rail — pinned messages, members, shared files (not while composing; needs 118 columns) |
| `Ctrl+V` | Paste a clipboard image |

## Chat list

| Key | Action |
|-----|--------|
| `j` / `k` (or `↓` / `↑`) | Move the cursor — opens nothing, so holding one down costs no history fetches |
| `l` | Open the cursored chat and focus the chat view (`h` here is a no-op) |
| `←` / `→` | Previous / next folder tab (`[` / `]` do the same, from either browsing panel) |
| `1`–`9` | Jump to folder N (1 = All, always present) |
| click a folder tab | Switch to it |
| `g` / `Home` | First chat |
| `G` / `End` | Last chat |
| `Enter` | Open the cursored chat — the same thing `l` does |
| `i` | Open the cursored chat and focus the composer |
| `/` | Filter this list live (`Esc` clears, `Enter` keeps it applied) |
| `q` | Quit — confirms first if the composer holds a draft or attachment |
| click a chat | Select it |
| wheel | Scroll |

## Chat view

| Key | Action |
|-----|--------|
| `j` / `k` (or `↓` / `↑`) | Move the cursor to the next / previous **message** — the unit every action key acts on |
| `Ctrl+E` / `Ctrl+Y` | Scroll the buffer one line down / up, as in vi. The cursor follows the viewport |
| `g` / `Home` | Top |
| `G` / `End` | Bottom |
| `Ctrl+D` / `Ctrl+U` | Page down / up |
| `PgDn` / `PgUp` | Page down / up, keeping a line of context |
| `h` | Focus the chat list (`l` here is a no-op) |
| `/` or `Ctrl+F` | Find in this chat |
| `n` / `N` | Next / previous match |
| `1`–`9` | Count prefix for the motions, as in vi: `9k` moves nine messages back, `4`&nbsp;`Ctrl+Y` scrolls four lines. The pending count shows in the thread header, and any non-motion key discards it |
| `Esc` | Close the find input while it's open; otherwise step back to the chat list (surviving find results are not cleared first) |
| `r` / `e` / `d` | Reply / edit / delete message (`keys.reply` / `keys.edit_message` / `keys.delete_message` replace these, rather than adding to them). Telegram only lets you edit your own messages; `e` on somebody else's says so rather than doing nothing |
| `Enter` | Open attachment — a photo opens full-pane in the terminal, everything else goes to the system viewer |
| `o` | Open attachment in the system viewer, always |
| `s` | Save attachment into `storage.download_dir` under the sender's filename (see [Where files go](configuration.md#where-files-go)) |
| `space` | Play the selected voice note or audio message |
| `y` | Copy the selected message's text to the system clipboard |
| `+` | React to the selected message — a one-row picker; `enter` on the one you already left takes it off |
| `p` | Pin the selected message, or unpin it when it is already pinned. Silent: no "X pinned a message" line goes into the chat |
| `t` | Open the discussion under a channel post — jumps to the linked group at the post's own copy, where the comments hang off it |
| `m` | Mark this chat read without moving the scroll or the unread divider (`keys.mark_read`) |
| `x` | Reveal spoilers in the selected message (press again to hide them) |
| `i` | Compose a message |
| click | Focus the panel **and** move the cursor to the clicked message — a click on a day divider or the header moves nothing |
| `q` | Quit — confirms first if the composer holds a draft or attachment |

An explicit `j`/`k` **pins** the cursor: it stays on the message you put it
on, even as new messages arrive, until `G` hands it back to the tail. Scrolling
the pinned message off the screen releases it too — that is the reader letting
go of it.

`/` is contextual in both browsing panels — vi convention, "search the
buffer in front of you": from the chat view it opens in-chat find, and
from the chat list (previous table) it opens a live local filter. `/`
opens the global cross-chat search overlay everywhere else that isn't the
composer (which it never reaches). `Ctrl+G` reaches that same global
search from any panel, chat list and chat view included, without the
ambiguity `/` has there.

## Composer

Focus stays on the composer after you send — a conversation is a run of
messages, not one — so `Esc` is how you leave it.

| Key | Action |
|-----|--------|
| `Enter` | Send |
| `Ctrl+J` / `Shift+Enter` | Insert a newline (`Enter` alone sends) |
| `Esc` | Cancel reply/edit/attachment first, then leave — never taking the text with it |
| `Ctrl+T` | Attach a file by path |
| `Ctrl+V` | Paste a clipboard image |
| `Ctrl+O` | Edit the draft in `$VISUAL`/`$EDITOR` |
| `Ctrl+P` | Expand the composer to the split source/preview form, and back |

Almost nothing is claimed at app level while the composer has focus, so
neither line-editing keymap below loses a chord. The complete exception
list is now four keys: `Ctrl+Q` (quit, or whatever `keys.quit` is set to),
`Ctrl+V`, `Esc` (only when there's nothing to cancel) and
`Tab`/`Shift+Tab`. It used to be longer — the panel-focus keys, the
chat/folder navigation and contacts were all on it, because `Alt+…` and the
function keys are not characters. They are plain letters now, so they are
text here and nothing else. `keys.quit` is matched before every other check
in `Update`, focus included, which is why a bare printable is refused there
— see "Configuring keys" below. No other `Ctrl+<letter>` is claimed at app
level, so `Ctrl+A/B/D/E/F/J/K/O/T/U/W` all reach the composer.

## Composer editing modes

The composer speaks either the **emacs** (readline) or **vi** line-editing
keymap, chosen by `ui.compose_editing` in `config.toml`:

| `compose_editing` | Behavior |
|---|---|
| `"emacs"` | Readline chords, always |
| `"vi"` | Modal vi, always |
| `"auto"` (default) | Inferred from `$VISUAL`, falling back to `$EDITOR`: if the editor's command name contains "vi" (`vi`, `vim`, `nvim`, `gvim`, `view`) → vi, else emacs. No editor set → emacs. |

**Emacs mode** adds, on top of the shared table above:

| Key | Action |
|-----|--------|
| `Ctrl+A` / `Home` | Start of line |
| `Ctrl+E` / `End` | End of line |
| `Ctrl+B` / `Ctrl+F` | Back / forward one character |
| `Ctrl+U` / `Ctrl+K` | Kill to start / end of line |
| `Ctrl+W` | Kill the previous word |
| `Ctrl+D` / `Delete` | Delete the character under the cursor (also live in vi mode's insert state, but not in its normal mode) |

**Vi mode** starts in insert mode — typing a message is the common case,
and landing in normal mode would swallow the first word. That applies
*every* time the composer takes focus, not just the first: `i`, `r` and `e`
all leave you able to type, even though a vi user leaves the composer
through normal mode. Re-entering is the only thing that resets it — focus
being re-asserted on a resize, or after an overlay closes, keeps whatever
mode you were in. Normal mode uses
real vi cursor semantics: the cursor sits *on* a character, never in the
gap after it, so `$x` deletes the line's last character rather than the
line break, and `$i` inserts before it rather than after:

| Key | Action |
|-----|--------|
| `Esc` | Leave insert mode; `Esc` again cancels reply/edit/attachment, then leaves — same as emacs's Esc, one keystroke later, and neither press takes the draft |
| `i` / `a` / `A` | Insert before / after cursor, at end of line |
| `o` / `O` | Open a line below / above and insert |
| `h`/`l`/`j`/`k` | Move by character / line (normal mode) |
| `w` / `b` | Move by word (normal mode) |
| `0` / `Home` | Start of line (`0` only in normal mode) |
| `$` / `End` | End of line (`$` only in normal mode) |
| `x` | Delete a character (never the line break) |
| `D` | Delete to end of line (never the line break) |
| `dd` | Delete the whole line |

The newline chord is inert in vi's normal mode (`o`/`O` open a line there
instead) and dropped from the hint line accordingly. `Shift+Enter` and
`Ctrl+Enter` only arrive from a terminal speaking the Kitty keyboard
protocol or xterm's `modifyOtherKeys` — the legacy encoding has no way to
put a modifier on Enter at all, which is why `Ctrl+J` is the primary
newline chord: every terminal can send it. `Alt+Enter` was accepted as a
third spelling and is not any more (decision I-1) — beyond Alt being gone,
its legacy encoding (`Esc` then `CR`) is byte-for-byte what "press Esc,
then press Enter" produces, which is exactly what a vi user types to leave
insert mode and send.

`Ctrl+O` writes the draft to a temp file and suspends the program to run
`$VISUAL` (falling back to `$EDITOR`) on it. The variable is treated as a
shell command line, so flags work (`nvim -u NONE`) exactly as they do for
`git`, and a path with spaces needs the same quoting
(`EDITOR='"/path with spaces/subl" -w'`). A non-zero exit (`:cq`, a crash)
keeps your original draft untouched; a clean exit replaces it, trims the
trailing newline editors add, and drops you back into insert mode if
you're in vi mode.

## Command palette (`:`)

`:` opens a filtered list of commands from the chat list or chat view. It is
the first piece of the [TUI 2.0](tui-2.0.md) design to reach the client,
and it does not change anything that already worked.

`:` opens it (see the Global table above). Once it's up:

| Key | Action |
|-----|--------|
| `up` / `down` | Move the selection |
| `tab` | Complete the highlighted command |
| `enter` | Run the command |
| `esc` | Cancel without running |
| `ctrl+u` | Clear the query |

Every other printable key goes into the query — that's how you filter and how
you type arguments.

Matching is prefix-first, then fuzzy: `mkrd` finds `mark-read`, while an
exactly-typed name always sorts to the top so `Enter` runs what you typed.

**Navigation is the arrows, not `j`/`k`** — every printable key has to reach
the query, or commands whose names contain those letters (`keymap`,
`mark-read`) could not be typed at all.

## Attach picker (`ctrl+t`)

`ctrl+t` opens a path being typed, the directory it names, and what is in it.
It is the command palette's twin: same width, same anchor, same selection
marker, no buttons — the palette collects a command and this collects a path.

| Key | Action |
|-----|--------|
| `up` / `down` | Move the selection |
| `tab` | Complete the path to the highlighted entry |
| `enter` | Enter a directory, or attach a file and close |
| `backspace` | Delete a character; up one directory when there is none left |
| `left` | Up one directory |
| `ctrl+t` | Send an image as a photo, or as a document |
| `ctrl+u` | Clear the path |
| `esc` | Cancel without attaching |

Every other printable key extends the path, which is why navigation is the
arrows here too: a filename may contain any letter.

**It says how the file will send before you commit.** An image goes as a
photo — recompressed, shown inline — and anything else as a document, with
the original bytes. `ctrl+t` changes that for an image and says so on the
state row. The prompt this replaced always attached as a document, silently,
while `ctrl+v` attached the same image as a photo.

**Dotfiles are hidden until you type a leading dot**, the way a shell hides
them. Matching is a case-insensitive prefix, because the default macOS
filesystem is itself case-insensitive.

**Dropping a file on the terminal works** anywhere `ctrl+t` does. A terminal
delivers a drop as a paste of the path, escaped the way a shell would need it
(`/Users/a/My\ Files/x.png`, a quoted path, or a `file://` URL); the picker
unquotes all three. Dropping one with no picker open stages it directly, but
only when the paste is unambiguously a path to a file that exists — anything
else is typed into the message, because a paste that merely resembles a path
must not silently become an attachment.

`:` only opens the palette where a bare letter is not text. With the composer
focused it types a colon like any other character; the sole exception is vi
editing in normal mode, where the composer is not accepting text either, so
`:` opens the palette there too.

Commands available now:

| Command | Action |
|---|---|
| `:mark-read` | Mark the open chat read, keeping your scroll position |
| `:search <query>` | Open the cross-chat search, pre-filled |
| `:keymap` | Open the help overlay |
| `:quit` | Quit |

An unknown command or a surplus argument reports on the composer's hint line
rather than failing silently. More commands (`pin`, `mute`, `reload-config`,
`theme`, `jump`) are designed but not yet registered — each needs a service
this build doesn't have, and a palette entry that can't run would be worse
than an absent one. See [TODO.md](../TODO.md).

## Contacts (`c`)

Borrows the chat list's column and draws its grid — the same row offsets,
the same selection bar, the same filter header — because the two swap into
one region and a reader whose eye has learned where a name starts should not
have to relearn it. The header's placeholder is what names the surface.

| Key | Action |
|-----|--------|
| `j` / `k` (or `↓` / `↑`) | Move the cursor |
| `Enter` | Open a private chat with the selected contact |
| `/` | Filter the list live, over both the name and the `@username` (`Esc` clears, `Enter` keeps it applied) |
| `Esc` | Clear an applied filter; with none, close the panel |
| `c` | Close the panel — the key that opened it |

## Overlays — search, contacts, dialogs

| Key | Action |
|-----|--------|
| `Esc` | Close. **`q` closes no overlay** — it is quit and nothing else |
| `Enter` | Accept the selection — in a **dialog**, this means whichever button is currently highlighted, not "confirm" |
| `j` / `k` (or `↓` / `↑`) | Move — a dialog's buttons move with `←`/`→` instead, and not with `j`/`k`: those are the keys a reader is already holding when a confirm appears mid-scroll |

`q` used to close the help card, the media overlay and the reaction row, in
each case one keystroke before it would have quit the client — `?` `q` `q`
was an exit nobody meant to type. It closes none of them now.

A **confirm dialog** (deleting a message, quitting with an unsent draft) starts with **Cancel** highlighted, not Confirm, precisely because `Enter` fires whichever button is lit: these dialogs guard destructive or lossy actions, so a reflex `Enter` must not be the thing that performs one. The highlighted button is marked two ways — reversed color, and literal `[ Brackets ]` around its label — so it reads correctly without color.

Every button also carries an **accelerator letter**, drawn in its own label (`[ Ca(n)cel ]`, `[ For (m)e ]`) and answering outright when pressed. A two-button confirm therefore answers to `y` and `n` directly — they are safe here for the reason `j`/`k` are not: nobody is holding `y` when a confirm appears mid-scroll. The dialog renders its own one-line hint, built from the buttons it actually has (`n/y: answer · ←/→: choose · enter: accept · esc: cancel`, or just `enter or esc: dismiss` for a single-button alert), so the keymap is visible in the moment and cannot describe a button set the dialog no longer offers.

**Deleting a message** asks *Delete this message?* and offers Telegram's real choice: `Cancel` · `For me` · `For everyone`, answering to `n` / `m` / `e`. It used to ask "Are you sure?" and always delete for everyone — the reach of a delete is a decision, and that dialog was making it silently. A server refusal of "for everyone" (the message is too old, or the chat does not permit it) is reported in the notice row.

## Help overlay (`?`)

A lazygit-style scrollable cheat sheet built from the same resolved
bindings as the tables above, so a rebound key is described correctly
instead of drifting out of sync with what the card shows.

| Key | Action |
|-----|--------|
| `?` / `Esc` | Close (`q` does not — see above) |
| `j` / `k` (or `↓` / `↑`) | Scroll |
| `PgUp` / `PgDn` | Page |
| `g` / `G` (or Home / End) | Top / bottom |

The card's own footer line only spells out `esc / ? to close · j k to
scroll` — `PgUp`/`PgDn` and `g`/`G` work too, just not named there; this
table is the complete list.

The frame's hint bar is the abbreviation that points at this card. It is
keyed by **surface** — the panel or overlay whose keymap is live right now —
and every set it can show is the "Hints" table in
[interaction-model.md](interaction-model.md), built from the resolved
bindings. The media overlay's strip is handed its content from the same
registry, so a rebound key shows correctly everywhere and a key that is not
bound shows nowhere.

It is the **only** hint row on screen. The chat list drew a second one along
the foot of its own column and it is gone: with the bar keyed by the live
surface, that row repeated the bar whenever the list had focus and described
the chat list's keys whenever it did not — `l open` and `/ filter` while the
chat view had the keyboard, where they mean a no-op and in-chat find. The
one thing it said that the bar could not, the way out of a filter, is in the
chat list's own set now (`esc clear · enter keep` while typing one, `esc
clear filter` once it is applied).

A dialog's own one-line hint is the exception, and deliberately so: it is
built from that dialog's button set, which is the only thing that knows
whether the answers are `n`/`y` or `n`/`m`/`e`. The hint bar reads the same
buttons for its `answer` row, so the two are renderings of one source
rather than two copies of it — the bar used to say `y/n` for every dialog,
which advertised an inert `y` over the delete choice.

While the help overlay is open it owns the keyboard entirely: everything
except its own close/scroll keys is swallowed rather than passed through,
since the panels behind it aren't visible.

If the Telegram client dies for good (see "Troubleshooting &
Diagnostics" below), the UI is replaced by an error panel and every
binding above except quit becomes inert.

## Configuring keys

`[keys]` in `config.toml` overrides bindings. Modifiers and key *names* are
case-insensitive and aliased — `"Escape"`, `"ESC"` and `"escape"` are the
same key, as are `"Option+1"` and `"alt+1"` — but a **lone printable letter
is case-sensitive**, because on an unmodified key the case *is* the binding.
`next_chat = "J"` and the chat list's own `j` are two different keys: a
`J` press reports itself as `shift+j`, and matching it as `j` would turn
every plain `j` into chat navigation. Write the modifier out
(`"alt+shift+l"`) if you want a shifted letter with one.

Every field follows **one rule** (decision I-13): a value *replaces* the
default, and a value that collides with anything already bound — another
field, or a key a panel owns outright — is *refused*, the default is kept,
and the refusal is printed on stderr at startup. There used to be three
rules (replace, add-alongside, and accepted-but-inert), which needed a page
to tell apart and let `forward` sit in the shipped example file bound to
nothing.

| Field | Default |
|-------|---------|
| `quit` | `ctrl+q` — a bare printable is refused |
| `quit_browsing` | `q` |
| `search` | `/` |
| `global_search` | `ctrl+g` |
| `contacts` | `c` |
| `compose` | `i` |
| `help` | `?` |
| `next_chat` / `prev_chat` | `J` / `K` |
| `next_unread` | `u` |
| `next_folder` / `prev_folder` | `]` / `[` |
| `reply` / `edit_message` / `delete_message` | `r` / `e` / `d` |
| `mark_read` | `m` |

**Removed**, and reported by `-migrate-config` as removed the way
`ui.chat_list_width` was: `focus_chat_list`, `focus_chat_view`,
`focus_composer`, `contacts_alt`, `forward`, `scroll_up`, `scroll_down`,
`page_up`, `page_down`. The first four went with the Alt and function keys;
`forward` was never dispatched; the motions are vi's and are not
configurable. **Old config files still load** — the decoder ignores fields
the schema no longer has — so nothing breaks on upgrade, and the migration
report says what was dropped.

"Already claimed" covers keys the chat view hardcodes for itself (`j`/`k`,
`g`/`G`, `Ctrl+E`/`Ctrl+Y`, `Ctrl+U`/`Ctrl+D`, `n`/`N`, `Ctrl+F`,
`Enter`/`o`/`s`) as well as the app-level surface the chat-view fields
cannot see on their own — `h`, `l`, `Tab`, `Ctrl+V`, `Ctrl+Q`, `Esc`, `:`,
`` ` ``, and whatever the app-dispatched fields above resolve to. That is
what stops e.g. `reply = "h"` from quietly stealing panel movement.

Resolution runs in two passes per tier, so an explicit setting always
outranks a default regardless of field order, and the app's tier is
resolved before the chat view's — because app-level dispatch runs before
the focused panel sees the key, so a chat-view binding pointed at an
app-level one is not ambiguous, it is dead. See the `Keys` and
`SetReservedKeys` doc comments on `internal/ui/components/chatview.Model`.

A refusal is not left invisible. It is printed on stderr at startup, and
the `?` help card shows an action left with no key at all as `(unbound)`
instead of a blank or a wrong key: seeing that on the card means some other
binding already holds the letter you wanted, so free it up (or pick a
different key) to restore the action.

Wired bindings are matched before the focused panel sees the key, so a
binding here shadows that key in the chat list and chat view. Most of them
do *not* reach the composer — typing there is only ever entered
deliberately (see "Composer" above), and app-level dispatch claims almost
nothing while it has focus. `keys.quit_browsing` and `keys.help` are
composer-safe: a bare letter there really is inert while composing.
Every app-dispatched field above is now a bare key or a chord that the
browsing panels own, and none of them reaches a focused composer.

`keys.quit` is the one field where a bare printable is not a judgement
call: it is matched before every other check, focus included, so
`quit = "x"` would mean pressing `x` while writing a message quit the app
instead of typing an `x`. **That configuration is now refused**: the
binding falls back to `Ctrl+Q`, and the client says so on stderr at
startup rather than only under `-migrate-config`. `space` is refused for
the same reason, its longer name notwithstanding — it types a character
too.

> [`config.example.toml`](../config.example.toml)'s `[keys]` block lists every
> field in the table above at its built-in default; a test
> (`TestExampleConfigKeysMatchDefaults` in `internal/config`) fails the
> build if the two ever drift apart. `internal/config/config.go`'s
> `defaultConfig()` remains the ultimate source of truth. Have a config of
> your own that predates a recent field? Run `-migrate-config` (below)
> rather than hand-editing it — it fills in exactly the gaps and tells you
> what it added.
