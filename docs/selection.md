# Text selection — spec

Status: **proposal** (2026-09-25). Nothing here is built. Phase 0 —
documenting the terminal's own Shift+drag — is separate and shipped in
`docs/mouse.md`; this document is about selection inside the app.

The client enables mouse reporting, so the terminal stops doing its own
selection and hands the events over. Today those events are used for two
things: a click focuses a panel and moves a cursor, and the wheel scrolls
(`handleMouseClick`, `handleMouseWheel` in `internal/app/app.go`). Dragging
does nothing. `Shift+drag` still gets a native selection, of the screen
rectangle, gutter and rail included.

## Why build it at all

Not to match the terminal — to beat it. A terminal can only copy what is
painted: the time and sender gutter, lines hard-wrapped at the width the
window happened to be, the panel rule, the rail's cells. The app knows
which message each cell belongs to and what that message's text actually
is, so it can copy the **source text**: unwrapped, ungutted, exactly the
runes somebody sent.

That is already the rule `y` follows. `YankCmd` copies `messageText(msg)` —
the message's own `FormattedText.Text`, explicitly not the rendered body,
with the comment "a message that is nothing but a code block yanks as the
code" (`internal/ui/components/chatview/actions.go:41`). Selection is `y`
with a finer grain, and it must agree with it.

## The decision that shapes everything

**A selection is a range over source text, never over screen cells.** The
mouse only *locates* a position; the range is `(messageID, runeOffset)` at
each end, and copying reads the source. Screen cells are an input and an
output, never the state.

This follows from three things the renderer does:

- **Wrapping discards the correspondence.** `cell.WrapLines` delegates to
  `ansi.Wrap`, which returns lines and no index back into the source
  (`internal/ui/cell/cell.go:139`). Nothing anywhere maps a column to a
  rune; `chatview.ClickAt` resolves a row to a message and stops
  (`cursor.go:125`).
- **Rendered lines carry decoration that is not text.** A code block is
  drawn inside a frame with a header and line numbers (`renderCodeBlock`,
  `internal/render/blocks.go:296`), and none of it is distinguished from
  content in the output string. A selection that read rendered lines would
  copy box-drawing characters.
- **A spoiler's text is in the drawn string.** It is hidden by painting
  foreground and background the same colour (`internal/render/entities.go:196`),
  not by substitution. A column-to-text reader that took the painted runes
  would read a spoiler the reader cannot see.

Anchoring to identity rather than position is the same choice `cursorID`
already makes over `scrollOffset`, and for the same reason: a photo
thumbnail landing changes every line count on screen, and a position-based
anchor silently comes to mean something else
(`internal/ui/components/chatview/model.go:171`).

## What has to be built, and the one hard part

**The line map is the whole project.** Everything else is small once it
exists. For each physical row of a rendered message the app needs:

```
lineSpan{
    lo, hi int  // rune offsets into the message's source text
    col    int  // the column where the body starts on this row
    text   bool // false for a row that is not message text at all
}
```

`text: false` covers every row that has no source range: a day or unread
divider, a reply or forwarded header, the send-state row, and a code
block's frame and header rows. A selection skips them; a drag across them
is a gap, not a selection of box characters.

Where it comes from: the body is wrapped *before* the gutter is composed —
`renderBlocks` styles each rune run and then calls `cell.WrapLines`, and
`gridGeometry.row` adds the time and sender per physical row afterwards
(`internal/ui/components/chatview/grid.go:319`). So an indexed variant of
`WrapLines` that reports the source range of each line it emits gives the
`lo`/`hi`, and the gutter's own width gives the `col`. Both are known at
the point the lines are built.

Where it lives: `gridEntry` in the render cache, beside the `lines` it
already holds. The cache is keyed by message ID, re-renders when the width
changes, and is invalidated per message on edit, reaction and refetch
(`model.go:81`) — every condition under which the map would go stale is
already handled for the lines, and the map goes stale on exactly the same
ones.

**Wide characters and clusters.** A click on the second cell of a
double-width glyph resolves to the rune that owns it; a click inside a ZWJ
cluster resolves to the cluster's first rune. The unit of selection is the
grapheme cluster, not the rune, or a selection can cut a family emoji in
half. `internal/ui/cell` already owns this machinery.

## Drawing the selection: free, via a path that exists

The renderer already takes a rune range and styles per rune. `BodyOptions`
carries `ArmedLinkLo/ArmedLinkHi` for the link armed with `gx`, and
`inlineStyles` turns that into a per-rune flag that changes how the run is
painted (`internal/render/markdown.go:173`, `internal/render/entities.go:82`).

A selection is the same shape: `SelectedLo/SelectedHi`, a second per-rune
flag, painted with the selection role. No partial-ANSI surgery over an
already-styled line — which would be the ugly way — and no new
invalidation rule, because arming a link already invalidates one message's
cache entry and a selection change is that same event.

