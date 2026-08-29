<p align="center">
  <img src="https://upload.wikimedia.org/wikipedia/commons/8/82/Telegram_logo.svg" width="80" />
</p>

<h1 align="center">Telegram CLI</h1>

<p align="center">
  <strong>A full-featured Telegram client for the terminal</strong>
</p>

<p align="center">
  <a href="https://github.com/imtaqin/telegram-cli/actions"><img src="https://github.com/imtaqin/telegram-cli/actions/workflows/ci.yml/badge.svg" alt="Build"></a>
  <a href="https://github.com/imtaqin/telegram-cli/releases"><img src="https://img.shields.io/github/v/release/imtaqin/telegram-cli?include_prereleases" alt="Release"></a>
  <a href="https://github.com/imtaqin/telegram-cli/blob/main/LICENSE"><img src="https://img.shields.io/github/license/imtaqin/telegram-cli" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white" alt="Go">
</p>

---

## Features

- **Chat Management** — Private chats, groups, supergroups, channels
- **Chat Folders** — Folder tab bar above the chat list; `[`/`]`, arrows, digits `1`-`9`, or a click switch tabs (terminal-independent); `Alt+H`/`Alt+L` work too wherever Option-as-Meta is on; bare `h`/`l` move between panels instead (see Keybindings below); Telegram-compatible pinned/include/exclude filter semantics
- **Message Bubbles** — Rounded bordered bubbles, own messages right-aligned, read status indicators, real ANSI-aware word wrap to the bubble width
- **Mute** — Muted chats show 🔕 and render dimmed; desktop notifications, sound, and unread emphasis are suppressed for them
- **Profile Avatars** — Colored initials or rendered profile photos in chat list
- **Incoming Markdown Rendering** — Code blocks, bold, italic, links via [Glamour](https://github.com/charmbracelet/glamour)
- **Outgoing Markdown** — Opt-in Telegram-subset formatting (`**bold**`, `` `code` ``, links, …) applied on send/edit/captions; off by default, see [Outgoing Markdown](#outgoing-markdown)
- **Image Rendering** — Kitty graphics protocol, Sixel, Unicode half-block fallback with CatmullRom scaling
- **Voice/Audio Playback** — Play voice messages and audio inline via `mpv` / `ffplay`
- **Video** — Open videos in external player (`mpv` / `vlc` / `xdg-open`)
- **File Transfer** — Download with `s`, open with `Enter`, progress bar during sync
- **Clipboard Paste** — `Ctrl+V` attaches a clipboard image or file reference and sends it as an inline photo (or document, when the format can't be a photo)
- **Search** — Search chats, messages, and the global Telegram directory; selecting a result jumps straight to that message, scrolled and centred, paging back through history if needed
- **Contacts** — Contact list with online status indicators
- **Group Info** — Member list, admin roles, group description (component exists; not yet reachable from a keybinding — see [TODO.md](TODO.md))
- **Help Overlay** — `?` opens a scrollable, lazygit-style keybinding cheat sheet built from the same bindings the app dispatches on, so it can't drift out of sync
- **Composer Editing Modes** — emacs (readline) or vi (modal, real cursor semantics) line editing, selectable or auto-detected from `$VISUAL`/`$EDITOR`; `Ctrl+O` edits the draft in a full external editor
- **Config Migration** — `-migrate-config` upgrades an existing `config.toml` to current defaults, with a timestamped backup and a change report
- **Authentication** — Phone/SMS code and 2FA password, plus QR login for `telegram-mcp`
- **First-Run Wizard** — Prompts for API credentials and saves config automatically
- **Notifications** — Desktop notifications via `notify-send` / `osascript`, gated on terminal focus for read receipts and skipped for muted chats
- **Responsive Layout** — Dual-panel (wide) or single-panel (narrow terminals)
- **Theming** — Dark and light themes with 256-color support
- **Persistence** — Update-sequence state and the peer access-hash cache persist to a local `state.db`, so updates missed while closed are gap-recovered on next start

## Screenshot

```
╭─ Chat List ──────────────╮╭─ Messages ──────────────────────────────────╮
│ AL  Alice          08:15 ││                                             │
│     see you tomorrow     ││                     ╭─────────────────────╮ │
│ DT  Dev Team       13:24 ││                     │ sounds good 👍      │ │
│     deploy is green   2  ││                     │ 15:20 ✓✓            │ │
│ TG  Telegram       08:03 ││                     ╰─────────────────────╯ │
│     Login code: 12345    ││ ╭──────────────────╮                        │
│ BO  BotFather      14:38 ││ │ Alice            │                        │
│     /newbot          81  ││ │ deal!            │                        │
│                          ││ │ 15:22            │                        │
│                          ││ ╰──────────────────╯                        │
╰──────────────────────────╯╰─────────────────────────────────────────────╯
╭─ Compose ───────────────────────────────────────────────────────────────╮
│ █                                                                       │
│ Enter: send | Esc: cancel                                               │
╰─────────────────────────────────────────────────────────────────────────╯
● Connected  alice    Tab:switch │ Esc:back │ /:search │ Alt+C:contacts
```

## Quick Start

### Prebuilt binaries

Download the latest release for your platform from [Releases](https://github.com/imtaqin/telegram-cli/releases) — Linux, macOS, Windows, and Android/Termux (arm64). Each archive contains all three binaries: `tele-tui`, `telegram-mcp`, `telegram-api`. Releases are fully automatic: every push to `main` bumps the patch version, tags, builds, and publishes (use `#minor` / `#major` in a commit message to bump those instead).

### Build from source

```bash
# Clone
git clone https://github.com/imtaqin/telegram-cli.git
cd telegram-cli

# Build & run — first run prompts for API credentials
make run
```

Pure Go, no CGO, no native dependencies — a plain `go build` works everywhere.

### Prerequisites

- **Go 1.23+**
- **mpv** (optional) — for voice/audio/video playback (`sudo apt install mpv`)
- **Telegram API credentials** — from [my.telegram.org/apps](https://my.telegram.org/apps)

### Windows

```powershell
go build -trimpath -ldflags="-s -w" -o tele-tui.exe .\cmd\teletui
```

On first run, you'll be prompted:

```
╔══════════════════════════════════════════╗
║         Telegram CLI - First Run         ║
╚══════════════════════════════════════════╝

Get your API credentials from:
https://my.telegram.org/apps

Enter API ID: xxxxxxx
Enter API Hash: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
Enter phone number (optional): +628xxxxxxxxxx

Config saved! Starting Telegram CLI...
```

## Keybindings

Motions follow vi convention: `h`/`j`/`k`/`l`, `g`/`G` jump to the ends,
`Ctrl+U`/`Ctrl+D` move a half page, `/` searches whatever you're looking at
and `n`/`N` step through the matches. In the chat list and chat view — the
two browsing panels, side by side — bare `h`/`l` are lazygit-style panel
movement rather than a motion: `l` from the chat list focuses the chat
view, `h` from the chat view focuses the chat list, and each is a no-op at
its own edge rather than wrapping around.

Typing is always entered on purpose — `i`, `c`, `Tab`, a focus key, or a
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
holds an unsent draft or a pending attachment; `Ctrl+C`/`Ctrl+Q` still quit
unconditionally from anywhere, including the composer, and `q` still closes
the help overlay as before.

`/` is contextual in both browsing panels now: from the chat list it opens
a live filter over the visible chats (`Esc` clears it, `Enter` keeps it
applied and leaves a `/query` chip showing in the tab bar); from the chat
view it's in-chat find, as it always was. `Ctrl+G` is the panel-independent
global search from everywhere except the composer.

Press `?` any time outside the composer for a scrollable cheat sheet.
[`internal/app/keymap.go`](internal/app/keymap.go) is where the keymap
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
(`TestReadmeKeymapMatchesHelpSections` in `internal/app`) diffs the key
*set* documented here, and which section each key is filed under, against
what the help card advertises, and fails the build on a mismatch — so this
page can lag behind by at most one unrun `go test`, not indefinitely. It
cannot catch a wrong *description* of what a key does, and it does not
read `keymap.go`'s prose table, `config.go`'s doc comment, or
`config.example.toml` at all, so drift in any of those is still possible.

### Global — any panel

| Key | Action |
|-----|--------|
| `Ctrl+C` / `Ctrl+Q` | Quit (plus `keys.quit` if set to something else) |
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
| `Ctrl+V` | Paste a clipboard image |

### Chat list

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
| `i` / `c` | Compose a message |
| `/` | Filter this list live (`Esc` clears, `Enter` keeps it applied) |
| `q` | Quit — confirms first if the composer holds a draft or attachment |
| click a chat | Select it |
| wheel | Scroll |

### Chat view

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
| `Esc` | Close the find input while it's open; otherwise step back to the chat list (surviving find results are not cleared first) |
| `r` / `e` / `d` | Reply / edit / delete message (`keys.reply` / `keys.edit_message` / `keys.delete_message` replace these, rather than adding to them) |
| `Enter` / `o` | Open attachment |
| `s` | Save attachment |
| `i` / `c` | Compose a message |
| `q` | Quit — confirms first if the composer holds a draft or attachment |

`/` is contextual in both browsing panels — vi convention, "search the
buffer in front of you": from the chat view it opens in-chat find, and
from the chat list (previous table) it opens a live local filter. `/`
opens the global cross-chat search overlay everywhere else that isn't the
composer (which it never reaches). `Ctrl+G` reaches that same global
search from any panel, chat list and chat view included, without the
ambiguity `/` has there.

### Composer

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

Almost nothing else is claimed at app level while the composer has focus,
so neither line-editing keymap below loses a chord. The complete exception
list: `Ctrl+C`/`Ctrl+Q` (quit, plus `keys.quit` if set to something else),
`Ctrl+V`, `Esc` (only when there's nothing to cancel), `Tab`/`Shift+Tab`,
the panel-focus keys (`Alt+1/2/3`, `F1`-`F3`), `Alt+J`/`K`/`H`/`L`
(chat/folder navigation), and `Alt+C`/`F4` (contacts) — every one of the
*hardcoded* spellings there is a modifier or function key no line-editing
keymap binds. `keys.quit` is the one field on that list a rebind can turn
into a problem: it is matched before every other check in `Update`,
focus included, so `quit = "x"` means pressing `x` while writing a
message quits the app instead of typing an `x` — see the Warning under
"Configuring keys" below. No other `Ctrl+<letter>` is claimed at app
level, so `Ctrl+A/B/D/E/F/J/K/O/T/U/W` all reach the composer.

### Composer editing modes

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
and landing in normal mode would swallow the first word. Normal mode uses
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

### Overlays — search, contacts, dialogs

| Key | Action |
|-----|--------|
| `Esc` | Close |
| `Enter` | Accept the selection — in a **dialog**, this means whichever button is currently highlighted, not "confirm" |
| `j` / `k` (or `↓` / `↑`) | Move — a dialog's buttons also move with `Tab`/`Left`/`Right`, and (outside the attach-file prompt, where `j`/`k` are typed as path text instead) with `j`/`k` too |

A **confirm dialog** (deleting a message, quitting with an unsent draft) starts with **Cancel** highlighted, not Confirm, precisely because `Enter` fires whichever button is lit: these dialogs guard destructive or lossy actions, so a reflex `Enter` must not be the thing that performs one. The highlighted button is marked two ways — reversed color, and literal `[ Brackets ]` around its label — so it reads correctly without color, and the dialog also renders its own one-line hint (`←/→ or tab: choose · enter: accept`, or just `enter: dismiss` for a single-button alert) so the behavior is visible in the moment, not only here.

### Help overlay (`?`)

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

### macOS: Alt bindings and the Option key

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

### Configuring keys

`[keys]` in `config.toml` overrides a subset of bindings; case-insensitive,
with `"escape"` accepted as an alias for `"esc"`, `"option"`/`"opt"` for
`"alt"`, and a handful of other common spellings normalized the same way.
Only some fields are actually consulted:

| Field | Default | Wired? |
|-------|---------|--------|
| `quit` | `ctrl+c` | yes |
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
`i`, `c`, `Tab`, `Ctrl+V`, `Ctrl+C`/`Ctrl+Q`, `Esc`, and whatever
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

**Warning:** wired bindings are matched before the focused panel sees the
key, so a binding here shadows that key in the chat list and chat view.
Most of them do *not* reach the composer — typing there is only ever
entered deliberately (see "Composer" above), and app-level dispatch
claims almost nothing while it has focus. `keys.quit` is the exception:
it is matched before every other check, focus included, so `quit = "x"`
means pressing `x` while writing a message quits the app instead of
typing an `x`. `keys.quit_browsing` and `keys.help` are correctly
composer-safe — a bare letter there really is inert while composing.
`keys.contacts`/`keys.contacts_alt` also reach the composer (gated only
on no dialog or search overlay being open, not on which panel has focus),
but their defaults (`alt+c`, `f4`) are a modifier and a function key, so
this only bites if you rebind one onto something typable. Nothing rejects
a `quit` rebind onto a printable character — the collision check above
only compares `[keys]` fields against each other and against what the app
already claims, never against "is this a character someone types."
Prefer a chord or a function key for `quit` especially.

> [`config.example.toml`](config.example.toml)'s `[keys]` block lists every
> field in the table above at its built-in default; a test
> (`TestExampleConfigKeysMatchDefaults` in `internal/config`) fails the
> build if the two ever drift apart. `internal/config/config.go`'s
> `defaultConfig()` remains the ultimate source of truth. Have a config of
> your own that predates a recent field? Run `-migrate-config` (below)
> rather than hand-editing it — it fills in exactly the gaps and tells you
> what it added.

### Clipboard paste (`Ctrl+V`)

`Ctrl+V` works from any panel while a chat is open: it spools whatever image
the system clipboard holds into a temporary file, attaches it to the
composer, and sends it as an inline photo on `Enter` (add a caption first if
you want one). If the clipboard holds a *file* reference — an image copied in
Finder or a file manager — that file is attached in place instead of being
copied.

An image that's only available as TIFF (some older macOS apps offer nothing
else) is converted to PNG on macOS and still sent as a photo; on Linux/BSD,
where no such conversion happens, a TIFF-only clipboard is spooled as-is and
sent as a **document** instead, since Telegram's inline-photo upload rejects
TIFF. `Ctrl+V` (and `Ctrl+T`) are refused while editing a message — edits
can't carry attachments — and switching chats or chat search results discards
a pending attachment along with the rest of the draft. If sending a pasted
attachment fails, it's restored to the composer (not lost), as long as
nothing newer has taken its place in the meantime.

Spooled files are deleted once sent, discarded, or replaced. The spool
directory itself is **not** removed on exit — Bubble Tea never waits for
in-flight uploads on quit, so deleting it there would race an upload still
reading from it. Instead it's swept the next time the app starts, once it's
confirmed no longer in use (a process-liveness check, with a safety floor
that never touches a directory younger than 48h).

Clipboard access uses the platform's own tooling: `osascript` (plus `sips`
for the TIFF fallback) on macOS, `wl-paste` (Wayland) or `xclip` (X11) on
Linux/BSD — install `wl-clipboard` or `xclip` if neither is present — and
PowerShell on Windows.

## Chat Folders

A tab bar above the chat list shows your Telegram folders, plus a
synthesized "All" tab that's always present — even for accounts with no
custom folders, and even before the folder list has loaded. See
[Keybindings](#keybindings) for how to switch tabs — several
terminal-independent ways, plus `Alt+H`/`Alt+L` where Option-as-Meta is on.

Filtering follows Telegram's own folder semantics: chats explicitly
*excluded* from a folder are always hidden; chats explicitly *pinned* or
*included* are always shown, bypassing both the category flags and the
mute/read excludes below; otherwise, if a folder sets any category flag
(contacts, groups, channels, bots), a chat must match at least one of them;
`ExcludeMuted` / `ExcludeRead` then drop muted/read chats from what's left.

This build's `User` type carries no "is a contact" field, so a folder's
`Contacts` and `NonContacts` flags can't be told apart — both are treated as
"any private, non-bot chat." This over-includes relative to real Telegram
behaviour (a contacts-only folder also shows non-contact DMs here) but never
under-includes.

## Mute

Muted chats show a 🔕 marker and render dimmed — faint title, faint unread
badge — in the chat list. Desktop notifications and the notification sound
are suppressed for messages from a muted chat, and for your own outgoing
messages (which would otherwise double-notify when they arrive back as an
update from another logged-in device). The status bar's unread counter shows
the unmuted count, with the true total in parentheses when it differs, e.g.
`[3 unread (7 total)]`.

## Message Rendering & UX

- **Word wrap** — message bubbles use real ANSI-aware word wrapping to the
  bubble's inner width, instead of hard truncation; a single unbroken token
  longer than the width still hard-wraps so it can't blow past the border.
- **Jump to search result** — picking a chat or message search hit opens the
  chat scrolled to (roughly centred on) that message, paging back through a
  few pages of history if it isn't already loaded.
- **Read receipts follow terminal focus** — a message only gets marked read
  while the terminal actually has focus; if you're viewing the chat but the
  terminal is unfocused, the read receipt is sent when focus returns. This
  needs terminal focus-reporting support — in tmux, add `set -g
  focus-events on` to `~/.tmux.conf`, or receipts won't update while inside
  a session.
- **Exact scroll targeting** — scrolling and jump-to-message use a cached
  per-message line index rather than a rough per-message jump, so the target
  lands on the exact line even as photo art and sender-name lookups change
  bubble heights after the initial render.

## Configuration

Config is stored at `~/.config/tele-tui/config.toml`. See [`config.example.toml`](config.example.toml) for all options:

```toml
[telegram]
api_id = 12345678
api_hash = "your_api_hash"

[ui]
theme = "dark"           # "dark" or "light"

[media]
image_protocol = "auto"  # "auto", "kitty", "sixel", "blocks"
voice_player = "mpv"     # "mpv", "ffplay"
video_player = "mpv"     # "mpv", "vlc", "xdg-open"
```

## Config Migration (`-migrate-config`)

```bash
bin/tele-tui -migrate-config
```

Brings an existing `~/.config/tele-tui/config.toml` (or wherever
`TELETUI_CONFIG` points) up to current defaults and exits without starting
the app. **Recommended for any config that predates this branch** — several
key defaults changed underneath it, and a config from before will otherwise
carry conflicting bindings silently.

What one run does:

- **Retires stale key defaults.** A `[keys]` field holding a value that used
  to ship as the default — not something you chose — is replaced with the
  current one: `focus_chat_list`/`focus_chat_view`/`focus_composer` from
  `ctrl+1`/`ctrl+2`/`ctrl+3` to `f1`/`f2`/`f3`; `contacts` from `ctrl+k`
  (now the composer's kill-to-end-of-line) to `alt+c`; `next_chat`/
  `prev_chat` from `ctrl+j`/`ctrl+k` (now the newline chord and
  kill-to-start-of-line) to `alt+j`/`alt+k`. A binding you actually chose
  is left exactly alone, even if it now collides with something — see the
  collision report below.
- **Fills in fields your file never had**, at their current default: any
  newer `[keys]` field (`help`, `global_search`, `contacts_alt`, …),
  `ui.compose_editing` (`"auto"`), and `storage.state_file` — written out
  explicitly as the path the client would otherwise derive implicitly
  (next to `session_file`), so the location stops being implied. Also
  `ui.parse_markdown`, but as a special case: it's set to `true` on
  migration and the change is reported, on the reasoning that an existing
  user already has a working setup and the feature is worth having —
  brand-new configs still default to `false` (see Outgoing Markdown below).
- **Reports what it did**, field by field, as `field   old -> new`; any
  config *section* your file didn't have at all, now added in full with
  defaults; any key your file had that this version no longer recognizes
  (dropped by the rewrite, but preserved in the backup); and any `[keys]`
  bindings that now collide with each other — checked only among the
  fields `internal/app` actually dispatches on, so a value that happens to
  match a hardcoded binding (`i`/`c`, the composer's readline chords, …)
  isn't flagged.
- **Backs up first, unconditionally.** The original is copied to
  `config.toml.bak`, byte-for-byte, at `0600`, before anything is
  rewritten — it's the only copy that keeps your comments and key
  ordering, since the migration re-encodes the whole file through the TOML
  marshaller. An existing backup is never overwritten: migrating twice
  gets a `config.toml.bak.<YYYYMMDD-HHMMSS>` the second time, so a repeat
  run can't clobber your one copy of the real original.
- **Writes atomically, at `0600`, following symlinks.** Both the config
  and its backup land at the *resolved* path — a `config.toml` symlinked
  in from a dotfiles repo gets rewritten in place there, not replaced with
  a plain file that breaks the link. The write itself goes to a temp file
  in the same directory, `fsync`'d, then renamed over the target, so a
  crash mid-write can't leave a truncated config.
- **Keeps paths portable.** `session_file`/`files_dir`/`state_file` are
  written back exactly as your file had them — a `~/...` form stays
  `~/...` — and a path your file lacked is filled with the same portable
  `~/...` literal the built-in default uses, never an absolute path baked
  to one machine's home directory.
- **Idempotent.** A config already at current defaults reports "already up
  to date" and writes nothing (no backup, no rewrite); running it again
  right after a successful migration is a no-op.

If there's no config file to migrate — including a `TELETUI_CONFIG` path
that doesn't exist — it says so and exits; there's nothing to back up,
since the app writes a fresh default config on first run anyway.

## Outgoing Markdown

Off by default. Turn it on with `ui.parse_markdown = true` in
`config.toml` — or run `-migrate-config` (the previous section), which
turns it on for an existing config and reports the change.

| Markup | Result |
|---|---|
| `**text**` | bold |
| `__text__` | italic |
| `` `text` `` | inline code |
| Triple backtick fence, optional language on the opening line | fenced code block — a single word right after the opening fence, followed by a newline, is read as the language and is not part of the content |
| `~~text~~` | strikethrough |
| `\|\|text\|\|` | spoiler |
| `[text](url)` | link — parentheses inside `url` may nest if balanced, and the scheme must be `http`, `https`, `tg`, `mailto`, or `ftp` |

Applies everywhere typed text reaches the wire: text messages, edits, and
photo/file captions alike.

**Why off by default:** the composer has no preview, so with parsing on
silently by default the first time you'd notice is after a message has
already gone out — and the syntax overlaps with things people paste
verbatim. `__init__` arrives as `init`; a code snippet full of `**` loses
it; a table of `||` cells collapses into spoilers. Opting in means knowing
that transformation happens. `-migrate-config` turns it on for existing
configs specifically because they already have a working setup and it's
worth having — but tells you so in its report — and the shipped
[`config.example.toml`](config.example.toml) turns it on too, with the
same warning inline.

Fallback guarantees, so a message is never mangled by a marker that wasn't
meant as one:

- An opening marker with no matching closer — or an empty span like
  `****` — is sent exactly as typed.
- `` `code` `` and ` ```pre``` ` are opaque: markers found *inside* them
  are never interpreted, which is what makes it possible to send markdown
  *about* markdown.
- A `[text](url)` whose scheme isn't on the allowlist (`javascript:`,
  `data:`, a bare/schemeless string, …) is sent exactly as typed too — not
  silently dropped or half-converted. The check is deliberately an
  allowlist rather than a blocklist: new dangerous schemes get invented
  faster than a blocklist can track them.

## Persistence

The TUI (and any process's `login` subcommand) persists Telegram's
update-sequence state (pts/qts/seq/date) and a peer access-hash cache to a
bbolt database, `state.db`, next to the session file it's using — by
default `~/.local/share/tele-tui/state.db`. This is what lets updates that
arrived while the app was closed be recovered via `updates.getDifference`
on the next start, and avoids re-resolving every peer from scratch each
session. The peer cache is namespaced per session file (a hash of its
path), so multiple accounts sharing one `state.db` — e.g. `telegram-api`
or `telegram-mcp` logged in as a different account than the TUI — don't
clobber each other's access hashes. Override the path with `state_file`
under `[storage]` in `config.toml`.

`telegram-api serve` and `telegram-mcp serve` never open it: bbolt takes an
exclusive file lock, and both run their Telegram connection in no-updates
(RPC-only) mode specifically so they can sit alongside a possibly-running
TUI over the same data directory without contending for it. If the
database can't be opened at all (e.g. briefly locked by another process's
`login`), that's logged and non-fatal — the run just falls back to gotd's
in-memory state for that session, losing gap recovery but nothing else.

## Troubleshooting & Diagnostics

**Logging is silenced by default**, `tele-tui` only. The TUI owns the
terminal in raw mode, so a stray log write lands in the middle of a
rendered frame — this used to happen from background goroutines logging
"connection state: connecting" every time the network blipped.
`telegram-api` and `telegram-mcp` are unaffected and keep logging to
stderr normally.

Get the log back with `TELETUI_DEBUG`:

```bash
TELETUI_DEBUG=/tmp/teletui.log bin/tele-tui
```

The file is opened in **append** mode with a
`=== teletui session started <RFC3339> (pid <pid>) ===` banner per run,
never truncated — debugging this app usually means restarting it
repeatedly, and truncating on every start would destroy the log of the run
that actually reproduced the problem. If the path can't be opened, that's
reported once on stderr (this runs before the alt screen takes over) and
the run continues with logging disabled, rather than sitting there waiting
for output that will never arrive.

**If the Telegram client dies for good** — the session was revoked from
another device, or the connection failed in a way the client gave up on —
the whole UI is replaced by an error panel: what happened, a plain
statement that it will not recover on its own, and a nudge to restart.
Every keybinding except quit goes inert at that point — the panels behind
the error screen are still holding their last-known state, but acting on
them would only mutate data you can no longer see. The panel points at
`TELETUI_DEBUG` for more detail.

**Non-fatal degradations** — the client keeps running, just with something
turned off — surface as a `⚠ ...` notice on the composer's hint line
instead of a full-screen panel. Two you may see in practice: the
update-state database being locked by another `tele-tui`/`login` process
(gap recovery disabled for this run only — see [Persistence](#persistence)),
and the peer cache in `state.db` having belonged to a different account
(rebuilt automatically, also covered there).

## TUI 2.0 design

The proposed terminal-native redesign, repository reconciliation, open product
decisions, phased delivery plan, and verification matrix are recorded in
[docs/tui-2.0.md](docs/tui-2.0.md). The document is intentionally a plan:
implementation starts after its data-scope and keymap decisions are resolved.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                   Bubbletea v2                        │
│  ╭────────╮  ╭──────────────╮  ╭──────────────────╮  │
│  │  Chat  │  │   Messages   │  │    Composer       │  │
│  │  List  │  │   (bubbles)  │  │  (text input)    │  │
│  ╰────────╯  ╰──────────────╯  ╰──────────────────╯  │
│  ╭──────────────────────────────────────────────────╮ │
│  │              Status Bar + Help                   │ │
│  ╰──────────────────────────────────────────────────╯ │
├──────────────────────────────────────────────────────┤
│              Store (thread-safe cache)                │
│         Chats · Messages · Users · Files              │
├──────────────────────────────────────────────────────┤
│         gotd/td — pure Go MTProto client               │
│      Update dispatcher → p.Send(tea.Msg)              │
└──────────────────────────────────────────────────────┘
```

## Project Structure

```
cmd/teletui/              Entry point + first-run wizard
internal/
  app/                    Root bubbletea model, key routing, layout
  config/                 TOML config loader + auto-save
  telegram/               gotd/td client wrapper + domain types
    types.go              Domain types (Chat/Message/User/File...)
    auth.go               Phone/code/2FA auth flow
    listener.go           Update dispatcher → tea.Msg bridge
    chats.go              Dialog list, history, search
    messages.go           Send/edit/fetch messages
    files.go              File registry + downloader
  ui/
    theme/                256-color dark/light themes
    layout/               Responsive panel sizing
    widgets/              List, textarea, spinner, tabs, progress bar
    components/
      chatlist/           Chat list with avatars + unread badges
      chatview/           Message bubbles + media playback
      composer/           Text input with reply/edit modes
      auth/               Auth flow screens
      search/             Tabbed search overlay
      contacts/           Contact list
      groupinfo/          Group/channel info panel
      statusbar/          Connection status + typing indicators
      dialog/             Modal dialogs
  media/                  Image rendering (kitty/sixel/blocks)
  render/                 Message content → terminal output
  notification/           Desktop notifications
  store/                  Thread-safe in-memory caches
pkg/utils/                String/time/sanitize utilities
```

## Building from Source

```bash
make build    # compile binaries → bin/tele-tui + bin/telegram-mcp (CGO_ENABLED=0)
make run      # build + run
make test     # run tests
make clean    # remove build artifacts
```

## MCP Server

The repo also ships `telegram-mcp`, an [MCP](https://modelcontextprotocol.io) server (stdio transport) that exposes your Telegram account to AI agents. It shares the config with the TUI but uses its own session file.

### Login

The MCP server uses a separate session (`session-mcp.json`), so log in once even if the TUI is already logged in:

```bash
bin/telegram-mcp login        # phone → code → 2FA
bin/telegram-mcp login --qr   # scan in Telegram → Settings → Devices
```

QR tokens refresh automatically until the login is accepted or cancelled. If
the account has two-step verification enabled, the password is read without
echoing it to the terminal. Both login modes write
`~/.local/share/tele-tui/session-mcp.json` by default.

### Client configuration

Register the server in your MCP client, e.g.:

```json
{
  "mcpServers": {
    "telegram": {
      "command": "telegram-mcp",
      "args": ["serve"]
    }
  }
}
```

`serve` is the default subcommand; it fails fast with `session not authorized, run 'telegram-mcp login' first` on stderr when the session is missing or expired.

### Tools

| Tool | Description |
|------|-------------|
| `get_me` | Authorized user info |
| `list_chats` | Dialog list (pinned first, then recent) |
| `get_chat_history` | Messages of a chat, newest first |
| `search_chats` | Search chats by title/username |
| `search_messages` | Global message search |
| `get_contacts` | Contact list |
| `send_message` | Send a text message (optional reply) |
| `send_file` | Upload a local file as a document (optional caption) |
| `edit_message` | Edit a message text |
| `mark_read` | Mark messages as read |
| `download_media` | Download message media, returns local path |

### Sessions

`telegram-mcp` uses its own session file (`session-mcp.json`) so the TUI and any number of MCP server processes each get their own Telegram connection with full realtime updates — like running Telegram on multiple devices. Set `TELETUI_SESSION=/path/to/session.json` to override the session path if you ever need to share one explicitly.

## REST API

`telegram-api` is a plain HTTP/JSON companion to the MCP server — same Telegram layer, same endpoints as the MCP tools, standard library only.

### Run

```bash
bin/telegram-api login &  # if not already logged in via tele-tui or telegram-mcp
bin/telegram-api serve    # listens on 127.0.0.1:8080, auth token auto-generated on first run
```

It binds **127.0.0.1 only** by default. Change the address with `-addr` or the `TELETUI_API_ADDR` env var:

```bash
bin/telegram-api serve -addr 127.0.0.1:9090
# or
TELETUI_API_ADDR=127.0.0.1:9090 bin/telegram-api serve
```

Precedence: `-addr` flag > `TELETUI_API_ADDR` > `127.0.0.1:8080`. It shares the MCP session file (`session-mcp.json`) — login via `telegram-api login` or `telegram-mcp login` once, both work. `TELETUI_SESSION` overrides the session path.

### Authentication

Every route except `GET /api/health` requires a bearer token, **including
requests to 127.0.0.1** — there is no loopback exemption. The token is
resolved in this order, first non-empty wins:

1. `-token-file <path>` flag
2. `TELETUI_API_TOKEN` environment variable
3. a default token file next to the (already `-mcp`-suffixed) session file —
   typically `~/.local/share/tele-tui/api-token` — **auto-generated** (32
   random bytes, hex-encoded) and written with **`0600`** permissions the
   first time `serve` runs without one

The server refuses to start if the resolved token is empty or blank —
serving unauthenticated by accident isn't possible short of the explicit
opt-out below. The token value itself is never logged, only where it came
from.

Clients send it as `Authorization: Bearer <token>`; the comparison is
constant-time. A missing or wrong token gets `401`.

```bash
TOKEN=$(cat ~/.local/share/tele-tui/api-token)

curl -s http://127.0.0.1:8080/api/chats?limit=10 \
  -H "Authorization: Bearer $TOKEN"

curl -s -X POST http://127.0.0.1:8080/api/send \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"chat_id": 123456789, "text": "hello from the API"}'
```

`-insecure-no-auth` disables the token check entirely and logs a loud
warning on startup — only use it on a fully trusted, isolated network:

```bash
bin/telegram-api serve -insecure-no-auth
```

### Binding beyond localhost

The server always validates the request's `Host` header (and `Origin`, if
present) against an allowlist — `localhost` / `127.0.0.1` / `::1` plus
whatever the listen address and `-allowed-host` add — before it even looks
at the auth token, so a browser-driven cross-origin or DNS-rebinding request
is rejected before it gets a chance to try one. A request with a
disallowed Host/Origin gets `403` with the current allowlist in the response
body. Binding a wildcard address (`0.0.0.0`, `::`, or no host at all, e.g.
`-addr :8080`) does **not** auto-allow anything — exposing the server beyond
loopback needs an explicit `-allowed-host`:

```bash
bin/telegram-api serve -addr 0.0.0.0:8080 -allowed-host 192.168.1.50
```

`-allowed-host` is repeatable and accepts a comma-separated list; `0.0.0.0`
and `::` are rejected even if passed explicitly (they can be tricked into
matching "localhost" by a browser — the "0.0.0.0 day" class of bug).

Every `POST` request must send `Content-Type: application/json` (parameters
are ignored, so `application/json; charset=utf-8` is fine) or gets `415`.

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Health check (no Telegram call, no auth required) |
| GET | `/api/me` | Authorized user info |
| GET | `/api/chats?limit=` | Dialog list |
| GET | `/api/chats/{id}/history?limit=&from_message_id=&offset=` | Chat messages, newest first |
| GET | `/api/search/chats?q=&limit=` | Search chats |
| GET | `/api/search/messages?q=&limit=` | Global message search |
| GET | `/api/contacts` | Contact list |
| POST | `/api/send` | Send text `{chat_id, text, reply_to_message_id?}` |
| POST | `/api/send-file` | Send file `{chat_id, path, caption?, reply_to_message_id?}` |
| POST | `/api/edit` | Edit message `{chat_id, message_id, text}` |
| POST | `/api/mark-read` | Mark read `{chat_id, message_ids[]}` |
| GET | `/api/media?chat_id=&message_id=` | Download message media, returns local path |

Errors are JSON (`{"error": "..."}`) with status 400 (bad params), 401
(missing/invalid bearer token), 403 (forbidden Host/Origin — body includes
`allowed_hosts`), 404 (unknown route/chat), 415 (POST without
`Content-Type: application/json`), or 502 (upstream Telegram error).

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/awesome`)
3. Commit your changes
4. Push to the branch
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) for details.

## Credits

- [Bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [Glamour](https://github.com/charmbracelet/glamour) — Markdown rendering
- [gotd/td](https://github.com/gotd/td) — Pure Go Telegram (MTProto) client
