<p align="center">
  <img src="https://upload.wikimedia.org/wikipedia/commons/8/82/Telegram_logo.svg" width="80" />
</p>

<h1 align="center">Telegram CLI</h1>

<p align="center">
  <strong>A full-featured Telegram client for the terminal</strong>
</p>

<p align="center">
  <a href="https://github.com/Ceesaxp/telegram-cli/actions"><img src="https://github.com/Ceesaxp/telegram-cli/actions/workflows/ci.yml/badge.svg" alt="Build"></a>
  <a href="https://github.com/Ceesaxp/telegram-cli/releases"><img src="https://img.shields.io/github/v/release/Ceesaxp/telegram-cli?include_prereleases" alt="Release"></a>
  <a href="https://github.com/Ceesaxp/telegram-cli/blob/main/LICENSE"><img src="https://img.shields.io/github/license/Ceesaxp/telegram-cli" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go">
</p>

---

> **This is a fork.** `telegram-cli` began as
> [imtaqin/telegram-cli](https://github.com/imtaqin/telegram-cli) — excellent
> work, and the foundation everything here is built on. It has since diverged
> substantially: the interface was rebuilt around a terminal-native design
> ([TUI 2.0](docs/tui-2.0.md)), and the client gained update-sequence
> persistence, chat folders, reactions, pinning, channel discussions, a
> context rail, per-chat drafts and a good deal more. Upstream is not
> responsible for anything here.

A Telegram client that behaves like a terminal program: vi motions, no mouse
required, no bubbles, and a frame whose every row is exactly the width of
your terminal. It speaks MTProto directly through [gotd/td](https://github.com/gotd/td)
— no bot API, no bridge, no Electron.

```
 tg │ 1:all 2:unread 3:work 4:channels 5:archive ● connected · 1 device │ 21:04
 / filter chats…          9/9 │ # infra-oncall │ group ·… buf 1 │ ln 45/45  bot
▌# infra-oncall         2m    │   20:47     nadia  That is the migration
▌  nadia: rebased, CI gr… [4] │                    backfill, not the rollout.
 @ Nadia Feld           6m    │                    It drains in ~20 min.
   you: pushing the tag now   │   20:52       you  Confirmed from the queue
 # relay-protocol       14m   │                    dashboard. Resuming.  ✓✓
   ivo: the 429 is upstr… [2] │ 4 NEW ─────────────────────────────────────────
 ~ wire notes           1h    │   20:58       ivo  Resumed. Canary at 5%.
   draft: saved locally       │   21:01     nadia  ↳ ivo Resumed. Canary at 5%.
 ! ops-alerts muted     2h    │                    Rebased onto main, CI is
   p95 back under 400ms  (31) │                    green now. 4412 ready for
 @ Mira Okonkwo         4h    │                    the second approval.
   sounds good — thurs then   │   21:02       sam  Approved. Merging behind the
 # design-crit          yd    │                    flag.
   you: left comments on 3    │                    [🚀 4]
 ! tape/changelog muted yd    │ ▌ 21:03       ivo  Good. I will write the
   v0.4.1 — keymap overhaul   │ ▌                  incident note either way —
 @ Jonas Vik            2d    │ ▌                  cheap to have, expensive to
   thanks, that unblocked me  │ ▌                  reconstruct.
                              │               ···  nadia is typing…
                              │ reply ↳ nadia: Rebased onto main, … esc to drop
 j/k move  l open  / filter   │ NORMAL › i to compose · : for commands       md
 j/k move  l open  / filter  [/] folder     idx 12 msgs · 9 buffers · 37 unread
```

That is not a mock-up. It is [`docs/fixtures/frame-80x24.txt`](docs/fixtures/frame-80x24.txt)
verbatim — one of six frames the renderer is asserted against **cell for
cell**, so the picture cannot drift from the program.

## Documentation

| | |
|---|---|
| [Features](docs/features.md) | what it does — folders, reactions, media, drafts, markdown, notifications |
| [Keybindings](docs/keys.md) | every key, both editing modes, and how to rebind them |
| [Interaction model](docs/interaction-model.md) | the rules the keyboard follows, and the decisions behind them |
| [Configuration](docs/configuration.md) | `config.toml`, where files go, migration |
| [MCP & REST](docs/integrations.md) | driving the account from another program |
| [Troubleshooting](docs/troubleshooting.md) | when something does not work |
| [Architecture](docs/architecture.md) | how the code is laid out, and the rules it follows |
| [TUI 2.0](docs/tui-2.0.md) | the design record: decisions, divergences, verification |

`config.example.toml` documents every setting with its default.

## Quick start

### Prebuilt binaries

Download the latest release for your platform from [Releases](https://github.com/Ceesaxp/telegram-cli/releases) — Linux, macOS, Windows, and Android/Termux (arm64). Each archive contains all three binaries (`tele-tui`, `telegram-mcp`, `telegram-api`) plus this README, the `docs/` directory, the LICENSE, and `config.example.toml`, which documents every setting. Verify what you downloaded against the release's `checksums.txt`.

Ask a binary what it is with `tele-tui -version` (or `version`, or `--version` — and the same on the other two):

```
tele-tui v0.4.2 (a1b2c3d, go1.25, darwin/arm64)
```

Releases are fully automatic: every push to `main` bumps the patch version, tags, builds, and publishes (use `#minor` / `#major` in a commit message to bump those instead).

### With the Go toolchain

```bash
go install github.com/Ceesaxp/telegram-cli/cmd/teletui@latest
```

The binary lands in `$GOPATH/bin` as `teletui` rather than `tele-tui` — `go
install` names it after its directory. The release archives and `make build`
both call it `tele-tui`.

### Build from source

```bash
# Clone
git clone https://github.com/Ceesaxp/telegram-cli.git
cd telegram-cli

# Build & run — first run prompts for API credentials
make run
```

Pure Go, no CGO, no native dependencies — a plain `go build` works everywhere.

To build the release archive itself:

```bash
make dist                          # this machine
make dist GOOS=linux GOARCH=arm64  # somewhere else
make dist-all                      # every platform the release publishes
make checksums                     # checksums.txt over whatever is in dist/
```

`make dist` is what the release workflow runs, rather than a second recipe that would have to be kept in step with it — so what you can build and open locally is the artifact people download. It stamps the version from `git describe`, which `make version` will print; override with `make dist VERSION=v1.0.0-rc1`.

### Prerequisites

- **Go 1.25+**
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

## Make targets

```bash
make build    # compile binaries → bin/tele-tui + bin/telegram-mcp (CGO_ENABLED=0)
make run      # build + run
make test     # run tests
make clean    # remove build artifacts
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/awesome`)
3. Commit your changes
4. Push to the branch
5. Open a Pull Request

`go test ./...` must pass. [docs/architecture.md](docs/architecture.md) has
the handful of rules worth knowing first — chiefly that all terminal geometry
goes through `internal/ui/cell`, and that the keymap tables in
[docs/keys.md](docs/keys.md) are checked against the running app in both
directions.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Credits

- [imtaqin/telegram-cli](https://github.com/imtaqin/telegram-cli) — the project this forked from
- [gotd/td](https://github.com/gotd/td) — pure Go Telegram (MTProto) client
- [Bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) — terminal styling
- [uniseg](https://github.com/rivo/uniseg) — grapheme clustering and display width
