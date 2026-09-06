# Theme configuration — spec

Status: **proposal** (not implemented), revised 2026-09-06 after the
architectural review in `docs/theming-review.md` — the review's findings
(G1–G12, Q3) are folded in below. Companion to the design note at
`TODO.md` ("theme `<name>` — mostly wired") and the palette record in
`docs/tui-2.0.md`. This document decides the question that note left open:
whether a theme is a name compiled in or a TOML file a reader writes.

## Decision

**A theme is a TOML file a reader writes; the two compiled-in palettes
(`dark`, `light`) remain as builtins and as inheritance bases.**

Rationale: the hard part of theming — a semantic role vocabulary, a single
injection point, tests that keep colour literals out of components — is
already done. Compiling in more palettes makes every taste dispute a code
change and a release; a theme file makes it a text file in the reader's
config directory. The builtins stay because (a) zero-config must keep
working, (b) a user theme needs a complete, known-good base to inherit
from, and (c) the hand-picked 256-colour tables cannot be conjured from a
user's hex values (see "Colour depth" below).

## What exists today (summary)

- `internal/ui/theme/roles.go` — `Roles`, 19 semantic `lipgloss.Color`
  fields; parallel hex and 256 tables for dark; light is a mechanical
  inversion. `RolesFor(name, trueColor)` is the single dispatch point.
- `ui.theme` config key (string, default `"dark"`), consumed once at
  startup in `internal/app/app.go` (`app.New`). Any unrecognised value
  silently yields dark — no warning.
- Guard tests: `TestNoColourLiteralsOutsideThePalette` (AST scan of
  `internal/ui`), `MarkerRoles()` + `TestEveryComponentUsesThePaletteItWasGiven`
  (reflective, auto-covers new fields). Golden frame fixtures are
  cell-exact but geometry-only; colour changes do not disturb them.
- Hard rule: **never query the terminal at runtime** — colour depth comes
  from `$COLORTERM`/`$TERM` only (`SupportsTrueColor`, and the OSC-reply
  war story documented above it).

## Config surface

`ui.theme` keeps its name and stays a plain string:

```toml
[ui]
theme = "gruvbox"        # builtin name, theme-file stem, or explicit path
```

Resolution order (the value is trimmed and lowercased first; an empty
value means `dark` **without** a warning — the `ResolveComposeEditing`
precedent is explicit that an empty value is an older config, not a
mistake):

1. `"dark"` or `"light"` → the compiled-in palette. Unchanged behaviour.
2. A value containing a path separator or ending in `.toml` → treated as a
   path (after `expandPath`, so `~` works). Absolute or, like **every other
   path in `config.toml`**, resolved by the OS relative to the working
   directory — a second relative-path convention in the same file is a trap
   for anyone reading it top to bottom. "Next to my config" is what rule 3
   is for.
3. Anything else → `themes/<name>.toml`, searched in two directories in
   order: the directory of the loaded config file, then the default config
   directory (`$XDG_CONFIG_HOME/tele-tui`). The second entry makes a shared
   theme collection the default for multi-profile users
   (`TELETUI_CONFIG=~/work.toml` would otherwise put the themes dir at
   `~/themes/`). So `theme = "gruvbox"` normally reads
   `~/.config/tele-tui/themes/gruvbox.toml`.
4. Not found / unreadable / invalid → fall back to `dark` and emit a
   `StartupWarnings` entry naming the file and the reason. **This replaces
   today's silent-typo-means-dark behaviour for builtins too** (which is
   also case-sensitive today: `theme = "Light"` silently means dark): an
   unrecognised name that resolves to no file warns instead of passing
   quietly.

Note the resolver is deliberately **not** a member of the
`ResolveEmojiWidth`/`ResolveHyperlinks`/… family: those are pure
`string → string`, while this one needs the config directory, file I/O,
and produces warnings. See "Where the loader lives" for the seam that
keeps `StartupWarnings` pure regardless.

Before reading, the file is `os.Stat`'d: regular file only, 64 KiB size
cap, anything else warns and falls back — a fifo or `/dev/stdin` must not
block startup, and a directory must not produce a confusing read error. No
permission checks: a theme carries no secrets, unlike the session file.

No config migration is needed: existing values `"dark"`/`"light"` resolve
exactly as before.

## Theme file format