## The interaction

- **Drag in the thread selects text.** Press, move, release. No terminal
  mode change is needed: `MouseModeCellMotion` is already set on every
  frame (`app.go:2687`) and reports motion while a button is held, so
  `MouseMotionMsg` and `MouseReleaseMsg` already arrive and are simply not
  handled yet. This is a switch case, not a protocol change.
- **The selection is clamped to the pane it began in.** Starting in the
  thread and dragging into the chat list extends within the thread and
  stops at its edge; it never selects two panels at once. `m.layout` owns
  the rectangles (`internal/ui/layout`), `bodyRow` the vertical chrome
  offset; the thread's local column origin is the one piece of geometry
  that does not exist yet.
- **Double-click selects a word, triple-click the message's body.** Cheap
  once the map exists, and between them they cover most of what a person
  actually wants.
- **Release copies, and says so.** Consistent with `y`, which reports
  `YankMsg{Runes: …}` and shows a notice. A selection that needs a second
  keystroke to copy would be a worse `y`, not a better one.
- **`v` drives the same state from the keyboard.** A vi-shaped client
  should have visual mode regardless, it needs the same anchors and the
  same copy path, and building selection as a mouse feature would be the
  wrong shape. `v` is in scope for this reason, not as an extra.
- **Esc clears it**, and so does opening another chat.

## What a selection copies

- **Within one message**: the source runes between the two anchors.
  Unwrapped — the newlines the sender typed, not the ones the pane width
  produced.
- **Across messages**: each message's selected run, prefixed `sender: `
  and separated by a newline. That is what a person pastes into a chat,
  and it is the reason to know about structure at all.
- **Inside a code block**: the code, with no frame, no line numbers and no
  language header — the same text `y` yanks.
- **A link**: its label, not its destination. The destination is
  deliberately out of band (`gx` shows it; OSC 8 carries it), and a
  selection that silently pasted a URL where the text said something else
  would be a small lie.
- **A spoiler**: its text. This is not a leak to argue about — `y` already
  copies it, and the reader can reveal it with `x`. It is recorded here
  because the opposite choice looks defensible until you notice the
  inconsistency it would create.

Copying goes through `internal/clipboard`, which shells out to a platform
tool and deliberately has no OSC 52 fallback, for reasons written down in
`copy.go:23`. **So selection cannot copy over a plain ssh session**, the
same as `y` today. Selection makes that limitation more visible without
changing it; re-read that comment before deciding it is still right.

## Waves

**Wave 1 — the line map** (structural, invisible). The indexed wrap, the
`lineSpan` table, threading it through `renderBlocks` and `RenderBody` into
`gridEntry`. No behaviour change and no UI; tested directly against
messages with wide characters, clusters, code blocks, quotes and replies.
Everything else depends on it, and it is the only part where being wrong is
expensive.

**Wave 2 — point to position.** `(row, col)` to `(messageID, runeOffset)`
and back, copying the shape of `ClickAt`'s row walk, plus the thread's
local column origin in `internal/app`.

**Wave 3 — the selection itself.** The anchored state, the three mouse
events, the `SelectedLo/Hi` render path, Esc, and copy-on-release with a
notice.

**Wave 4 — the rest of the interaction.** Double and triple click, `v`
visual mode over the same state, auto-scroll when a drag reaches the pane's
edge, and the cross-message `sender: ` form.

**Out of scope, recorded.** Selecting in the chat list or the rail (rows
there are not text a person quotes); selecting across history that is not
loaded; selecting the gutter; a mouse-driven selection in the composer,
which is a text area with its own cursor and should not grow a second
model.

## Risks

- **The line map drifting from the lines.** Two things describing one
  render is a bug waiting to happen. They must be produced together, in one
  function, or the map will eventually describe the previous width. A test
  that asserts every span's `lo`/`hi` slices back to the text actually on
  that line is the guard.
- **Drag event volume.** A fast drag across a tall pane emits a motion
  event per cell. The selection change per event is cheap, but a re-render
  of the message under it is not entirely — the armed-link path has the
  same shape and is fine at human speeds, which is evidence rather than
  proof.
- **Code block decoration.** The frame is drawn into the same string as the
  code. If `text: false` is not exactly right for those rows, a selection
  will quietly copy `│` characters, and it will look like the map works.
- **A selection that outlives its message.** Deleted mid-drag, or scrolled
  out of loaded history. The anchor is an ID, so the failure is detectable:
  clear the selection rather than clamp it to a neighbour.
- **Habit.** A reader who already knows `Shift+drag` will keep using it and
  get the gutter. That is an acceptable escape hatch, not a bug, but it
  means the in-app version has to be visibly better or it will read as a
  regression.
