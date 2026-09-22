# Theme configuration — spec

Status: **Phase 1 implemented** (2026-09-21, issue #74); **Phase 2
implemented** (2026-09-22): `:theme` and `:reload-config`. Revised
2026-09-06 after the architectural review in
`docs/theming-review.md` — the review's findings (G1–G12, Q3) are folded in
below, and the spec stays normative for what was built: where the code and
this page disagree, one of them is a bug. "As built" at the end says where
each part lives. Companion to the palette record in `docs/tui-2.0.md`. This
document decides the question the old `TODO.md` design note left open:
whether a theme is a name compiled in or a TOML file a reader writes.

Example themes to copy into place: [`docs/themes/`](themes/).

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

## What existed before Phase 1 (summary)

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

Resolution order (the value is trimmed first, and lowercased unless rule 2
makes it a path — a path keeps its case, because on most filesystems case
is part of a file's name; an empty value means `dark` **without** a
warning — the `ResolveComposeEditing` precedent is explicit that an empty
value is an older config, not a mistake):

1. `"dark"` or `"light"` → the compiled-in palette. Unchanged behaviour.
2. A value containing a path separator or ending in `.toml` → treated as a
   path (after `expandPath`, so `~` works). Absolute or, like **every other
   path in `config.toml`**, resolved by the OS relative to the working
   directory — a second relative-path convention in the same file is a trap
   for anyone reading it top to bottom. "Next to my config" is what rule 3
   is for.
3. A plain name → `themes/<name>.toml`, searched in two directories in
   order: the directory of the loaded config file, then the default config
   directory (`$XDG_CONFIG_HOME/tele-tui`). The second entry makes a shared
   theme collection the default for multi-profile users
   (`TELETUI_CONFIG=~/work.toml` would otherwise put the themes dir at
   `~/themes/`). So `theme = "gruvbox"` normally reads
   `~/.config/tele-tui/themes/gruvbox.toml`. The name is lowercased, so on
   a case-sensitive filesystem the file must be named in lower case:
   `theme = "Gruvbox"` reads `themes/gruvbox.toml`, never `Gruvbox.toml`.

   A plain name is a whitelist, because it is spliced into a path: letters
   (any script), digits, `-`, `_` and `.`, not starting with `.`. Anything
   else that is not path-shaped — `..`, `~`, a space, a `:` — is not a
   theme name: it is never searched for, so nothing outside `themes/` is
   reachable through one, and it warns (`ui.theme ".." is not a theme name
   — a name is letters, digits, "-", "_" and ".", and a file is named by a
   path or a .toml suffix`) and falls back to `dark`.
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
name    = "gruvbox"        # optional, and ignored: nothing reads it. A label
                           # for whoever reads the file.
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

**Decode shape (normative, not an implementation detail):** the whole
document decodes as `map[string]any`, and every section and value is
checked by hand afterwards. In `[colors]` and `[colors256]` a string is
taken as-is, an integer is stringified, and anything else warns and
inherits. Any typed shape hands the checking to go-toml, which fails the
**whole file** over one mismatch — `bg = 235` (an xterm index written the
way everyone writes xterm indices) into a `map[string]string`,
`senders = ["mauve"]` into a struct — and says so in Go's type names,
which is exactly the degradation promise this spec makes broken at its
first contact with a user. Section names, and the keys of `[theme]` and
`[senders]`, fold to lower case like role keys: `[Colors]` is `[colors]`.
Duplicate keys and duplicate tables are hard TOML parse errors and
correctly fall under "fails parsing entirely" below; the warning carries
the parser's position when it has one. A syntax error has a line and
column; a duplicate key or table is not a `*toml.DecodeError` and carries
neither — its warning names the key or table instead.

### Validation

Strict where cheap, forgiving where it matters:

- Unknown key in `[colors]`/`[colors256]` → warning, key ignored. Not
  fatal: a theme written against a newer build should degrade, not brick
  the older one. Role names are the theme half's to know, so this warning
  (like every role, value and ramp warning below) comes from
  `theme.CheckSpec`, which `main` prints in the same loop as
  `StartupWarnings`.
- Malformed colour value → warning, that role inherits from the base.
  **Validation is strict and happens at load**, because lipgloss renders
  an invalid colour as *no colour at all* — zero escape bytes, which for
  `bg`/`panel` means unpainted surfaces, the failure class divergence 19
  exists to prevent. Accepted: `#rrggbb` and `#rgb` in `[colors]`; the
  decimal range 0–255 in `[colors256]` (termenv emits an out-of-range
  index verbatim, so the range check is not optional).
- Two keys in one table that fold to the same role (`Cyan` and `cyan`) →
  warning, and the first in sorted order is used — every run, whatever
  order the map decoded in.
- A section that is not a table (`senders = ["mauve"]`, `colors = 5`,
  `[[colors]]`) → warning in plain words (`senders should be a table, like
  [senders]`), that section ignored; the rest of the file still applies.
- One section written twice in two cases (`[colors]` and `[Colors]`) →
  warning, and the first in sorted order is used, as for role keys.
- A top-level key that is not a section → warning, ignored. `inherit`,
  `name` and `ramp` there say which section they belong under; nothing is
  guessed into one.
- Unknown `inherit` value → warning, `dark`.
- `[senders].ramp` naming an unknown role → warning, default ramp — the
  whole ramp, since a shorter one moves everybody's colour anyway. An
  entry that is not a string (`5`) is passed on as its text and fails the
  same way.
- Empty `ramp` → warning, default ramp. (One-element ramps are legal:
  uniform sender colour is a defensible taste.)
- A theme file that fails TOML parsing entirely → fall back to `dark`,
  one warning.

All failures degrade toward the builtin dark palette; the app never
refuses to start over a theme.

Theme files are made to be shared, so what one says is somebody else's
text, and warnings quote it. Keys are named as TOML writes them — bare when
they can be, quoted otherwise (`colors."\x1b]0;…"`) — and `main` prints
every startup warning through a filter that replaces C0 and C1 control
characters, DEL and non-UTF-8 bytes with U+FFFD, so no file can write an
escape sequence to the terminal.

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

**Phase 1 — startup only. Implemented.** `config.Load()` → `LoadTheme` →
`theme.RolesForSpec`, called once in `app.New` in place of `RolesFor`.
Components receive `Roles` at construction exactly as before. This phase is
the whole spec's must-have; it already delivers "a TOML file a reader
writes".

**Phase 2 — `theme <name>` + `reload-config`, together. Implemented.** The
two share their wiring, as the TODO said they would:

- **Every component has a `SetRoles`**, and each re-derives inside it
  whatever it built from the old palette: widget styles (a text input's, a
  spinner's, a list's), bound row renderers, the notice already on the hint
  bar, and the thread grid's cache of rendered lines, which is dropped. The
  five components that used to bake styles in their constructors — chatlist,
  auth, composer (textarea included), contacts and search — now build them
  in a `restyle` their constructor and `SetRoles` both call. A palette
  applied at runtime is the uniform-setter option, not a rebuilt component
  tree: nothing is torn down, so no state (a draft, a scroll position, a
  half-typed search) is at risk.
- **`applyRoles` is the one path** (`internal/app/retheme.go`). It sets the
  app's own copy of the roles, calls every component's `SetRoles`, tells an
  open dialog, and hands the sender ramp to the thread and the rail. `New`
  calls it once the components exist, and a switch calls it again, so
  startup and a switch cannot reach different things. `applyThemeSpec`
  resolves a spec through `RolesForSpec` at the colour depth `New` decided
  — the environment is not consulted again, and the terminal never is —
  and returns the palette half's warnings.
- **The guard test** `TestEveryComponentFollowsARetheme`
  (`internal/app/retheme_test.go`) draws every surface in one marker
  palette, switches to a second, and draws it again: a surface with no
  setter, a setter that leaves derived styles behind, or a cache serving old
  lines all show up as a colour from the first. Adding a surface to the app
  means adding it there.

Decisions made in Phase 2:

- **`:theme` applies on Enter only.** There is no live preview while the
  candidates are browsed. `:theme ` lists the builtins and every
  `themes/*.toml` in the two search directories (`config.ListThemes`, over
  the directories `config.Load` recorded — `Config.ThemeDirs`), opening on
  the theme in use, marked "current", so Enter straight away reports
  "unchanged" and writes nothing. `:theme` alone says which theme is on.
- **A name that names nothing usable is not applied.** At startup the
  loader falls back to `dark`, because there is nothing else to draw; at
  runtime the theme on screen is working, so `:theme` and `:reload-config`
  keep it and put the loader's reason in the notice. A theme that loads with
  warnings is applied, and the notice counts them — the file half's and the
  palette half's together — and quotes the first. This is also where theme
  warnings reach the hint bar, which the out-of-scope list below asked for.
- **`:theme` persists by editing one line** (`config.SetThemeLine`), never
  through `config.Save`, which re-encodes the whole file and drops its
  comments. The value of the `theme` key in `[ui]` is replaced in place —
  found with go-toml's parser, so a `[ui]` inside a multi-line string is not
  a table — keeping indentation, the key's spelling, the spacing around `=`
  and a trailing comment; a `[ui]` without one gets a line under its header,
  a file without `[ui]` gets the table appended, and a missing file is
  created at 0600. CRLF and a missing final newline are kept. The file is
  backed up first with `BackupFile`, written atomically through a symlink
  with its own mode, and read back with the real loader; if it does not
  load, or does not say the new value, the original is restored. It refuses,
  and says to edit by hand, when `ui` is dotted keys or an inline table or
  the file is not TOML: the theme still switches, and the notice says "not
  saved" and why.
- **`:reload-config` applies the theme and lists the rest.** It re-reads
  the file `config.Load` read (`Config.Reload`) and applies its theme
  through the path `:theme` takes, re-reading the theme file too, without
  saving — the file is the source. Every other setting that changed is named
  by its TOML key, found by walking the struct (`config.ChangedSettings`),
  as "restart to apply: …", and keeps its running value, so the app goes on
  behaving as one config rather than half of two. A file that does not load
  changes nothing. `docs/tui-2.0.md` decision 8 has reload-config confirm
  first when the composer holds a draft or an attachment; it does not,
  because nothing a reload applies touches the composer — the theme is
  pushed through `SetRoles`, not a rebuilt tree.

## Implementation notes

- **Where the loader lives — decided, not "either is fine":**
  `internal/config` resolves and reads (name → builtin | path, stat guard,
  TOML decode into a transport `ThemeSpec{Name, Inherit; Colors,
  Colors256; Ramp}`, warnings accumulated — no lipgloss import);
  `internal/ui/theme` converts (`RolesFrom(spec, base, trueColor)`, built
  as `RolesForSpec` — the snake_case reflection map, validation,
  quantisation, ramp resolution).
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
  channel. It carries the config half's warnings (resolution, reading,
  decoding); the role, value and ramp warnings come from
  `theme.CheckSpec`, which `main` prints in the same loop.
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
  **Done in Phase 1**, the cheap route: `SenderColourFrom` plus
  `SetSenderRamp` on the two components.

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
  worth considering. Phase 2 puts the warnings of a theme applied at
  runtime in the hint bar; the startup theme's still go to stderr only.
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
   escape hatch is the path form: a path to `themes/light.toml` reaches the
   file, because rule 1 matches the two exact strings only. The warning
   spells out the file's full path to set, since a relative
   `./themes/light.toml` is the working directory's, like every relative
   path in `config.toml`. Keep the shadow check cheap: stat for it only
   when the configured name *is* `dark`/`light`, and warn only about a file
   the stat found — a `themes` that is itself a file, or cannot be
   searched, shadows nothing.
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

