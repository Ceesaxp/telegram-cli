# MCP server and REST API

Two ways to drive this account from another program. Both ship in the
release archive beside the TUI as `telegram-mcp` and `telegram-api`, and
neither is needed to use `tele-tui` itself.

They share one Telegram session with the TUI but never its update stream:
only the full client subscribes to updates, so the two servers stay
in-memory and never contend for the `state.db` lock (see
[architecture.md](architecture.md)).

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
| `send_file` | Upload a local file as a document (optional caption). Only paths inside the [send roots](configuration.md#send-roots-send_dirs) are accepted |
| `forward_messages` | Forward messages between chats using Telegram's own forwarding, keeping attribution and captions |
| `edit_message` | Edit a message text |
| `mark_read` | Mark messages as read |
| `download_media` | Download message media, returns local path |

### Which files can be sent

Both servers accept a `path` only if it resolves inside one of their **send
roots**: the media cache (`files_dir`) plus `[storage] send_dirs`, which
defaults to `~/.local/share/tele-tui/outbox`. The effective set is logged at
startup, and anything else is rejected with an error naming the roots.

This is deliberately narrow for MCP: the caller there is a model reading
messages from strangers, and a message asking it to send a private key should
fail rather than depend on where you started the server. See
[Send roots](configuration.md#send-roots-send_dirs).

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
| POST | `/api/send-file` | Send file `{chat_id, path, caption?, reply_to_message_id?}`. `path` must be inside the [send roots](configuration.md#send-roots-send_dirs) |
| POST | `/api/forward` | Forward messages `{from_chat_id, to_chat_id, message_ids[]}` — both chats named explicitly; there is no "current chat" |
| POST | `/api/edit` | Edit message `{chat_id, message_id, text}` |
| POST | `/api/mark-read` | Mark read `{chat_id, message_ids[]}` |
| GET | `/api/media?chat_id=&message_id=` | Download message media, returns local path |

Errors are JSON (`{"error": "..."}`) with status 400 (bad params), 401
(missing/invalid bearer token), 403 (forbidden Host/Origin — body includes
`allowed_hosts`), 404 (unknown route/chat), 415 (POST without
`Content-Type: application/json`), or 502 (upstream Telegram error).
