# Review of docs/theming.md (issue #74)

Status: review of the **proposal**, 2026-09-06. Read-only architectural review
against the code at main (b697d8c); no implementation. Companion to
`docs/theming.md`.

## Verdict

**Phase 1 is implementable essentially as specced.** Every factual claim in
"What exists today" checks out (all 16 verified against file:line). One
architectural assumption fails contact with the config package — `ResolveTheme`
cannot join the `Resolve*` family (those are pure `string → string`; theming
needs a directory, file I/O, and warnings) — fixable with a different seam, not
a different design. Realistic size: ~700–1000 lines in this repo's style, 3 new
files, ~8 touched, one reviewable PR. No component changes, no `app.New`
signature change, no guard/marker changes.

Recommended layout:
- `internal/config/theme.go` — name→builtin|path resolution, stat guard, TOML
  decode into a transport `ThemeSpec`; warnings accumulated; no lipgloss import.
- `internal/ui/theme/load.go` — `RolesFrom(spec, base, trueColor)`: snake_case
  reflection map, hex validation, quantisation, ramp resolution. Owns colour.
- `internal/ui/theme/testdata/*.toml` — fixtures.

The spec's "either is fine" on loader placement is wrong: only this split keeps
`StartupWarnings` a pure function of `*Config` (its tested contract).

## Notable verification findings

- Silent-dark fallback is worse than stated: the builtin match is
  case-sensitive, so `theme = "Light"` silently gives dark today (roles.go:178).
- `TestEveryComponentUsesThePaletteItWasGiven` covers 5 of ~16 components by
  hand-written list; the name overstates it (palette_test.go:35-98).
- Splash has a **fourth** hardcoded literal the spec misses:
  `BorderForeground(lipgloss.Color("39"))` at app.go:2231.
- Warnings print to stderr before app start and the app does not use the alt
  screen, so they survive in scrollback — but scroll away in seconds.

## Gaps, ranked

- **G1 (high)** `ResolveTheme` seam: resolve during `config.Load()`, stash
  `themeSpec`/`themeWarnings` in unexported Config fields (invisible to go-toml,
  so Save round-trips); `StartupWarnings` appends them and stays pure. Loader
  takes the config dir as a parameter — reading env inside it would make ~19
  app tests environment-sensitive.
- **G2 (high)** "loader is pure so Phase 2 is free" is wrong about consumers:
  five components bake derived styles at construction with no SetRoles
  (chatlist:111,114; auth:43-44; composer model.go:101-102 — its SetRoles does
  NOT refresh these; contacts:54; search:195-208). Phase 2's real cost is
  making those re-derivable. Phase 1 unaffected; spec should scope this.
- **G3 (high)** Decode `[colors]`/`[colors256]` as `map[string]any`, not
  `map[string]string`: with go-toml v2.2.3, `bg = 235` (unquoted — how everyone
  writes xterm indices) fails the WHOLE file under the string-map shape,
  contradicting "all failures degrade". Coerce per-key; fold keys to lowercase;
  duplicate keys are hard parse errors (and not `*toml.DecodeError`).
- **G4 (high)** An invalid colour renders as NO colour: lipgloss emits zero
  escape bytes for junk (verified) — unpainted surfaces, the exact failure
  class of divergence 19. Validate strictly at load: 3- or 6-digit hex (3-digit
  works: `#abc` → `170;187;204`), and 0–255 range for 256 values (termenv
  happily emits invalid indices past 255).
- **G5 (medium)** Don't write a quantiser: with the ANSI256 profile lipgloss
  already down-converts hex at render time; for deterministic load-time values
  use `termenv.ANSI256.Convert` (termenv is a direct dep). Spot-checks vs the
  hand-picked table mostly agree (232/179/108/110) with deliberate divergences
  (`#6fb8c9`→74 vs 73, `#c9ced4`→188 vs 252) that substantiate the
  "hand-picked, never generated" stance — pin one in a test.
