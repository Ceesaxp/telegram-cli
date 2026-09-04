# Interaction review — work order

> **Status: shipped.** All four waves landed. Raised 2026-09-04 from a
> review of the interaction model, keymaps and interactive surfaces of
> `teletui`. The decisions it implements are in
> [docs/interaction-model.md](docs/interaction-model.md) (cited as I-n
> below); read that first, it is the authority on *what* and *why*. This
> file is only *in what order* and *what done looks like*, and is kept as
> the record of the round rather than as outstanding work.
>
> Two deviations were made on implementation and are recorded rather than
> left in a commit message: `keymap.go`'s prose keymap was deleted in wave
> 2 rather than wave 4, because a commit that removed `alt+1/2/3` while
> shipping a table describing them would break this file's own first ground
> rule; and the badge column is padded to six cells rather than seven,
> which is written up as an amendment to I-12 in the model document because
> seven would have moved the frame.
>
> Successor to [`KEYMAP-REVIEW.md`](KEYMAP-REVIEW.md), which stays as the
> record of the previous round. Its verdict that the paradigm should not be
> reopened is upheld here.

## Ground rules for every task

- **Ship the docs with the code, in the same commit.** `docs/keys.md` and
  `helpSections()` are diffed by `TestKeymapDocMatchesHelpSections`; a key
  change without its rows fails the build. That is the intended friction.
- **Regenerate goldens only when copy changes, never when geometry does**
  (TUI 2.0 decision 11). Several tasks change hint text in the fixtures
  under `docs/fixtures/`; a diff there must be a text diff on a hint row,
  and every row must still be exactly its stated width.
- **Every retired key gets a negative test** (it is inert where it was
  live, and the letter is typeable in the composer if it is a printable),
  in `internal/app/keys_test.go`.
- **Do not widen.** Each task is the finding it names. A second thing found
  on the way gets a note in `TODO.md`, not a commit.
- `go test ./...` and `go vet ./...` green before push.

## Sequence

Four waves. Tasks inside a wave are independent and can run in parallel
on separate branches; waves are ordered because the later ones edit the
same files as the earlier ones (`internal/app/app.go`, `keymap.go`,
`docs/keys.md`, `config.go`) and would conflict.

| Wave | Purpose | Tasks |
|---|---|---|
| 1 | Stop losing work. No key changes. | 1, 2, 3, 4 |
| 2 | The keymap cut. Alt/Fn gone, motions straightened, config simplified. | 5, 6, 7, 8, 9, 10, 11 |
| 3 | Truthful surfaces. One hint table, fourth badge. | 12, 13 |
| 4 | Documentation and residue. | 14, 15 |

Wave 2 is the largest and touches `app.go` from three directions. Tasks
5, 6 and 9 all edit the dispatcher and `config.go`; run them **serially in
the order 9 → 5 → 6**, or hand them to one agent. Tasks 7, 8, 10 and 11
are confined to a component each and can run beside them.

---

## Wave 1 — safety

### 1. `Esc` keeps the text (I-3) — HIGHEST IMPACT

`composer.handleEsc` calls `Reset()` on the cancel rung, which wipes the
textarea. In emacs mode that is the first `Esc`; in vi mode the second,
which is the reflex every vi user types. A half-written reply is gone
without a confirm, while `q` asks before dropping the same text.

A second loss on the same path: `EnterEditMode` overwrites
`textarea.Value` with the message being edited, so pressing `e` while a
draft exists destroys the draft.

**Change**

- Split `Reset` into *clear context* (mode, reply target, edit target,
  attachment, notice) and *clear text*. The `Esc` cancel rung calls only
  the first. `submit` still calls both.
- `EnterEditMode` parks the current draft (text and reply target) before
  loading the message text; cancelling the edit or sending it restores the
  parked draft. Reuse the per-chat `drafts` map's `draft` type rather than
  adding a second one.
- Unstaging an attachment on `Esc` stays as it is.

**Files** `internal/ui/components/composer/model.go`, `model_test.go`;
`internal/app/keys_test.go` for the ladder end to end.

