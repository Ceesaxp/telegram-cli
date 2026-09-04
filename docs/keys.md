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

Every `Alt+…` binding below has an alt-free alternative *except* `Alt+J`/
`Alt+K` (next/prev chat — use the chat list's own `j`/`k` while it's
focused) and folder cycling from any panel other than the chat list (the
alt-free `[`/`]`, arrows and digits only work while the chat list has
focus). Bare `h`/`l` do **not** cycle folders any more — that role belongs
entirely to `[`/`]`, the arrows, the digits and `Alt+H`/`Alt+L`, since bare
`h`/`l` now move between panels (above). See "macOS: Alt bindings and the
Option key" below if `Alt+…` bindings don't seem to work at all.

`q` quits from the chat list and chat view — lazygit's "q is the way out"
everywhere it can't be mistaken for typing. It asks first if the composer
holds an unsent draft or a pending attachment; `Ctrl+Q` still quits
unconditionally from anywhere, including the composer, and `q` still closes
the help overlay as before.

`/` is contextual in both browsing panels now: from the chat list it opens
a live filter over the visible chats (`Esc` clears it, `Enter` keeps it
applied and leaves a `/query` chip showing in the tab bar); from the chat
view it's in-chat find, as it always was. `Ctrl+G` is the panel-independent
global search from everywhere except the composer.

Press `?` any time outside the composer for a scrollable cheat sheet.
[`internal/app/keymap.go`](../internal/app/keymap.go) is where the keymap
lives in code, but it is not all one thing: four on-screen surfaces — the
help card itself, its footer line, the status bar's hint strip, and the
one-line hint at the bottom of the screen that changes with focus — are
genuinely *generated*, built at runtime from the same resolved bindings
`app.go` dispatches on, so none of them can name a key that does not
actually fire. The hand-written prose table at the top of that same file,
`internal/config/config.go`'s `KeyConfig` doc comment, and the tables
below are not generated — they are maintained by hand, in three separate
places, and can each say something the code no longer does. The tables
below are the one exception with a safety net: a test
(`TestKeymapDocMatchesHelpSections` in `internal/app`) diffs the key
*set* documented here, and which section each key is filed under, against
what the help card advertises, and fails the build on a mismatch — so this
page can lag behind by at most one unrun `go test`, not indefinitely. It
cannot catch a wrong *description* of what a key does, and it does not
read `keymap.go`'s prose table, `config.go`'s doc comment, or
`config.example.toml` at all, so drift in any of those is still possible.

## Global — any panel

