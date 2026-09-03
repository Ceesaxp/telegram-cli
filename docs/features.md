# Features

What this client does, at the level of "can it" rather than "which key" —
for that, see [keys.md](keys.md). Settings are in
[configuration.md](configuration.md), and `config.example.toml` documents
every one of them.

## What the frame is

The **frame** is TUI 2.0: a one-row top bar with folder tabs and connection
state, borderless columns divided by single-cell rules, and a context-sensitive
hint bar at the foot. Every row is exactly the terminal width.

The **chat list** is TUI 2.0 too: two-line rows, a type sigil instead of an
avatar (`@` DM, `#` group, `!` channel, `~` saved messages), a filter header
and a contextual footer.

The **thread** is TUI 2.0 as well: a fixed time / sender / body grid in place
of bubbles, with a 24-cell gutter that compresses to 20 on a narrow pane, day
and unread dividers, a cursor bar marking the message the action keys act on,
and delivery marks read from the chat's own read markers rather than assumed.
Inside a message, code fences are framed and numbered, quotes get a rule,
lists get a hanging indent, attachments get a metadata card, and spoilers
stay hidden until `x`.

The **composer** carries an always-visible mode badge — NORMAL, INSERT or
COMMAND — so one glance answers whether the next letter types or navigates.
`Ctrl+P` expands it to a split view with your source on the left and what
will actually be sent on the right. Drafts are per chat: switching away parks
one and switching back restores it, reply target and staged attachment
included.

The **context rail** is the last panel: pinned messages, members and shared
files in a thirty-cell column beside the thread, on a terminal 118 columns or
wider. Backtick toggles it, `ui.rail` sets the default. Nothing about it is
fetched until you open it, so a chat you open with the rail closed costs
exactly what it did before the rail existed.

`Enter` on a photo opens it **full-pane in the terminal**, drawn with
whichever image protocol `media.image_protocol` resolves to. Nothing is
fetched or drawn until you press it, `Esc` puts it away, and `o` hands the
same file to your system viewer if the terminal draws it badly. `y` copies
the selected message's text, `space` plays a voice note, and `M` clears the
unread badge without moving your place in the history.

Reactions render as chips under the message, polls show their answers with
scaled bars and percentages, a link preview gets a cyan rule with the host,
title and two lines of description, and a voice note draws its 24-cell
waveform beside its duration. What is **not** there is a voice note's
transcript — that is a Telegram premium call this client does not make; see
[the design record](tui-2.0.md).

## Everything, briefly