**Acceptance** In both editing modes: type text, `r` on a message, type
more, `Esc` (twice in vi) → reply chip gone, text intact, focus still on
the composer. `e` on an own message with a draft pending, `Esc` → the
original draft is back. The existing `Esc` ladder tests pass unmodified.

### 2. `:quit` confirms like `q` (I-5)

`commands.go`'s `quit` returns `tea.Quit` unconditionally; it is reachable
from a vi composer in its command state with a draft on screen.

**Change** Route `:quit` through the same check `quitBrowsing` uses
(`HasDraft() || Attachment() != ""` → the `"quit"` confirm dialog).
Extract that check into one method so the two cannot drift.

**Files** `internal/app/commands.go`, `app.go`, `commands_test.go`.

**Acceptance** `:quit` with a draft opens the confirm; without one quits.
`ctrl+q` is untouched and has a test saying it still quits with a draft.

### 3. The delete dialog says for whom, and dialogs answer to `y`/`n` (I-7)

"Are you sure?" deletes for everyone (`revoke=true`), and the dialog names
nothing. Dialogs also refuse `y`/`n`.

**Change**

- `dialog` grows a general N-button constructor (`NewChoice(roles, id,
  title, message, buttons []Button)`) where a `Button` has a label, an
  accelerator rune and a result value. `NewConfirm` and `NewAlert` become
  wrappers. `DialogResultMsg` carries the chosen button's value as well as
  `Confirmed`.
- Two-button confirms accept `y` and `n`. Every button's accelerator is
  drawn in its label (`[ For (m)e ]` or an underline, whichever survives
  the monochrome rule the dialog already follows for brackets).
- Delete becomes *Delete this message?* with `Cancel · For me · For
  everyone`, Cancel highlighted, accelerators `n` / `m` / `e`. "For me"
  calls `DeleteMessages` with `revoke=false`. A server refusal of "for
  everyone" (too old, not permitted) reports in the notice row.
- The dialog's own hint line reads from the button set.

**Files** `internal/ui/components/dialog/model.go`, `model_test.go`;
`internal/app/app.go` (`handleMessageAction`, `DialogResultMsg`),
`keys_test.go`; `docs/keys.md` overlays section.

**Acceptance** `d`, `Enter` → nothing deleted. `d`, `e` → deleted with
revoke. `d`, `m` → deleted without. Quit confirm: `q`, `y` quits; `q`, `n`
returns. `j`/`k` still do nothing in a dialog.

### 4. `quit` refuses a bare printable (I-13, first half)

**Change** In `config` key resolution, a `quit` value that is a single
unmodified printable is refused with a startup warning and falls back to
`ctrl+q`. Reuse the collision-warning path so the message shape matches.

**Files** `internal/config/config.go`, `config_test.go`;
`internal/app/keymap.go` (`quitKeys` shows the fallback).

**Acceptance** `quit = "x"` → warning, `x` types in the composer, `ctrl+q`
quits. The keys.md warning paragraph about this is deleted.

---

## Wave 2 — the keymap cut

### 9. `[keys]` becomes one semantic (I-13) — do this one first in the wave

Three semantics (replace / add alongside / round-trip only), a
collision-refusal that only reports under `-migrate-config`, and `forward`
bound to nothing.

**Change**

- `KeyConfig` fields become exactly the table in interaction-model.md
  "Configuration": add `compose`, `next_unread`, `mark_read`; remove
  `focus_chat_list`, `focus_chat_view`, `focus_composer`, `contacts_alt`,
  `forward`, `scroll_up`, `scroll_down`, `page_up`, `page_down`.
- Removed fields go into `migrate.go`'s `removedFields` with this version,
  so `-migrate-config` reports them as `(removed)` exactly as decision 10
  did for `ui.chat_list_width`. Confirm the decoder still loads an old
  file with them present (it ignores unknown fields today; pin that with
  a test).