| Key | Action |
|-----|--------|
| `Ctrl+Q` | Quit (`keys.quit`, and this is its default) |
| `q` | Quit — chat list / chat view only (`keys.quit_browsing`); confirms first if the composer holds a draft or attachment |
| `?` | Toggle the help overlay |
| `Tab` / `Shift+Tab` | Cycle panel focus (works from the composer too) |
| `F1` / `Alt+1` | Focus chat list |
| `F2` / `Alt+2` | Focus chat view |
| `F3` / `Alt+3` | Focus composer |
| `Esc` | Close overlay, else step back |
| `Alt+J` / `Alt+K` | Next / previous chat |
| `Alt+L` / `Alt+H` | Next / previous folder |
| `Alt+C` / `F4` | Toggle contacts overlay |
| `Ctrl+G` | Search all chats (not while composing) |
| `:` | Command palette (not while composing) |
| `` ` `` | Toggle the context rail — pinned messages, members, shared files (not while composing; needs 118 columns) |
| `Ctrl+V` | Paste a clipboard image |

## Chat list

| Key | Action |
|-----|--------|
| `j` / `k` (or `↓` / `↑`) | Next / previous chat |
| `l` | Focus the chat view (`h` here is a no-op) |
| `←` / `→` | Previous / next folder tab |
| `[` / `]` | Previous / next folder tab (lazygit spelling) |
| `1`–`9` | Jump to folder N (1 = All, always present) |
| click a folder tab | Switch to it |
| `g` / `Home` | First chat |
| `G` / `End` | Last chat |
| `Enter` | Open the selected chat |
| `i` | Compose a message |
| `/` | Filter this list live (`Esc` clears, `Enter` keeps it applied) |
| `q` | Quit — confirms first if the composer holds a draft or attachment |
| click a chat | Select it |
| wheel | Scroll |

## Chat view

| Key | Action |
|-----|--------|
| `j` / `k` (or `↓` / `↑`) | Scroll down / up (plus `keys.scroll_down` / `keys.scroll_up`, if configured to something new) |
| `g` / `Home` | Top |
| `G` / `End` | Bottom |
| `Ctrl+D` / `Ctrl+U` | Page down / up |
| `PgDn` / `PgUp` | Page down / up, keeping a line of context (plus `keys.page_down` / `keys.page_up`, if configured to something new) |
| `h` | Focus the chat list (`l` here is a no-op) |
| `/` or `Ctrl+F` | Find in this chat |
| `n` / `N` | Next / previous match |
| `}` / `{` | Move the cursor to the next / previous message. `j`/`k` scroll the buffer by lines, vi-style; these move between messages, and `G` hands the cursor back so it follows new arrivals again |
| `1`–`9` | Count prefix for the motions, as in vi: `9{` moves nine messages back, `4k` scrolls four times. The pending count shows in the thread header, and any non-motion key discards it |
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
| `M` | Mark this chat read without moving the scroll or the unread divider |
| `x` | Reveal spoilers in the selected message (press again to hide them) |
| `i` | Compose a message |
| `q` | Quit — confirms first if the composer holds a draft or attachment |

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
| `Esc` | Cancel reply/edit/attachment first, then leave |
| `Ctrl+T` | Attach a file by path |
| `Ctrl+V` | Paste a clipboard image |
| `Ctrl+O` | Edit the draft in `$VISUAL`/`$EDITOR` |
| `Ctrl+P` | Expand the composer to the split source/preview form, and back |

Almost nothing else is claimed at app level while the composer has focus,
so neither line-editing keymap below loses a chord. The complete exception
list: `Ctrl+Q` (quit, or whatever `keys.quit` is set to),
`Ctrl+V`, `Esc` (only when there's nothing to cancel), `Tab`/`Shift+Tab`,
the panel-focus keys (`Alt+1/2/3`, `F1`-`F3`), `Alt+J`/`K`/`H`/`L`
(chat/folder navigation), and `Alt+C`/`F4` (contacts) — every one of the
*hardcoded* spellings there is a modifier or function key no line-editing
keymap binds. `keys.quit` is matched before every other check in `Update`,
focus included, which is why a bare printable is refused there — see
"Configuring keys" below. No other `Ctrl+<letter>` is claimed at app
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
| `Esc` | Leave insert mode; `Esc` again cancels reply/edit/attachment, then leaves — same as emacs's Esc, one keystroke later |
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
newline chord: every terminal can send it. `Alt+Enter` is also accepted,
but deliberately not primary: its legacy encoding (`Esc` then `CR`) is
byte-for-byte what "press Esc, then press Enter" produces — exactly what a
vi user types constantly to leave insert mode and send.

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

## Overlays — search, contacts, dialogs

| Key | Action |
|-----|--------|
| `Esc` | Close |
| `Enter` | Accept the selection — in a **dialog**, this means whichever button is currently highlighted, not "confirm" |
| `j` / `k` (or `↓` / `↑`) | Move — a dialog's buttons also move with `Tab`/`Left`/`Right`, and (outside the attach-file prompt, where `j`/`k` are typed as path text instead) with `j`/`k` too |

A **confirm dialog** (deleting a message, quitting with an unsent draft) starts with **Cancel** highlighted, not Confirm, precisely because `Enter` fires whichever button is lit: these dialogs guard destructive or lossy actions, so a reflex `Enter` must not be the thing that performs one. The highlighted button is marked two ways — reversed color, and literal `[ Brackets ]` around its label — so it reads correctly without color.

Every button also carries an **accelerator letter**, drawn in its own label (`[ Ca(n)cel ]`, `[ For (m)e ]`) and answering outright when pressed. A two-button confirm therefore answers to `y` and `n` directly — they are safe here for the reason `j`/`k` are not: nobody is holding `y` when a confirm appears mid-scroll. The dialog renders its own one-line hint, built from the buttons it actually has (`n/y: answer · ←/→: choose · enter: accept · esc: cancel`, or just `enter or esc: dismiss` for a single-button alert), so the keymap is visible in the moment and cannot describe a button set the dialog no longer offers.

**Deleting a message** asks *Delete this message?* and offers Telegram's real choice: `Cancel` · `For me` · `For everyone`, answering to `n` / `m` / `e`. It used to ask "Are you sure?" and always delete for everyone — the reach of a delete is a decision, and that dialog was making it silently. A server refusal of "for everyone" (the message is too old, or the chat does not permit it) is reported in the notice row.

## Help overlay (`?`)

A lazygit-style scrollable cheat sheet built from the same resolved
bindings as the tables above, so a rebound key is described correctly
instead of drifting out of sync with what the card shows.

| Key | Action |
|-----|--------|
| `?` / `Esc` / `q` | Close |
| `j` / `k` (or `↓` / `↑`) | Scroll |
| `PgUp` / `PgDn` | Page |
| `g` / `G` (or Home / End) | Top / bottom |

The card's own footer line only spells out `esc / ? / q to close · j k to
scroll` — `PgUp`/`PgDn` and `g`/`G` work too, just not named there; this
table is the complete list. The status bar's one-line hint strip is even
more abbreviated — it has no room to be contextual about `/`, and names
only the shortest path back to moving: `?`, the panels, the folder tabs,
find, and the way out. It leads with `?:Help` precisely because it is a
pointer at the full picture rather than the full picture itself.

While the help overlay is open it owns the keyboard entirely: everything
except its own close/scroll keys is swallowed rather than passed through,
since the panels behind it aren't visible.

If the Telegram client dies for good (see "Troubleshooting &
Diagnostics" below), the UI is replaced by an error panel and every
binding above except quit becomes inert.

## macOS: Alt bindings and the Option key

The default `Alt+…` bindings only reach the app when the terminal reports
Option as a modifier. Several don't, by default:

| Terminal | Fix |
|---|---|
| Ghostty | `macos-option-as-alt = true` (default `false` on macOS) |
| Terminal.app | Settings → Profiles → Keyboard → "Use Option as Meta key" (off by default) |
| iTerm2 | Settings → Profiles → Keys → Left/Right Option key → "Esc+" |
| kitty / WezTerm / Alacritty | Report Option as Alt by default — nothing to change |

Without that setting, macOS composes the character itself and the
terminal sends only that — Option+1 arrives as a bare "¡" with no modifier
bit, indistinguishable from someone typing "¡" outright. No amount of key
matching recovers the binding from that: it never reaches the app as
`alt+1` at all, on any terminal, because the substitution happens before
the terminal builds the key event. That's why every `Alt+…` binding except
next/prev chat and cross-panel folder cycling has a fallback that doesn't
depend on Option being reported — see the Global table above — and why
rebinding to `ctrl+…` or a function key in `[keys]` is the fix for those
two if Option can't be made to work.

## Configuring keys

`[keys]` in `config.toml` overrides a subset of bindings; case-insensitive,
with `"escape"` accepted as an alias for `"esc"`, `"option"`/`"opt"` for
`"alt"`, and a handful of other common spellings normalized the same way.
Only some fields are actually consulted:

| Field | Default | Wired? |
|-------|---------|--------|
| `quit` | `ctrl+q` | yes |
| `quit_browsing` | `q` | yes — chat list / chat view only; confirms first if the composer holds a draft or attachment |
| `focus_chat_list` | `f1` | yes — *in addition to* the hardcoded `Alt+1` |
| `focus_chat_view` | `f2` | yes — *in addition to* the hardcoded `Alt+2` |
| `focus_composer` | `f3` | yes — *in addition to* the hardcoded `Alt+3` |
| `search` | `/` | yes |
| `global_search` | `ctrl+g` | yes |
| `contacts` | `alt+c` | yes |
| `contacts_alt` | `f4` | yes — the alt-free fallback for `contacts` |
| `help` | `?` | yes |
| `next_folder` | `alt+l` | yes |
| `prev_folder` | `alt+h` | yes |
| `next_chat` | `alt+j` | yes |
| `prev_chat` | `alt+k` | yes |
| `reply` | `r` | yes — a configured value *replaces* the built-in `r` in the chat view (mnemonic, not a motion) |
| `edit_message` | `e` | yes — replaces the built-in `e` |
| `delete_message` | `d` | yes — replaces the built-in `d` |
| `scroll_up` | `k` | yes — an *extra* spelling for scroll-up in the chat view, alongside the always-live `k`/`↑` |
| `scroll_down` | `j` | yes — an extra spelling for scroll-down, alongside `j`/`↓` |
| `page_up` | `pgup` | yes — an extra spelling for page-up, alongside `PgUp` |
| `page_down` | `pgdown` | yes — an extra spelling for page-down, alongside `PgDn` |
| `forward` | `f` | no — parsed and saved so old config files round-trip, but not consulted anywhere; there is no forward-a-message feature to bind it to |

A configured `reply`/`edit_message`/`delete_message`/`scroll_up`/
`scroll_down`/`page_up`/`page_down` that collides with a key already
claimed elsewhere is **refused**, not silently double-bound: the built-in
letter keeps working and the configured value is simply never reached.
"Already claimed" covers keys the chat view hardcodes for itself (`g`/`G`,
`Ctrl+U`/`Ctrl+D`, `n`/`N`, `Ctrl+F`, `Enter`/`o`/`s`) as well as the whole
app-level surface these seven fields cannot see on their own — `h`, `l`,
`i`, `Tab`, `Ctrl+V`, `Ctrl+Q`, `Esc`, and whatever
`quit`/`quit_browsing`/`help`/`search`/`global_search`/`contacts`/
`contacts_alt`/the focus and next/prev chat/folder fields resolve to. That
is what stops e.g. `reply = "h"` from quietly stealing panel movement.
See the `Keys` and `SetReservedKeys` doc comments on
`internal/ui/components/chatview.Model` for the exact resolution order.

A refusal is not left invisible. `-migrate-config` (below) reports the
clash as a warning when it runs, comparing every `[keys]` field against
what the others (and the app) claim — though it only runs on demand, not
on every plain startup. And the `?` help card shows the now-unreachable
action as `(unbound)` instead of a blank or a wrong key: seeing that on
the card means some other binding already holds the letter you wanted, so
free it up (or pick a different key) to restore the action.

Wired bindings are matched before the focused panel sees the key, so a
binding here shadows that key in the chat list and chat view. Most of them
do *not* reach the composer — typing there is only ever entered
deliberately (see "Composer" above), and app-level dispatch claims almost
nothing while it has focus. `keys.quit_browsing` and `keys.help` are
composer-safe: a bare letter there really is inert while composing.
`keys.contacts`/`keys.contacts_alt` also reach the composer (gated only on
no dialog or search overlay being open, not on which panel has focus), so
rebinding one onto something typable is worth thinking about.

`keys.quit` is the one field where a bare printable is not a judgement
call: it is matched before every other check, focus included, so
`quit = "x"` would mean pressing `x` while writing a message quit the app
instead of typing an `x`. **That configuration is now refused**: the
binding falls back to `Ctrl+Q`, and the client says so on stderr at
startup rather than only under `-migrate-config`.

> [`config.example.toml`](../config.example.toml)'s `[keys]` block lists every
> field in the table above at its built-in default; a test
> (`TestExampleConfigKeysMatchDefaults` in `internal/config`) fails the
> build if the two ever drift apart. `internal/config/config.go`'s
> `defaultConfig()` remains the ultimate source of truth. Have a config of
> your own that predates a recent field? Run `-migrate-config` (below)
> rather than hand-editing it — it fills in exactly the gaps and tells you
> what it added.
