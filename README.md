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
- **Chat Folders** — Folder tab bar above the chat list, `Alt+H` / `Alt+L` to cycle, Telegram-compatible pinned/include/exclude filter semantics
- **Message Bubbles** — Rounded bordered bubbles, own messages right-aligned, read status indicators, real ANSI-aware word wrap to the bubble width
- **Mute** — Muted chats show 🔕 and render dimmed; desktop notifications, sound, and unread emphasis are suppressed for them
- **Profile Avatars** — Colored initials or rendered profile photos in chat list
- **Markdown Rendering** — Code blocks, bold, italic, links via [Glamour](https://github.com/charmbracelet/glamour)
- **Image Rendering** — Kitty graphics protocol, Sixel, Unicode half-block fallback with CatmullRom scaling
- **Voice/Audio Playback** — Play voice messages and audio inline via `mpv` / `ffplay`
- **Video** — Open videos in external player (`mpv` / `vlc` / `xdg-open`)
- **File Transfer** — Download with `s`, open with `Enter`, progress bar during sync
- **Clipboard Paste** — `Ctrl+V` attaches a clipboard image or file reference and sends it as an inline photo (or document, when the format can't be a photo)
- **Search** — Search chats, messages, and the global Telegram directory; selecting a result jumps straight to that message, scrolled and centred, paging back through history if needed
- **Contacts** — Contact list with online status indicators
- **Group Info** — Member list, admin roles, group description (component exists; not yet reachable from a keybinding — see [TODO.md](TODO.md))
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

`Esc` works app-wide: it closes whatever overlay/dialog is open, clears a
composer that's mid-reply/edit or holding a pending attachment, or otherwise
steps focus back toward the chat list — one `Esc` at a time. `F1`/`F2`/`F3`
also work app-wide for panel focus (this used to be broken — a config-vs-key
casing mismatch meant they were silently ignored — it's fixed now).

### Navigation

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Cycle chat list → messages → composer (and back) |
| `Esc` | Close overlay/dialog, clear reply/edit/attachment, or go back |
| `F1` / `Alt+1` | Focus chat list |
| `F2` / `Alt+2` | Focus messages |
| `F3` / `Alt+3` | Focus composer |
| `Alt+H` / `Alt+L` | Previous / next chat folder tab |
| `Alt+J` / `Alt+K` | Next / previous chat in the list |
| `i` | Start composing (from chat view) |
| any printable key | Quick-type: jump to the composer and start typing (chat list / chat view, once a chat is open) |
| `j` / `k` (or `↓` / `↑`) | Scroll down/up |
| `g` / `G` (or Home / End) | Jump to oldest/newest loaded message |
| `Ctrl+U` / `Ctrl+D` | Page up/down (messages panel) |

### Actions

| Key | Action |
|-----|--------|
| `Enter` | Select chat / send message / play or open the focused message's media |
| `o` | Open/play media |
| `s` | Save/download file |
| `/` | Search |
| `Alt+C` | Toggle contacts |
| `r` | Reply to message |
| `e` | Edit own message |
| `d` | Delete message |
| `Ctrl+V` | Paste an image/file from the clipboard and attach it (any panel, while a chat is open) |
| `Ctrl+Q` / `Ctrl+C` | Quit |

### Composer

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Esc` | Cancel reply/edit or discard a pending attachment first, then leave the composer |
| `Ctrl+W` | Delete word |
| `Ctrl+U` | Clear line before cursor |
| `Ctrl+K` | Clear line after cursor |
| `Ctrl+T` | Attach a file by path |
| `Ctrl+V` | Paste an image from the clipboard and attach it |

### Configuring keys

`[keys]` in `config.toml` overrides a subset of bindings; case-insensitive,
with `"escape"` accepted as an alias for `"esc"`. Only some fields are
actually consulted:

| Field | Default | Wired? |
|-------|---------|--------|
| `quit` | `ctrl+c` | yes |
| `focus_chat_list` | `f1` | yes — *in addition to* the hardcoded `Alt+1` |
| `focus_chat_view` | `f2` | yes — *in addition to* the hardcoded `Alt+2` |
| `focus_composer` | `f3` | yes — *in addition to* the hardcoded `Alt+3` |
| `search` | `/` | yes |
| `contacts` | `alt+c` | yes |
| `next_folder` | `alt+l` | yes |
| `prev_folder` | `alt+h` | yes |
| `next_chat` | `alt+j` | yes |
| `prev_chat` | `alt+k` | yes |
| `reply`, `edit_message`, `delete_message`, `forward`, `scroll_up`, `scroll_down`, `page_up`, `page_down` | — | no — parsed and saved so old config files round-trip, but not consulted anywhere |

**Warning:** wired bindings are checked before quick-type's composer
fallthrough, so binding a bare single printable character (e.g.
`quit = "q"` or `next_folder = "l"`) shadows that character everywhere —
it becomes untypeable as message text whenever a chat is open. Prefer
modifier-based bindings (`alt+`, `ctrl+`, a function key).

> [`config.example.toml`](config.example.toml)'s `[keys]` block is kept in
> sync with these defaults; `internal/config/config.go`'s `defaultConfig()`
> remains the source of truth if the two ever drift.

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
custom folders, and even before the folder list has loaded. `Alt+H` /
`Alt+L` cycle tabs left/right, wrapping around.

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