- Every field is *replace*. A collision — with another field or with a
  fixed key the panels own — is refused, the default is kept, and the
  warning is printed **at startup**, not only under `-migrate-config`.
  Delete chatview's separate `scrollUpExtra`/`pageUpExtra` machinery and
  the additive `alsoBound` rendering in `helpSections`; motions are no
  longer configurable.
- `keys.AppReserved` and `chatview.SetReservedKeys` keep their job; the
  reserved list shrinks to what remains.
- `config.example.toml` `[keys]` is rewritten to the new field set and the
  long comment above it is cut to the one rule. `TestExampleConfigKeysMatchDefaults`
  keeps it honest.

**Files** `internal/config/config.go`, `migrate.go`, both `_test.go`;
`internal/app/keymap.go`, `app.go` (`resolveKeys`, `reservedKeys`);
`internal/ui/components/chatview/model.go` (`Keys`, `SetKeys`,
`ActiveKeys`); `config.example.toml`; `docs/keys.md` "Configuring keys";
`docs/configuration.md` if it lists fields.

**Acceptance** The Wired? table in keys.md has one column fewer and every
row says yes. `reply = "h"` at startup prints the refusal and `h` still
moves panels. An old config with `scroll_up = "k"` loads, and
`-migrate-config` lists it as removed.

### 5. Alt and function keys go; their plain spellings arrive (I-1)

**Change**, in `app.go`'s dispatcher:

- Remove `alt+1/2/3`, `f1`–`f3` (panel focus), `alt+j/k`, `alt+h/l`,
  `alt+c`, `f4`. Remove `alt+enter` from `composer.isNewlineChord`.
- `c` in the browsing panels toggles contacts (`keys.contacts`). `c` in
  the contacts overlay closes it, so the toggle stays a toggle.
- `J` / `K` in the browsing panels open the next / previous chat
  (`keys.next_chat` / `prev_chat`), through `chatList.SelectDelta` and a
  `ChatSelectedMsg` as today's `alt+j` does. They open immediately — that
  is their point, unlike `j`/`k`.
- `u` in the browsing panels opens the next chat with unread messages
  (`keys.next_unread`): search downward from the cursor within the active
  folder, wrap once, notice `no unread chats` if none. Add
  `chatlist.SelectNextUnread()`.
- `[` / `]` move to app-level for both browsing panels (`keys.prev_folder`
  / `next_folder`). chatlist keeps `←`/`→` and the digits. Remove
  chatlist's own `[`/`]` case so there is one implementation.
- `Tab`/`Shift+Tab` unchanged (I-9). `Esc` unchanged.
- Delete the macOS Option section from keys.md and the Option notes on
  `config.KeyConfig`. Delete `contacts_alt` handling (task 9 removed the
  field).

**Files** `internal/app/app.go`, `keymap.go`, `keys_test.go`,
`reserved_keys_test.go`; `internal/ui/components/chatlist/model.go`;
`internal/ui/components/composer/editing.go`, `editing_test.go`;
`docs/keys.md`; `docs/features.md` (folder paragraph mentions Alt).

**Acceptance** Negative tests for every retired chord. `J` from the chat
view opens the next chat without leaving the view. `u` with no unread
reports rather than moving. `]` from the chat view switches folder.
`alt+enter` in the composer inserts nothing and is not advertised.

### 6. The chat list opens what the cursor is on (I-2)

**Change**

- `l` in the chat list: if the cursored chat is not the open chat, emit
  `ChatSelectedMsg` for it (which already focuses the chat view via
  `openChatAt`); otherwise just focus the chat view. `Enter` is unchanged
  and now equals `l`.
- `i` in the chat list: same open-if-different step, then focus the
  composer. `i` in the chat view is unchanged.
- The `i` guard `composer.ChatId() != 0` becomes "there is a cursored
  chat" for the chat list case.

**Files** `internal/app/app.go`, `keys_test.go`;
`internal/ui/components/chatlist/model.go` (`CursorChatId()` accessor).

