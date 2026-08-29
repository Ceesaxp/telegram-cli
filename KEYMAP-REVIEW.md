# Keymap review — work order

> **Status: closed. All seven items shipped.** Archived for the reasoning, not
> as outstanding work — the original text below is unedited and still written
> in the imperative, so read it as a record of what was decided and why, never
> as a to-do list.
>
> | Item | Shipped as | Verify |
> | --- | --- | --- |
> | 1. `h`/`l` move between panels | `555df3c` | `internal/app/app.go`; README "Keybindings" |
> | 2. `q` quits from the browsing panels | `555df3c` | `app.go` `quitBrowsing`; confirms on a pending draft |
> | 3. `/` filters the chat list | `91331ea` | `chatlist.OpenFilter` / `FilterActive` / `ClearFilter` |
> | 4. Document the digit-key departure | shipped | `keymap.go` — "Deliberate departures from lazygit" |
> | 5. README reconciled with the dispatcher | `851b515` | `TestReadmeKeymapMatchesHelpSections` now fails the build on drift |
> | 6. Status bar hints built from resolved keys | `555df3c` | `statusbar.SetHints`, fed by `app.statusHints()` |
> | 7. `config.example.toml` dead fields and stale comment | `851b515` | quick-type wording gone; `forward` marked round-trip-only; the other seven fields are wired |
>
> Item 5's preferred fix — generating the README key section rather than
> hand-maintaining a third copy — was **not** adopted. The README tables are
> still hand-written; what was added instead is a test that diffs the
> documented key *set* and its section assignment against the help card. That
> catches a missing or misfiled key, but not a wrong *description* of what a
> key does. The three-sources problem this document names is narrowed, not
> solved.

Reviewer's finding: the keymap is internally coherent (the `internal/app/keymap.go`
prose table and the dispatcher genuinely agree, and the design choices are argued
rather than accidental). The gap to lazygit feel is **four key assignments** plus
**documentation that has drifted one wave behind the code**. The paradigm — modal,
three panels, explicit entry into typing — is sound and should not be reopened.

Removing quick-type was the pivotal decision: it committed the app to a modal design
(mutt/aerc lineage: index + pager + compose), which is the only way bare-letter
bindings like `r/e/d/s/o` can coexist with a free-text composer. Everything below
assumes that commitment stands.

Work is ordered by impact. Items 1–4 are behavior; 5–7 are drift cleanup and can be
done independently.

---

## 1. `h`/`l` should move between panels, not cycle folder tabs — HIGHEST IMPACT

This is the single biggest lazygit mismatch. In lazygit, left/right (`h`/`l`) move
**between panels**; tabs within a panel are `[`/`]`.

Current state:

- Folder tabs have **five** spellings: `alt+h`/`alt+l`, bare `h`/`l`, `[`/`]`,
  `←`/`→`, and `1-9`.
- Lateral panel movement has **zero** vi spelling — only `tab`/`shift+tab`,
  `alt+1/2/3`, and `f1/f2/f3`.

In a layout that is literally two columns side by side, a lazygit user's hands expect
`l` in the chat list to land in the chat view, and `h` in the chat view to go back.

**Change:** in the browsing panels (chat list, chat view), rebind bare `h`/`l` to
move panel focus left/right. Folder cycling keeps `[`/`]`, `←`/`→`, `1-9`, and
`alt+h`/`alt+l` — still four spellings, still terminal-independent, nothing lost.

Implementation notes:

- The bare-`h`/`l` folder handling is the `viFolder` gate in `internal/app/app.go`
  (~lines 351–370). That gate is what changes; the `m.keys.prevFolder` /
  `m.keys.nextFolder` (alt) matches in the same `if` stay as they are.
- `[`/`]` and the digits live in `internal/ui/components/chatlist/model.go`
  (~lines 386–397) and are untouched.
- Check the `h`/`l`-cycles-folders comments in both files and in
  `internal/config/config.go`'s macOS notes (which currently cite "bare h/l for the
  folder tabs" as the alt-free fallback — that fallback role passes to `[`/`]` and
  the arrows).
- `h` from the chat list and `l` from the composer-adjacent edge should be no-ops,
  not wraps — match the `esc` ladder's one-step-at-a-time discipline.

## 2. Bind `q` to quit from the browsing panels

`q` currently closes the help overlay and does nothing anywhere else. In lazygit `q`
is quit/back everywhere. The browsing panels are not text surfaces, so `q` is free
there — and it is safe, because the composer owns printables and is unaffected.

**Change:** `q` quits when focus is the chat list or chat view and no overlay/dialog
is up. Keep `ctrl+c`/`ctrl+q` global. Leave `q`-closes-help as is.