- **G6 (medium)** The ramp must NOT become a field on `Roles`: `MarkerRoles`
  sets every field to a `lipgloss.Color` by reflection and would panic. The
  refactor fear is overstated the other way: `SenderColour` has exactly two
  call sites (chatview/grid.go:242, rail/view.go:236); `SetSenderRamp` on those
  two is ~30 lines. Either do that in Phase 1 or cut `[senders]` to Phase 2.
- **G7 (medium)** Path-shaped values should be CWD-relative like every other
  path in config.toml (all `expandPath`'d then OS-resolved), not
  config-dir-relative; rule 3 already covers "next to my config".
- **G8 (medium)** Multi-profile: under `TELETUI_CONFIG=~/work.toml` the themes
  dir becomes `~/themes/`. Search `dir(ConfigPath())/themes/` then
  `dir(defaultConfigPath())/themes/` — four lines, shared collections work.
- **G9 (low)** Stat before read: regular file only, size cap (64 KiB) —
  `/dev/stdin` or a fifo blocks startup forever.
- **G10 (low)** Trim + lowercase the theme value; empty → dark WITHOUT warning
  (the `ResolveComposeEditing` precedent: empty is an older config, not a
  mistake).
- **G11 (low)** Implementer tripwires: `TestTheExampleInventsNoSettings` fails
  on any non-field `key =` line in config.example.toml even in comments — theme
  examples go in docs/, not there. The colour-literal guard matches
  `lipgloss.Color("...")` call expressions in tests too — write loader-test
  expectations as plain strings.
- **G12 (low)** Theme warnings scroll away; consider a first-render hint-bar
  notice in Phase 2. Out of scope for Phase 1.

## Open questions

- **Q1 shadowing: no, and warn** (agree with spec). Two added reasons:
  `inherit` resolves against builtins only — shadowable builtins give
  `inherit = "light"` two meanings and quietly reintroduce chains; and a
  fallback a user file can redefine is not a fallback. Escape hatch exists via
  the path form (`theme = "./themes/light.toml"`). Stat for the shadow warning
  only when the configured name IS dark/light.
- **Q2 examples: yes, in `docs/themes/`, not `contrib/`** (contrib implies a
  third-party review queue for taste). Conditions: a test loads every shipped
  theme with zero warnings; one example exercises `[colors256]`, one omits it.
- **Q3 (new): drop "required `[colors]`"** — it contradicts "all failures
  degrade" and is undetectable anyway (empty and absent tables decode
  identically). A theme with no colours is its base; warn "defines no colours".

## Phase 1 task breakdown (ordered)

1. Fix the spec per G1-G8/Q3; record the fourth splash literal in out-of-scope.
2. `config/theme.go`: pure `ResolveThemeName(value, configDir)` — table-tested,
   no filesystem.
3. `config/theme.go`: the reader — stat guard, decode, coerce, warnings;
   returns `(*ThemeSpec, []string)`, never an error.
4. Wire into `Load` + `StartupWarnings` (unexported fields; Save round-trips).
5. `theme/load.go`: reflective field map + count-pinning test (enforces the
   "new field extends the format for free" promise; fails loudly on a future
   non-Color field instead of panicking MarkerRoles).
6. `theme/load.go`: `RolesFrom` — inheritance, strict validation, per-role
   fallback; fixture under testdata/.
7. Colour depth: `[colors256]` wins → termenv convert → base hand-picked; pin
   the `#c9ced4` 188-vs-252 divergence.
8. Sender ramp: `SetSenderRamp` on chatview + rail, or cut from Phase 1. Not a
   `Roles` field.
9. Keep `app.go:308` the single dispatch point (`RolesForSpec`); palette tests
   untouched.
10. Docs: theming.md → implemented, configuration.md + config.example.toml
    comment-only edits (G11), `docs/themes/*` + loads-clean test, rewrite the
    stale TODO.md "thirteen components" entry.
11. Regression sweep: golden fixtures byte-identical, literal guard zero
    offences.