## As built (Phase 1)

- **Config half** — `internal/config/theme.go`. `ResolveThemeName` is the
  pure name → builtin | path | stem resolution; `LoadTheme(value,
  configDir, defaultConfigDir)` stats, reads and decodes into a
  `ThemeSpec{Name, Source, Inherit, Colors, Colors256, Ramp}` and never
  fails. `config.Load` calls it and keeps the result on unexported fields,
  read through `Config.ThemeSpec()` (nil for a builtin) and
  `Config.ThemeBuiltin()` (the base); `StartupWarnings` appends its
  warnings and stays pure. `[theme].name` is not read.
- **Theme half** — `internal/ui/theme/load.go`.
  `RolesForSpec(spec, builtin, trueColor) (Roles, []lipgloss.Color,
  []string)` is the palette, the resolved sender ramp and the warnings;
  a nil spec is the builtin alone. `CheckSpec(spec, builtin)` is the same
  warnings without the palette. The snake_case key map is built by
  reflection over `Roles` and pinned by a test that fails if a field is
  ever anything but a `lipgloss.Color`.
- **Sender ramp** — `theme.SenderColourFrom(id, ramp, roles)` hashes into
  the resolved ramp with the unchanged FNV-1a hash (an empty ramp is the
  default one); `SetSenderRamp` on `chatview` and `rail`. `SenderColour`
  is still the default ramp and colours every user as before.
