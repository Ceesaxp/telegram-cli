<p align="center">
  <img src="https://upload.wikimedia.org/wikipedia/commons/8/82/Telegram_logo.svg" width="80" />
</p>

<h1 align="center">Telegram CLI</h1>

<p align="center">
  <strong>A full-featured Telegram client for the terminal</strong>
</p>

<p align="center">
  <a href="https://github.com/tegal1337/telegram-cli/actions"><img src="https://github.com/tegal1337/telegram-cli/actions/workflows/build.yml/badge.svg" alt="Build"></a>
  <a href="https://github.com/tegal1337/telegram-cli/releases"><img src="https://img.shields.io/github/v/release/tegal1337/telegram-cli?include_prereleases" alt="Release"></a>
  <a href="https://github.com/tegal1337/telegram-cli/blob/main/LICENSE"><img src="https://img.shields.io/github/license/tegal1337/telegram-cli" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white" alt="Go">
</p>

---

## Features

- **Chat Management** — Private chats, groups, supergroups, channels
- **Message Bubbles** — Rounded bordered bubbles, own messages right-aligned, read status indicators
- **Profile Avatars** — Colored initials or rendered profile photos in chat list
- **Markdown Rendering** — Code blocks, bold, italic, links via [Glamour](https://github.com/charmbracelet/glamour)
- **Image Rendering** — Kitty graphics protocol, Sixel, Unicode half-block fallback with CatmullRom scaling
- **Voice/Audio Playback** — Play voice messages and audio inline via `mpv` / `ffplay`
- **Video** — Open videos in external player (`mpv` / `vlc` / `xdg-open`)
- **File Transfer** — Download with `s`, open with `Enter`, progress bar during sync
- **Search** — Search chats, messages, and global Telegram directory
- **Contacts** — Contact list with online status indicators
- **Group Info** — Member list, admin roles, group description
- **Authentication** — Phone/SMS code, 2FA password (QR code login not yet implemented)
- **First-Run Wizard** — Prompts for API credentials and saves config automatically
- **Notifications** — Desktop notifications via `notify-send` / `osascript`
- **Responsive Layout** — Dual-panel (wide) or single-panel (narrow terminals)
- **Theming** — Dark and light themes with 256-color support

## Screenshot

```
╭─ Chat List ─────────────╮╭─ Messages ──────────────────────────────────╮
│ DA  Dadang Jordan  08:15 ││                                             │
│     tes lim              ││                      ╭─────────────────────╮ │
│ SK  SKY API        13:24 ││                      │ naon we             │ │
│     sudah aman        2  ││                      │ 15:20 ✓✓            │ │
│ TG  Telegram       08:03 ││                      ╰─────────────────────╯ │
│     Login code: 90969... ││ ╭──────────────────╮                        │
│ AP  Api MX         14:38 ││ │ Dadang Jordan    │                        │
│     okesiap koo      81  ││ │ tah              │                        │
│                          ││ │ 15:22            │                        │
│                          ││ ╰──────────────────╯                        │
╰──────────────────────────╯╰─────────────────────────────────────────────╯
╭─ Compose ───────────────────────────────────────────────────────────────╮
│ █                                                                       │
│ Enter: send | Esc: cancel                                               │
╰─────────────────────────────────────────────────────────────────────────╯
● Connected  IMTAQIN    Tab:switch │ Esc:back │ /:search │ Alt+C:contacts
```

## Quick Start

```bash
# Clone
git clone https://github.com/tegal1337/telegram-cli.git
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

### Navigation

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Cycle between panels |
| `Esc` | Go back / close overlay |
| `F1` / `Alt+1` | Focus chat list |
| `F2` / `Alt+2` | Focus messages |
| `F3` / `Alt+3` | Focus composer |
| `i` | Start composing (from chat view) |
| `j` / `k` | Scroll up/down |
| `g` / `G` | Jump to top/bottom |
| `PgUp` / `PgDn` | Page scroll |

### Actions

| Key | Action |
|-----|--------|
| `Enter` | Select chat / Send message / Play media |
| `o` | Open/play media |
| `s` | Save/download file |
| `/` | Search |
| `Alt+C` | Toggle contacts |
| `r` | Reply to message |
| `e` | Edit own message |
| `d` | Delete message |
| `Ctrl+Q` / `Ctrl+C` | Quit |

### Composer

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Esc` | Cancel reply/edit, or leave composer |
| `Ctrl+W` | Delete word |
| `Ctrl+U` | Clear line before cursor |
| `Ctrl+K` | Clear line after cursor |

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
bin/telegram-mcp login   # phone → code → 2FA, writes ~/.local/share/tele-tui/session-mcp.json
```

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
| `edit_message` | Edit a message text |
| `mark_read` | Mark messages as read |
| `download_media` | Download message media, returns local path |

### Sessions

`telegram-mcp` uses its own session file (`session-mcp.json`) so the TUI and any number of MCP server processes each get their own Telegram connection with full realtime updates — like running Telegram on multiple devices. Set `TELETUI_SESSION=/path/to/session.json` to override the session path if you ever need to share one explicitly.

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