Decide and document whether a confirmation dialog is wanted; a bare `q` that drops a
half-typed draft would be worse than the current inert key. If the composer holds a
non-empty draft, prefer a confirm.

## 3. `/` in the chat list should filter the chat list

`internal/app/app.go` (~line 372) justifies the chat view's `/` with vi convention:
"find in the buffer you are looking at." The chat list breaks that same convention —
`/` there opens the **global** search overlay, a different feature.

**Change:** `/` from the chat list opens a local fuzzy filter over the visible chat
list (respecting the active folder tab). `ctrl+g` stays the global search from every
panel and is already panel-independent, so nothing becomes unreachable. Result: `/`
means "search what's in front of me" in both panels, which is the rule the code
already claims to follow.

This is the largest of the four (needs a filter input + filtered render in chatlist).
If it has to be deferred, deferring this one is fine — but then correct the comment at
`app.go:372` so it stops asserting a rule the chat list doesn't follow.

## 4. Digits: keep as-is, but document the departure

Lazygit uses digits for panels; this app uses digits for folders and `alt+`digits for
panels. **Keep it** — folder switching is the higher-frequency action in a chat
client, and `alt+1/2/3` is stable browser-tab muscle memory. But it is a paradigm
inversion, and it should be listed as a deliberate departure rather than discovered by
surprise.

Add a short "Deliberate departures from lazygit" block to the `keymap.go` header
comment covering:

- digits = folders, not panels (this item)
- `enter` in the chat view = open attachment, where a drill-in convention would
  expect focus/expand
- `o` = open attachment in the chat view, but open-line-below in composer vi mode
  (fine as modal context, but worth stating)

---

## 5. README is a wave behind the code

- **Quick-type is documented but removed.** `README.md:133` still lists "any printable
  key — Quick-type", and the warning at `README.md:185` describes a
  single-char-binding hazard that no longer exists (removal is recorded in
  `internal/app/keys_test.go:692` — `TestComposeRequiresAnExplicitMove` — and in the
  `chatlist/model.go:381` comment). Delete both; document `i`/`c` as the way into the
  composer.
- **The `[keys]` "Wired?" table is stale.** It omits `help`, `global_search`, and
  `contacts_alt`, all of which exist in `internal/config/config.go` and are dispatched.
- **The key tables omit** `?`, `ctrl+g`, `[`/`]`, `1-9`, and `i`/`c`.

Preferred fix: generate the README key section from `Model.helpSections()` rather than
hand-maintaining a third copy. There are currently three sources — the `keymap.go`
prose table, `helpSections()`, and the README — and only the first two are kept in
step. If generation is too much, at minimum reconcile the README against
`helpSections()` and add a note pointing at `keymap.go` as the source of truth.

## 6. Status bar is hardcoded and omits `?`

`internal/ui/components/statusbar/model.go:160` renders a fixed string:

    Alt+1/2/3:Focus  /:Search  Alt+C:Contacts  ←→/1-9:Folders

Two problems. It won't reflect rebinds — which violates the principle `keymap.go`
states as the reason `helpSections()` is generated from `resolvedKeys` ("a rebound key
must not leave the card advertising the old one"). And it omits `?`, the one key that
unlocks every other key; `?:Help` earns its place there more than anything currently
listed.

**Change:** build the hint strip from the same resolved keys the dispatcher matches,
and include `?:Help`. Update `statusbar/model_test.go:101` accordingly. If items 1–2
land, the strip should also show the new `h/l` and `q` meanings.

## 7. `config.example.toml`: dead fields and a stale comment

The key list itself is in sync with `defaultConfig()` — good. Two remaining issues:

- Lines 58–59 warn about shadowing **quick-type**, which no longer exists. Rewrite to
  describe what actually happens now: a wired bare-printable binding shadows that key
  in the chat list and chat view, but never in the composer.
- Eight fields are advertised but never consulted: `reply`, `edit_message`,
  `delete_message`, `forward`, `scroll_up`, `scroll_down`, `page_up`, `page_down`.
  Shipping them in the example config invites users to set bindings that silently do
  nothing. Either wire them (the components hardcode these today) or mark them clearly
  in the example file as accepted-for-round-trip-only. Do not just delete them —
  `config.go` parses them so old config files still load.

---

## Acceptance

- `keymap.go`'s prose table, `helpSections()`, the status bar, the README, and
  `config.example.toml` all describe the same keymap.
- No key has a meaning in the docs it does not have in the dispatcher.
- `internal/app/keys_test.go` covers the new `h`/`l` panel movement and `q` quit,
  including the negative cases (`h`/`l` inert with an overlay up; `q` inert from the
  composer).
- The "deliberate departures from lazygit" list exists and is honest.