- **Wiring** — `app.New` calls `RolesForSpec(cfg.ThemeSpec(),
  cfg.ThemeBuiltin(), SupportsTrueColor())`, the single dispatch point, and
  hands the ramp to the two components; its signature is unchanged. `main`
  prints `config.StartupWarnings` and `theme.CheckSpec` in one loop, before
  `app.New`, each line through `printable`, which replaces control
  characters.
- **Examples** — nine themes in `docs/themes/`, each mapped from a vim
  colour scheme's own source (named in the file's header) by the same rules:
  Normal bg/fg for `bg`/`fg`, the sidebar, status line, visual and cursor-line
  backgrounds for `panel`, `chrome`, `sel` and `cur_line`, NonText → Comment →
  secondary fg for the text ramp, and the scheme's cyan, yellow, green,
  purple, blue and red for the accents. Gruvbox, Dracula and Everforest write
  `[colors256]` from their own cterm tables; the rest quantise. A test loads
  every file there through both halves and requires zero warnings, and at
  least one theme of each depth kind.

## As built (Phase 2)

- **Setters** — every component under `internal/ui/components` has a
  `SetRoles`; `internal/app/retheme.go` holds `applyRoles` (the one path)
  and `applyThemeSpec`; `internal/app/retheme_test.go` holds the guard.
- **Commands** — `internal/app/themecmd.go` (`:theme`, its candidates, and
  `applyTheme`, the resolve-and-apply step both commands share) and
  `internal/app/reloadcmd.go` (`:reload-config`); both are registered in
  `internal/app/commands.go`. The palette's `Arg.Current` marks the theme
  in use and opens the list on it.
- **Config** — `Config.ThemeDirs` and `Config.Path` (recorded by `Load`;
  a Config `Load` did not build answers with `ConfigPath` and the default
  directory), `Config.Reload` and `ChangedSettings`
  (`internal/config/reload.go`), and `SetThemeLine`
  (`internal/config/themeline.go`).
