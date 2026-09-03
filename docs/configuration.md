# Configuration

`config.toml` lives in `~/.config/tele-tui/` (or `$XDG_CONFIG_HOME`), and
`config.example.toml` in the repository documents every setting with its
default and an example — it is the reference, and this page is the
explanation of the parts that need one.

## The config file

Config is stored at `~/.config/tele-tui/config.toml`. A first run writes one
for you. [`config.example.toml`](../config.example.toml) carries every
option with its default; this is the shape of it:

```toml
[telegram]
api_id = 12345678
api_hash = "your_api_hash"

[ui]
theme = "dark"           # "dark" or "light"
inline_images = "on_open" # "never", "on_open", "always"
rail = false             # open the context rail by default (` toggles it)

[media]
image_protocol = "auto"  # "auto", "kitty", "sixel", "blocks"
voice_player = "mpv"     # "mpv", "ffplay"
video_player = "mpv"     # "mpv", "vlc", "xdg-open"
```

## Where files go

Two directories under `[storage]`, and they are not the same thing:

| Setting | Default | What it holds |
|---|---|---|
| `files_dir` | `~/.local/share/tele-tui/files` | The media **cache**. Downloads land here named by their Telegram file id, so a photo drawn twice is fetched once — including across restarts, since the name is derived from the id rather than remembered. Nothing here is meant to be found by hand. |
| `download_dir` | `~/Downloads` | Where `s` **saves**: a copy under the sender's own filename, in the folder you'd look in. |

Deleting the cache is always safe: anything missing is fetched again. A file
whose size disagrees with what Telegram said is treated as missing, and a
transfer that dies leaves nothing behind — the download goes to a temporary
name beside the destination and is renamed into place only once it is
complete.

`s` downloads into the cache (if it isn't there already) and then copies out
of it. The copy:

- **never overwrites** — a second `photo.jpg` is saved as `photo (2).jpg`,
  with the suffix before the extension, so it's still a `.jpg`;
- **stays in the directory** — the filename comes off the wire, so it's
  reduced to its last path element before use: a document called
  `../../.ssh/authorized_keys` is saved as `authorized_keys`, and a name that
  reduces to nothing becomes `telegram-file`;
- **leaves nothing behind if it fails** — no half-written file in Downloads
  that looks like it worked.

If `download_dir` is unset, `s` says so rather than guessing. Running
`-migrate-config` fills it in with the `~/Downloads` literal.

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

## Emoji width (`ui.emoji_width`)

If there's a gap of a few cells between the folder tabs and the clock, this
is the setting. Set `ui.emoji_width = "separate"` and it closes.

Emoji width is not a property of the string — the terminal decides it, and
terminals disagree. Three kinds of sequence carry a *composition rule*, and a
terminal that applies it and one that doesn't draw different widths:

| Sequence | Tables say | Composed | Not composed |
|---|---|---|---|
| `❤️` — a narrow base plus U+FE0F | 2 | 2 | **1** (the selector is ignored, the text heart is drawn) |
| `👨‍👩‍👧` — three emoji joined by U+200D | 2 | 2 | **6** (all three are drawn) |
| `🇷🇸` — a regional-indicator pair | 2 | 2 | **4** (two letter-boxes) |
| `👍🏻` — an emoji plus a skin tone | 2 | 2 | **4** (the swatch is drawn beside it) |

So "narrow or wide" is the wrong question: the same terminal is narrower than
the tables on the first row and wider on the other two. The question is
whether it **composes**, which is what the values name:

| `emoji_width` | Meaning |
|---|---|
| `"auto"` (default) | Don't assume. Measure with the tables, and where a row is being laid out against a budget, keep room for whichever rendering is wider. Never overflows; may leave a gap. |
| `"composed"` | This terminal applies every rule. |
| `"separate"` | This terminal applies none of them. |

It is a declaration because it cannot be detected: no environment variable
reports it, and the runtime query that would ask was removed for leaking its
response bytes into the composer. There's no harm in trying the other value —
set it, look at the top bar, keep whichever ends flush against the clock.

The setting is process-wide and read once at startup, like the colour
profile, so every panel that measures a string agrees about it.

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
  `ui.compose_editing` (`"auto"`), `storage.download_dir` (`~/Downloads`,
  where `s` saves — see [Where files go](configuration.md#where-files-go)), and
  `notifications.method` (`"auto"`, see [Notifications](features.md#notifications)),
  and `storage.state_file` — written out explicitly as the path the client
  would otherwise derive implicitly (next to `session_file`), so the
  location stops being implied. Also
  `ui.parse_markdown`, but as a special case: it's set to `true` on
  migration and the change is reported, on the reasoning that an existing
  user already has a working setup and the feature is worth having —
  brand-new configs still default to `false` (see Outgoing Markdown below).
  TUI 2.0 adds `ui.inline_images` (`"on_open"`) and `ui.rail` (`false`).
- **Names the fields this version removed**, as `field   old -> (removed)`.
  `ui.chat_list_width` and `ui.show_avatars` are gone: the chat list is a
  fixed 38 cells because the grid inside it is measured in display cells,
  and avatars are an explicit TUI 2.0 non-goal — the type sigil replaced
  them. Reported as removals rather than as unrecognized keys, because
  they are different news: an unrecognized key reads as a typo you should
  fix, a removed one is a setting that used to work. The old values are in
  the backup.
- **Reports what it did**, field by field, as `field   old -> new`; any
  config *section* your file didn't have at all, now added in full with
  defaults; any key your file had that this version no longer recognizes
  (dropped by the rewrite, but preserved in the backup); and any `[keys]`
  bindings that now collide with each other — checked only among the
  fields `internal/app` actually dispatches on, so a value that happens to
  match a hardcoded binding (`i`, the composer's readline chords, …)
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
- **Keeps paths portable.** `session_file`/`files_dir`/`download_dir`/`state_file` are
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
