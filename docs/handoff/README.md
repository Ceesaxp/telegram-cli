# Design handoff archive

Source-of-record for the supplied TUI 2.0 design handoff. **This directory is
read-only history.** The living specification is
[docs/tui-2.0.md](../tui-2.0.md); the acceptance artifact is
[docs/fixtures/](../fixtures/).

| File | SHA-256 | Kept here? |
| --- | --- | --- |
| `README.md` → `tui-redesign-handoff.md` | `64b5a1de84eea58245e5f5ce325926125a6936b08dea3b074e0c63ca332208a3` | yes, verbatim |
| `Telegram TUI.dc.html` | `1eba60a943ed94ae83c439241966d0803c295410a75cc3013ace29a6fb3e7038` | no — visual reference only |
| `support.js` | `8fe7df74405f3c55f49b7249c74ea1397e65d07dea2b1bd3b4a489bec2e28cbe` | no — support script for the HTML |

Received 2026-08-29. The bundle archive itself is recorded in
[docs/tui-2.0.md](../tui-2.0.md) with its own checksum.

The HTML reference and its support script are deliberately not vendored. They
are a prototype of intended look and behaviour, not code to port, and they are
not deterministic terminal output — that is what the fixtures are for. Consult
the HTML only for a visual detail the written spec left unstated.

## Read this copy as superseded

`tui-redesign-handoff.md` is kept byte-for-byte as received, which means it
still contains statements that later review and the fixtures overturned — the
`s` spoiler binding, the "presentation-layer only" scope claim, the rail's "no
new API calls", and an expanded-composer chord footer that collides with both
composer editing keymaps.

Do not implement from this file. Implement from
[docs/tui-2.0.md](../tui-2.0.md), whose
"Divergences from the handoff prose" section lists every such point and the
reasoning behind each.