**Acceptance** Open chat A, `jj` to chat C, `l` → chat view shows C. Same
with `i` → composer is pointed at C (per-chat draft for C restored).
`l` with the cursor on the open chat sends no `ChatSelectedMsg` (no
reload). Single-panel width: same behaviour.

### 7. `j`/`k` step messages, `ctrl+e`/`ctrl+y` scroll lines (I-4)

**Change**, in `chatview.handleKey`:

- `j`/`k`/`↓`/`↑` call `moveCursor(±count)` — today's `}`/`{` body,
  including the minimum-scroll rule and the pin.
- `ctrl+e`/`ctrl+y` take over today's `j`/`k` body: scroll by one line
  per count (not three; a line is a line), cursor follows the viewport via
  `syncCursor`, the `clampScrollUp` older-page fetch stays on the upward
  motion.
- `}`/`{` removed. `isScroll` and the lazy-photo trigger cover the new
  keys.
- The header's pending-count display and `takeCount` are unchanged.
- Mouse wheel stays three lines per notch.

**Files** `internal/ui/components/chatview/model.go`, `model_test.go`,
`count.go`; `docs/keys.md`; `docs/tui-2.0.md` divergence 23 gets a
one-line "superseded by I-4" note at its head.

**Acceptance** `k` from the newest message moves the cursor to the one
above and pins it; a new arrival does not move it; `G` unpins. `3j` moves
three messages. `ctrl+y` on the oldest loaded line requests the next page.
`}` and `{` are inert and untested-positive anywhere.

### 8. `m` marks read (I-10)

**Change** Rebind `M` → `m` as `keys.mark_read` (task 9 added the
field); `M` becomes inert. Palette `:mark-read` shows the resolved key.

**Files** `internal/ui/components/chatview/model.go`, `model_test.go`;
`internal/app/commands.go`; `docs/keys.md`, `docs/features.md`.

### 10. `q` closes no overlay (I-8)

**Change** Remove `q` from the help card's close set, the media overlay's
close set and the reaction row's cancel set. `Esc` (and `?` for help)
close. Update each surface's own hint string.

**Files** `internal/app/app.go`, `keymap.go` (`helpFooter`);
`internal/ui/components/mediaview/model.go`;
`internal/ui/components/reactionpicker/model.go`; tests beside each.

**Acceptance** `?` then `q` → help still open, and a test that `?`, `q`,
`q` does **not** quit.

### 11. A click in the thread moves the cursor (I-11)

**Change** `handleMouseClick`'s thread branch passes the body row to a new
`chatView.ClickAt(row)` that resolves the row to a message through the
existing line index (`sliceLines` / `renderedMessages`) and calls
`setCursor` with the pin rule of an explicit motion.

**Files** `internal/app/app.go`; `internal/ui/components/chatview/model.go`,
`model_test.go`.

**Acceptance** Click a message, `r` → the reply chip names that message.
A click on a day divider or the header moves nothing.

---

## Wave 3 — truthful surfaces

### 12. One hint table, keyed by surface (I-6)

The hint bar is keyed by mode and shows the chat-view set in the chat
list, under contacts, under a dialog and in a vi composer. The chat list
footer advertises `u unread` with nothing bound (task 5 binds it, but the
footer must still be *derived*, or the next one recurs). `helpLine` is a
correct per-focus generator that nothing renders.

**Change**

- New `internal/app/hints.go`: a `Surface` enum (chat list, chat view,
  composer insert, composer vi, palette, attach, contacts, search,
  dialog, help, media, reactions) and `func (m Model) hintsFor(Surface)
  []hintbar.Hint`, built from `resolvedKeys` and the sets in
  interaction-model.md "Hints", in that order. `Model.surface()` derives
  the current one from focus and overlay state the same way `Mode()`
  does, and `Mode()` may be expressed through it.
- `refreshChrome` calls `hintsFor(m.surface())`. Delete `hintsForMode`
  and `helpLine` and their tests; move any test that pinned a *rebound*
  key showing correctly onto the new function.