- **Chat Management** — Private chats, groups, supergroups, channels
- **Chat Folders** — Numbered folder tabs in the top bar (they moved there with the TUI 2.0 frame; selection and keys are unchanged); `[`/`]`, arrows, digits `1`-`9`, or a click switch tabs (terminal-independent); `Alt+H`/`Alt+L` work too wherever Option-as-Meta is on; bare `h`/`l` move between panels instead (see [keys.md](keys.md)); Telegram-compatible pinned/include/exclude filter semantics
- **Thread Grid** — Messages on a fixed time / sender / body grid rather than bubbles: one body column the whole conversation aligns to, deterministic per-sender colours, day and unread dividers, single-row reply quotes, and delivery marks read from the chat's read markers. Real ANSI-aware word wrap, measured in display cells
- **Where You Are** — The chat list times are relative (`2m`, `4h`, `yd`, `2d`), because a list is read for recency. The thread header carries the chat's kind and member count, which buffer you are in, and a `bot`/`top`/`all` marker beside the line position, so "is there more below" is answered without comparing two numbers. The hint bar counts what there is — `idx 12 msgs · 9 buffers · 37 unread` — dropping each part when it would say nothing, and saying `3 of 9 buffers` while the list is filtered, so a count that falls does not read as chats going missing
- **Content Blocks** — Framed, numbered code fences with diff and comment colouring and horizontal truncation (code is never re-wrapped); ruled block quotes; hanging-indent lists; metadata cards for attachments that collapse to one line on a narrow pane; spoilers drawn in their own background until `x` reveals them
- **Reactions** — `+` on a message opens a one-row picker of Telegram's twelve defaults; pick with `1`-`9`/`0`, the arrows, or `enter`. It opens on the one you already left, so pressing `enter` takes it off. Nothing is written locally — the chips redraw from what the server says
- **Channel Discussions** — A channel post with comments says so under it — `12 comments · t to open`, amber when there is something new — and `t` jumps to the linked group at the post's own copy. Nothing said it before: a channel looked like a place where nothing could be said back
- **Pinned Messages** — `p` pins the selected message or unpins it, reading which from the message itself so one key does both. Silent: no "X pinned a message" line goes into the chat. The context rail lists a chat's pins
- **Mute** — Muted chats show 🔕 and render dimmed; desktop notifications, sound, and unread emphasis are suppressed for them
- **Incoming Rich Text** — Bold, italic, underline, strikethrough, inline code, links, mentions and spoilers rendered from Telegram's own text entities in a semantic palette, so what you see is what was sent rather than a Markdown round-trip. Overlapping and nested spans are layered rather than replayed
- **Outgoing Markdown** — Opt-in Telegram-subset formatting (`**bold**`, `` `code` ``, links, …) applied on send/edit/captions; off by default, see [Outgoing Markdown](#outgoing-markdown)
- **Image Rendering** — Kitty graphics protocol, Sixel, Unicode half-block fallback with CatmullRom scaling
- **Voice/Audio Playback** — Play voice messages and audio inline via `mpv` / `ffplay`
- **Video** — Open videos in external player (`mpv` / `vlc` / `xdg-open`)
- **File Transfer** — Save with `s` into `storage.download_dir` (`~/Downloads` by default) under the sender's own filename, never overwriting; open with `Enter`, progress bar during sync
- **Clipboard Paste** — `Ctrl+V` attaches a clipboard image or file reference and sends it as an inline photo (or document, when the format can't be a photo)
- **A chat list that goes back further than one page** — the first fifty dialogs load at startup and the next page is fetched as the cursor nears the bottom, so an account with hundreds of chats is scrollable rather than searchable-only. The unread total counts the chats that are loaded, and grows as more arrive
- **Search** — Search chats, messages, and the global Telegram directory; selecting a result jumps straight to that message, scrolled and centred, paging back through history if needed
- **Contacts** — Contact list with online status indicators
- **Help Overlay** — `?` opens a scrollable, lazygit-style keybinding cheat sheet built from the same bindings the app dispatches on, so it can't drift out of sync
- **Composer Editing Modes** — emacs (readline) or vi (modal, real cursor semantics) line editing, selectable or auto-detected from `$VISUAL`/`$EDITOR`; `Ctrl+O` edits the draft in a full external editor
- **Mode Badge** — NORMAL / INSERT / COMMAND at the head of the composer row, derived from what the next key will actually do rather than from a separate flag, so it cannot contradict the keymap
- **Split Compose Preview** — `Ctrl+P` opens a two-column view: your source with line numbers on the left, what will actually be sent on the right, rendered by the same code that draws received messages
- **Per-Chat Drafts** — switching chats parks the draft and restores that chat’s own, reply target and staged attachment included; the chat list shows `draft: saved locally` where the preview would be. In memory for the session, never synced to Telegram
- **Config Migration** — `-migrate-config` upgrades an existing `config.toml` to current defaults, with a timestamped backup and a change report
- **Authentication** — Phone/SMS code and 2FA password, plus QR login for `telegram-mcp`
- **First-Run Wizard** — Prompts for API credentials and saves config automatically
- **Notifications** — Posted by your terminal itself where it supports it (so they carry the terminal's name and icon, not "Script Editor", and work over ssh), falling back to `notify-send` / `osascript`; skipped for muted chats. See [Notifications](#notifications)
- **Responsive Layout** — Borderless columns sized by terminal width: chat list 38 cells, thread flexing, dropping to a narrower list and then a single panel as space runs out. Every row is exactly the terminal width
- **Theming** — Dark and light themes with 256-color support
- **Inline Images** — `ui.inline_images` chooses where a photo is drawn: `never` and `on_open` (the default) show a metadata card in the thread and the picture full-pane when you press `Enter`; `always` also draws an eight-row preview inline. The bound is deliberate — a message whose height changes when a thumbnail lands moves the history under you mid-scroll
- **Media Overlay** — `Enter` on a photo draws it full-pane, with the protocol `media.image_protocol` resolves to (kitty, sixel, or Unicode half-blocks). Nothing is downloaded or drawn until you ask, `Esc` closes, and closing removes the image the terminal was holding rather than leaving it on screen
- **Terminal Hyperlinks** — `ui.hyperlinks` puts OSC 8 links on the links in a message: `auto` (only on terminals known to support them), `never`, or `always`. Links are cyan and underlined either way; this adds the click. Only `http`, `https`, `mailto` and `tg` links are made clickable — a terminal hands the URI to your platform's opener, so the scheme decides what runs, and a message's link is written by whoever sent it. Anything else still reads as a link and still shows where it claims to go; it just is not handed to the opener
- **Context Rail** — A thirty-cell column beside the thread with the chat's pinned messages, members, and shared files or links, chosen by chat type. Toggled with `` ` ``, defaulted by `ui.rail`, and shown only at 118 columns or wider. Nothing is fetched until you open it, and every section says whether it is loading, empty, or unavailable rather than leaving you to guess
- **Persistence** — Update-sequence state and the peer access-hash cache persist to a local `state.db`, so updates missed while closed are gap-recovered on next start

## Chat Folders

A tab bar above the chat list shows your Telegram folders, plus a
synthesized "All" tab that's always present — even for accounts with no
custom folders, and even before the folder list has loaded. See
[keys.md](keys.md) for how to switch tabs — several
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

- **Word wrap** — the thread body uses real ANSI-aware word wrapping to the
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
[`config.example.toml`](../config.example.toml) turns it on too, with the
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

## Notifications

Set with `notifications.method` in `config.toml`:

| Value | Who posts it |
|---|---|
| `"auto"` (default) | The terminal, where it is known to understand the sequence; the system otherwise. |
| `"terminal"` | Always the terminal — for one the allowlist doesn't know. A terminal that doesn't understand the sequence **prints** it, into whatever is on screen. |
| `"system"` | Always the platform notifier: `notify-send` on Linux, `osascript` on macOS. |

**Why the terminal, and why macOS says "Script Editor".** `osascript` posts
notifications as Script Editor, because the process *is* Script Editor — so
the alert carries its name, its icon, and its notification settings. No flag
changes that, and a command-line binary can't post under its own name on
macOS at all: `UserNotifications` requires a bundle identifier, which
requires shipping an `.app`.

Your terminal already has all three, granted deliberately. Asking it to post
the alert gives you the right name and icon, and works over ssh — where a
system notification fires on the wrong machine.

| Terminal | Support |
|---|---|
| kitty, Ghostty, WezTerm, foot, urxvt | Title and body (OSC 777) |
| iTerm2, Windows Terminal | Body only (OSC 9) — the sender is folded into the message |
| Terminal.app | None; falls back to the system |
| Under tmux or screen | Nothing is sent — whether the sequence gets through depends on configuration that can't be read from inside |

Muted chats never notify. The mute flag is read from your account's notify
settings, including for chats below the first page of the dialog list — a
message from one of those holds its notification until the client has been
told who the chat is, rather than ringing first and asking afterwards. If
that answer doesn't arrive within a few seconds the notification goes out
anyway, unnamed: a late alert beats a lost one.

## Clipboard paste (`Ctrl+V`)

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
