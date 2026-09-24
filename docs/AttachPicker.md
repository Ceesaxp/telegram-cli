> Extracted from `design_handoff_tui_redesign/README.md` §16 (TUI 2.0 redesign
> handoff) on 2026-09-02. Self-contained: everything needed to build the
> surface is below. Target repo: github.com/ceesaxp/telegram-cli, Bubble Tea
> v2 + Lipgloss, presentation layer only.
>
> Colour roles referenced here (`fg`, `faint`, `ghost`, `amber`, `cyan`,
> `red`, `bright`, `rule-soft`) are the shipped `theme.Roles` values — see
> `internal/ui/theme/roles.go` and README §2.
>
> Live reference: `Telegram TUI.dc.html` in the main handoff bundle — press
> `t` or `^t`, type to filter, `⇥` to complete, `↵` to attach.

# Attach picker (`Ctrl+T`) — replaces the prompt dialog

`Ctrl+T` currently raises `dialog.NewPrompt(… "attach-file", "Attach File",
"Path to file:")`: a centered rounded box with a title, a 30-col blind text
field, and a `[ Cancel ] [ OK ]` button row. It is the last GUI dialog left in
the client — it has buttons, it has title case, it cannot complete a path, and
it cannot tell you whether the thing you typed exists until it fails. Delete
that call site and build the surface below.

**New component: `internal/ui/components/attach/`.** It is the palette's twin,
not the dialog's: same 60-col fixed width, same anchor (~8 rows from the top),
same `▌` selection marker, same key-hint footer, no buttons anywhere. The
palette collects a command; this collects a path. Everything on screen is
derived from the typed path, the way a shell derives completions.

```
 ▤ ~/Downloads/back█                                       1 of 1
▌▤ backoff.patch                              2.1 KB      14:22
 ────────────────────────────────────────────────────────────────
 backoff.patch · 2.1 KB                    document · original bytes
 ↵ attach  ⇥ complete  ^p as photo  ^h up  esc cancel
```

With nothing typed after the directory:

```
 ▤ ~/Downloads/█                                          1 of 10
▌▸ patches/                                 12 items
 ▣ auth-p95-2608.png                          184 KB     26 Aug
 ▤ backoff.patch                               2.1 KB     14:22
 ▤ incident-0812.md                            6.2 KB     09:05
 ▣ rail-mock.png                               220 KB     24 Aug
 ▶ standup-2608.m4a                            1.4 MB     26 Aug
 +4 more
 ────────────────────────────────────────────────────────────────
 patches/ · directory                                  ↵ to enter
 ↵ open  ⇥ complete  ^h up  esc cancel
```

### Column arithmetic (60 cols)

| Cols | Content |
|---|---|
| 1 | selection bar: `▌` cyan on the cursored row, blank otherwise |
| 2 | type glyph + space |
| flex | entry name, truncated with `…`; a directory keeps its trailing `/` |
| 9 | size, right-aligned, `faint` — `12 items` for a directory |
| 8 | mtime, right-aligned, `ghost` — `HH:MM` today, `26 Aug` otherwise |

**Prompt row.** Amber `▤`, then the path in two colors: the directory part in
`#7f8a93` and the typed tail in `bright`, so the cursor's scope is visible.
The rest of the cursored entry's name follows in `ghost` as an inline
suggestion (shell-style, not a popup), then a cyan block cursor. Right side is
`N of M` in `ghost` — cursor position within the current match set.

**Type glyphs** reuse the media-card badges, so a file looks the same here as
it will in the thread: `▣` image, `▤` document, `▶` audio, `▷` video, and
`▸` for a directory. Files take amber (the attachments role); a directory's
glyph is `ghost` and its name `#9aa4ac` — structure, not payload.

**List cap** is 6 rows + `+N more`, the palette's rule for the same reason: the
overlay sits 8 rows down and must survive a 24-row terminal.

**State row**, under a `rule-soft` divider — one line, never two:

| Condition | Left (`fg`) | Right (`faint`) |
|---|---|---|
| file cursored | `name · size` | `photo · recompressed` or `document · original bytes` |
| directory cursored | `name · directory` | `↵ to enter` |
| no match in a real directory | `no match in ~/Downloads/` in amber | — |
| directory does not exist | `no such directory` in red | — |

The right half is the fix for the real defect behind this rework: the old
prompt always attached as a document (`m.replaceAttachment(path, false)`),
while `Ctrl+V` attached images as photos. Now the send mode is stated before
you commit, and `^p` toggles it — for images only; on anything else the hint
reads `^p document only` and the key is inert.

### Keys

| Key | Action |
|---|---|
| printable | appended to the path; the match list and suggestion refilter live |
| `⇥` | complete the path to the cursored entry (adds the `/` on a directory) |
| `↵` | on a directory, descend into it; on a file, attach and close |
| `↑` `↓` | move the cursor — **not** `j/k`: this is a text surface and a path may contain either letter, exactly the divergence the palette already recorded |
| `^h` / `←` | up one directory |
| `^p` | toggle photo/document (images only) |
| `⌫` | delete a character, never past `~/` |
| `esc` | cancel, staging nothing |

### After it closes

Attaching switches the composer to its **expanded** form in INSERT with the
chip already staged — `▣ auth-p95-2608.png ✕` under the source column, the
header reading `1 photo` or `1 attachment` — so the caption is the next thing
you type and the thing you just attached is visible while you type it. One
staged attachment still (decision 5): attaching over an existing one replaces
it and says so in the hint bar, unchanged.

The two refusals stay exactly as they are: `Ctrl+T` during an edit does not
open the picker at all and shows `⚠ cannot attach while editing`, and
switching chats discards a staged attachment with the draft.

### Cleanup

`attach-file` is the only `dialog.NewPrompt` caller. Once the picker lands,
`dialog.KindPrompt`, its input branch in `Update`, `theme.OverlayInput` and the
prompt-specific hint are dead — delete them, and with them the
`Enter`-accepts-OK reasoning in `NewPrompt`'s doc comment. `KindConfirm` and
`KindAlert` stay: a destructive yes/no is genuinely a two-button question.
`internal/app/mode.go`'s "prompt dialog such as the attach-file path" branch
moves to the picker's own mode (printables type, so it resolves like the
palette does).

Frame integrity is asserted the same way as the palette's: every line of
`View()` is exactly 60 cells, measured with `uniseg`, including rows carrying a
CJK filename.