- `chatlist.renderListFooter` takes its hints from the app (a
  `SetFooterHints` setter, called from `refreshChrome`), never from a
  literal. Same for `dialog.renderHint` (task 3 already made it read the
  button set) and `mediaview.hints`.
- Extend `keymap_docs_test.go`: for every surface, every key named in
  its hint set must appear in that surface's help section, and every
  literal `hint(...)` call in a component is gone (grep in the test, the
  way `TestAppFixedMatchesDispatcher` parses `app.go`).
- Regenerate the frame fixtures whose hint row changes. Geometry must not
  move.

**Files** `internal/app/hints.go` (new), `frame.go`, `app.go`,
`keymap_docs_test.go`, `keys_test.go`; `internal/ui/components/chatlist/rows.go`;
`docs/fixtures/*.txt`; `docs/tui-2.0.md` "Top bar, chat list, and hint
bar" gets a note that the sets live in interaction-model.md.

**Acceptance** With contacts open the bar reads the contacts set. With a
confirm up it reads the dialog set. In a vi composer in command state it
reads the VI set. `TestHintSurfacesMatchHelp` exists and fails when a
literal hint is reintroduced.

### 13. The fourth badge, `VI` (I-12)

**Change** `InteractionMode` gains `ModeVi`; `resolveMode` returns it for
`focus == PanelComposer && composerViNormal`. `composer.AppMode` gains
the mirror value, mauve, label `VI`, padded to the seven-cell badge
column. `:` opens the palette from `ModeNormal` **or** `ModeVi`; the
backtick stays `ModeNormal` only. `TestComposerModeIsExhaustive` and
`TestComposerBadgeAgreesWithTheResolver` extend to the new value.

**Files** `internal/app/mode.go`, `mode_test.go`, `frame.go`, `app.go`;
`internal/ui/components/composer/view.go`, `view_test.go`;
`docs/features.md` (mode badge bullet), `docs/tui-2.0.md` "Mode
integration" gets a superseded note.

**Acceptance** Vi composer, `Esc` → badge `VI`, hint bar VI set, `:` opens
the palette, `?` does not open help. Emacs composer never shows `VI`.

---

## Wave 4 — documentation and residue

### 14. One prose copy fewer (I-15)

**Change** Delete the keymap prose comment at the top of
`internal/app/keymap.go` (it already lists the removed `c`); leave a
three-line pointer to `docs/interaction-model.md` and `docs/keys.md`.
Rewrite `docs/keys.md` to the post-cut keymap in full: the tables, the
`Esc` ladder paragraph, the composer exception list (now `ctrl+q`,
`ctrl+v`, `Esc`, `Tab` and nothing else), the overlays table, and the
"Configuring keys" section. Update the README's keybinding pointer and
the frame excerpt if a hint row in it changed. Update `docs/tui-2.0.md`
decision 3's "Escape ladder" paragraph with a note that I-3 refines it.

**Files** as named.

### 15. Residue

- `TODO.md`: close "Status bar hints are hardcoded" (done twice over now),
  add a "Interaction review" pointer to this file, and note the two things
  found but out of scope: no forward-message feature, no mute key
  (palette `:mute` is designed but unregistered).
- `KEYMAP-REVIEW.md`: add a line at the head pointing here as the
  successor.
- `docs/features.md`: the "Everything, briefly" bullets that name
  `Alt+H`/`Alt+L`, `M`, `}`/`{`.

---

## Acceptance for the whole order

- No `alt+` or `f<n>` string survives in `internal/app`, `internal/config`,
  `internal/ui`, `docs/` or `config.example.toml` except in a negative
  test or a migration report.
- `Esc` cannot lose typed text on any surface; a test says so per surface.
- Every hint string on every surface is produced by `hintsFor`, and a
  test fails when one is not.
- `docs/keys.md`, the help card, the hint bar, the chat list footer, the
  dialog hint and `config.example.toml` describe one keymap.
- `docs/interaction-model.md` is still true. If a task had to deviate
  from it, the deviation is recorded there as an amendment to the
  decision, not left in a commit message.