One file defines one theme. **All sections are optional** — a theme file
with no colours is a well-defined no-op: it *is* its base. (An earlier
draft required `[colors]`, but an empty table and an absent one decode
identically, so the rule was undetectable — and it contradicted "all
failures degrade". A theme defining no colours draws one warning.)

```toml
# ~/.config/tele-tui/themes/gruvbox.toml

[theme]
name    = "gruvbox"        # optional; display only. Defaults to file stem.
inherit = "dark"           # "dark" or "light" (only builtins may be
                           # inherited; no theme-file chains). Default "dark".

# Roles omitted here inherit from the base. Values are hex ("#rrggbb" or
# the "#rgb" shorthand, which lipgloss renders correctly).
[colors]
# Surfaces, darkest context first.
bg        = "#1d2021"      # app background
panel     = "#282828"      # side panels
chrome    = "#32302f"      # top bar, hint bar
sel       = "#3c3836"      # selected chat row
cur_line  = "#32302f"      # selected message row
rule      = "#504945"      # panel separators
rule_soft = "#3c3836"      # in-panel dividers
border    = "#665c54"      # attachment, code and poll frames

# Text ramp, dimmest to brightest.
ghost  = "#665c54"         # separators and inert glyphs
faint  = "#7c6f64"         # timestamps and byte counts
dim    = "#928374"         # secondary copy
fg     = "#ebdbb2"         # message body
bright = "#fbf1c7"         # active titles and bold spans

# Semantic accents.
cyan  = "#8ec07c"          # sole focus and key accent
amber = "#d79921"          # commands, attachments, channels, inline code
green = "#b8bb26"          # insert mode, online, own messages, sent
mauve = "#d3869b"          # groups, italics, sender colour
blue  = "#83a598"          # DMs, mentions, sender colour
red   = "#fb4934"          # errors, failures, removed diff lines

# Optional. Hand-picked xterm-256 fallbacks, same keys as [colors].
# Roles absent here are quantised mechanically from the hex (see below).
[colors256]
bg = "235"
fg = "223"

# Optional. The deterministic sender-name ramp (FNV-1a over user id).
# Entries are role names, resolved after [colors] is applied.
# Default: ["mauve", "cyan", "blue", "amber"] — today's compiled-in ramp.
[senders]
ramp = ["mauve", "cyan", "blue", "amber"]
```

Key names are the `Roles` field names in snake_case, folded to lower case
before lookup. The mapping is mechanical (`CurLine` → `cur_line`); a new
`Roles` field automatically becomes a new legal key. Reference table with
the roles' meanings: `docs/tui-2.0.md` palette section.

**Decode shape (normative, not an implementation detail):** `[colors]` and
`[colors256]` decode as `map[string]any`, with values coerced afterwards —
a string is taken as-is, an integer is stringified, anything else warns
and inherits. Decoding straight into `map[string]string` would make
`bg = 235` (an xterm index written the way everyone writes xterm indices)
a **whole-file** parse failure under go-toml v2, which is exactly the
degradation promise this spec makes broken at its first contact with a
user. Duplicate keys and duplicate tables are hard TOML parse errors and
correctly fall under "fails parsing entirely" below; the warning must
carry the parser's position, and note the duplicate-key error is not a
`*toml.DecodeError`, so the message cannot be recovered via `errors.As`.

### Validation

Strict where cheap, forgiving where it matters:

- Unknown key in `[colors]`/`[colors256]` → warning (named in
  `StartupWarnings`), key ignored. Not fatal: a theme written against a
  newer build should degrade, not brick the older one.
- Malformed colour value → warning, that role inherits from the base.
  **Validation is strict and happens at load**, because lipgloss renders
  an invalid colour as *no colour at all* — zero escape bytes, which for
  `bg`/`panel` means unpainted surfaces, the failure class divergence 19
  exists to prevent. Accepted: `#rrggbb` and `#rgb` in `[colors]`; the
  decimal range 0–255 in `[colors256]` (termenv emits an out-of-range
  index verbatim, so the range check is not optional).
- Unknown `inherit` value → warning, `dark`.
- `[senders].ramp` naming an unknown role → warning, default ramp.
- Empty `ramp` → warning, default ramp. (One-element ramps are legal:
  uniform sender colour is a defensible taste.)
- A theme file that fails TOML parsing entirely → fall back to `dark`,
  one warning.

All failures degrade toward the builtin dark palette; the app never
refuses to start over a theme.

## Colour depth

The two-column shape (hex + hand-picked 256) is preserved, not replaced
with `lipgloss.AdaptiveColor` — depth is still resolved once at startup
from the environment, per the no-runtime-queries rule.

For a user theme on a non-truecolour terminal:

1. Role present in `[colors256]` → use it as given.
2. Role absent there but present in `[colors]` → quantise the hex via
   `termenv.ANSI256.Convert` (termenv is already a direct dependency, and
   this is what lipgloss does at render time anyway — do **not** write a
   nearest-by-RGB quantiser by hand; the load-time conversion exists only
   so the result is deterministic and unit-testable without a rendering
   harness). Guard the conversion's `nil` return on junk input.
3. Role absent from both → the base palette's hand-picked 256 value.

This keeps the repo's stance intact — the *builtin* 256 tables remain
hand-picked, never generated — while sparing theme authors from writing
38 values when 19 will do. A theme author who cares about 256-colour
terminals writes `[colors256]`; one who doesn't gets a tolerable
approximation. The stance is testable: the mechanical conversion mostly
agrees with the hand-picked table but deliberately diverges in places
(`#c9ced4` → 188 mechanically, 252 by hand) — pin at least one such
divergence in a test so "hand-picked, not generated" is asserted, not
just asserted-about.

## Runtime behaviour

Phased, matching the `TODO.md` design notes:

**Phase 1 — startup only.** `config.Load()` → `ResolveTheme` →
`theme.RolesFor` grows a lookup through the theme file loader. Components
receive `Roles` at construction exactly as today. `theme <name>` and
`reload-config` remain unregistered. This phase is the whole spec's
must-have; it already delivers "a TOML file a reader writes".

**Phase 2 — `theme <name>` + `reload-config`, together** (the TODO is
explicit that they share their wiring). Requirements recorded there and in
`docs/tui-2.0.md`:

- Re-derive `Roles` and push through every component. Note: the TODO line
  claiming "thirteen components take `SetRoles`" is stale — most setters
  were deliberately removed; only composer, attach, reactionpicker, rail
  and the message renderer still have one. Phase 2 either restores a
  uniform `SetRoles` across components or rebuilds the component tree;
  that choice is Phase 2's to make, not this spec's.
- Invalidate the thread grid's cache of rendered (already-styled) lines —
  a stated prerequisite in `docs/tui-2.0.md`.
- `theme <name>` with no argument could cycle or list; suggestion: list
  available themes (builtins + `themes/*.toml` stems) in the palette.

Phase 1 must not paint itself into a corner for Phase 2, and doesn't: the
loader is pure (`name → Roles`), so calling it again later is free. But
the loader being free is not Phase 2 being free — five components
additionally **bake derived styles at construction** and have no
`SetRoles` to refresh them: chatlist (empty-state style and the spinner),
auth, composer (whose *existing* `SetRoles` does not refresh the textarea
styles set in its constructor — the one component that looks re-themable
is only half re-themable), contacts, and search (seven styles). Phase 2's
real cost is making those constructors' derived styles re-derivable, which
is the rebuild-the-component-tree option in disguise. Recorded here so
Phase 2 is scoped honestly; Phase 1 is unaffected.

## Implementation notes

- **Where the loader lives — decided, not "either is fine":**
  `internal/config` resolves and reads (name → builtin | path, stat guard,
  TOML decode into a transport `ThemeSpec{Name, Inherit; Colors,
  Colors256; Ramp}`, warnings accumulated — no lipgloss import);
  `internal/ui/theme` converts (`RolesFrom(spec, base, trueColor)` — the
  snake_case reflection map, validation, quantisation, ramp resolution).
  Only this split keeps `StartupWarnings` a pure function of `*Config`,
  which is its tested contract — and ordering forces it anyway: `main`
  prints warnings *before* `app.New` runs, so a theme resolved inside
  `app.New` would produce warnings after the print, into the void.
  Concretely: resolution happens during `config.Load()`, the result rides
  on unexported `Config` fields (`themeSpec`, `themeWarnings` — invisible
  to go-toml, so `Save` round-trips unchanged), `StartupWarnings` appends
  them and stays pure, and `app.New` keeps its signature. The loader takes
  the config directory as a **parameter** — reading `TELETUI_CONFIG`
  inside it would make every `app.New(cfg, …)` test environment-sensitive
  to whatever the developer has in `~/.config/tele-tui/themes/`.
- **Field mapping by reflection**, like `MarkerRoles()` — one place that
  walks `Roles` fields and derives snake_case keys, so a new role extends
  the file format, the marker test and the guard test with zero extra
  registration.
- **Guard tests are unaffected**: theme files contain colour literals but
  live outside the repo; the AST scan covers `internal/ui` sources only.
  Loader unit tests (inheritance, bad values, quantisation, ramp
  resolution) are new and cheap. Add one fixture theme under `testdata/`.
- **`StartupWarnings`** (`internal/config/config.go`) is the reporting
  channel; it exists and is currently keymap-only.
- **Sender ramp**: `SenderColour` currently hashes into a fixed
  `[Mauve, Cyan, Blue, Amber]`. **The ramp must never become a field on
  `Roles`**: `MarkerRoles` sets every field to a `lipgloss.Color` by
  reflection and a `[]lipgloss.Color` field panics it, taking
  `TestEveryComponentUsesThePaletteItWasGiven` with it — the all-same-type
  invariant is also what makes the snake_case mapping trivially safe, so
  write it down and keep it. (An earlier draft floated a
  `Theme{Roles, Ramp}` wrapper; that is the expensive option — it changes
  the type held by every component.) The cheap route: `SenderColour` has
  exactly two non-test call sites (chatview's grid and the rail), so a
  `SetSenderRamp([]lipgloss.Color)` on those two components, resolved at
  load time so the hot path stays allocation-free, is ~30 lines. Either do
  that in Phase 1 or cut `[senders]` from Phase 1 entirely and ship it
  with Phase 2 — both defensible; splitting the difference is not.

## Out of scope (recorded, not forgotten)

- **Splash screen colours** — `internal/app/app.go` still hardcodes `39`,
  `51`, `244` — and a fourth the first count missed, the
  `BorderForeground(lipgloss.Color("39"))` a screen further down; it
  renders after config load, so it *could* take roles. Separate small
  change; also worth extending the literal-scan to `internal/app` when
  done.
- **Theme warnings beyond scrollback** — warnings print to stderr before
  the program starts and the app does not use the alt screen, so they
  survive *above* the frame, but scroll away within seconds of use. For a
  cosmetic setting people will typo, a first-render hint-bar notice is
  worth considering in Phase 2, when the hint bar is in the model anyway.
- **Hand-tuned light palette** — light remains a mechanical inversion with
  a known `Ghost`/`Faint` legibility issue (`docs/tui-2.0.md`); a
  hand-tuned `light` is now just a theme file away, which is half the point
  of this spec.
- **Auto light/dark switching** — background detection requires querying
  the terminal (OSC 11), which is forbidden here for good documented
  reasons. Environment-only proxies are unreliable. Not doing it.
- **Theme-file chains** (`inherit = "someother.toml"`) — inheritance is
  builtins-only. Chains buy little and cost cycle detection.
- **Styling beyond colour** (borders, bold/italic mapping) — the role
  system deliberately encodes emphasis in components, not in the palette.
  A theme changes colours; it does not restyle the UI.

## Open questions — resolved (review of 2026-09-06)

1. `theme = "light"` shadowing — a user dropping `themes/light.toml` to
   override the builtin — is **not allowed**; builtin names win, with a
   warning if a shadowing file exists. Beyond predictability, two
   structural reasons: `inherit` resolves against builtins only, and a
   shadowable builtin would give `inherit = "light"` two meanings —
   quietly reintroducing the theme-file chains this spec rules out; and
   the builtins are the guaranteed-good floor of every degradation path
   above — a fallback a user file can redefine is not a fallback. The
   escape hatch is the path form: `theme = "./themes/light.toml"` reaches
   the file, because rule 1 matches the two exact strings only. Keep the
   shadow check cheap: stat for it only when the configured name *is*
   `dark`/`light`.
2. Example themes ship in-repo under **`docs/themes/`** — not `contrib/`,
   which implies a place third parties add to and therefore a review queue
   for taste, the exact thing this spec exists to avoid. Two or three
   (gruvbox and solarized cover most asks), documented as copy-into-place,
   under two conditions: a test loads every shipped theme through the
   loader and asserts zero warnings (or the examples rot into the best
   demonstration of a broken format), and the set includes one theme that
   writes `[colors256]` and one that omits it, so both depth paths have a
   real-world specimen.

Two implementer tripwires, recorded so they are hit on purpose:
`TestTheExampleInventsNoSettings` fails on any non-field `key =` line in
`config.example.toml`, comments included — theme examples cannot be
pasted there even commented-out; and the colour-literal guard matches
`lipgloss.Color("…")` call expressions in test files too, so loader tests
express expectations as plain strings (`string(got.Cyan) != "#8ec07c"`),
while TOML fixtures under `testdata/` are never scanned.
