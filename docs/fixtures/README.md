# Terminal golden fixtures

Answer to **D11**. These are deterministic, cell-exact renderings of the target
frame — the thing the HTML prototype cannot be. Every line in every file is
**exactly** the stated display width. Treat them as golden files.

| File | Size | Purpose |
|---|---|---|
| `frame-80x24.txt` | 80×24 | Minimum supported. Rail off, chat list at 30, sender column compressed to 8 |
| `frame-100x30.txt` | 100×30 | Rail off, chat list 38 |
| `frame-120x40.txt` | 120×40 | First size with the rail. Narrowest three-column frame |
| `frame-137x29.txt` | 137×29 | Non-standard width, short height |
| `frame-200x60.txt` | 200×60 | Wide reference. Full gutter, all blocks visible |
| `wide-runes-120x40.txt` | 120×40 | CJK / emoji / RTL / combining-mark / ZWJ fixture |
| `blocks-100x52.txt` | 100×52 | Block gallery: list, quote, link card, voice waveform, poll, code pane, media card, reactions |

Each file has two `#` comment lines and a `┄` rule above and below the frame.
Strip those three-plus-one lines to get the raw frame.

## How to assert against them

```go
func TestFrameGeometry(t *testing.T) {
    for _, tc := range []struct{ w, h int }{{80,24},{100,30},{120,40},{137,29},{200,60}} {
        got := strings.Split(renderFrame(t, tc.w, tc.h), "\n")
        want := loadFixture(t, fmt.Sprintf("frame-%dx%d.txt", tc.w, tc.h))
        if len(got) != tc.h { t.Fatalf("row count = %d, want %d", len(got), tc.h) }
        for i, line := range got {
            if n := uniseg.StringWidth(stripANSI(line)); n != tc.w {
                t.Errorf("row %d width = %d, want %d: %q", i+1, n, tc.w, line)
            }
            if stripANSI(line) != want[i] {
                t.Errorf("row %d:\n got %q\nwant %q", i+1, stripANSI(line), want[i])
            }
        }
    }
}
```

**The width assertion is the one that must pass on day one.** The string
equality is the design contract: expect to regenerate the fixtures when copy
changes, but never when geometry does — a geometry diff is a bug in the layout,
not stale goldens.

Width is measured with `github.com/rivo/uniseg` (`StringWidth`). The fixtures
were generated with the identical rule set:

- Combining marks, bidi controls, ZWJ, and variation selectors are **0** cells.
- East Asian Wide/Fullwidth and emoji presentation are **2** cells.
- Ambiguous-width glyphs (`●○▪▣▤▶│┌└─▌▁▂▃`) are assumed **1** cell. This holds
  in every terminal we care about; if a target terminal disagrees, that is a
  font/terminal bug, not a layout one — do not "fix" it by padding.

## Reading the plain-text goldens

They carry geometry, not color. Two substitutions were made so the frames stay
diffable as plain text:

| In the fixture | Actually rendered as |
|---|---|
| `[👍 3]` | reaction chip: 1 cell of padding each side, `border`-colored frame cells. Same 2 cells of width either way |
| `[4]` / `(31)` | unread badge: `bg` text on cyan; parens = the muted variant on `#39424b` |
| `▌` | selection / cursor bar, cyan when focused, ghost when not |
| `│` between panels | panel rule in `rule` (`#1f242b`) |

Everything else is literal. Colors are in README §2, and the region→role map is
in §3–§9.

## What the fixtures settled

Generating these surfaced three things the prose spec had wrong or vague. **The
fixtures are correct; README §5 has been amended to match.**

1. **The 24-col gutter does not survive a narrow thread pane.** At 120×40 with
   the rail on, the thread is 50 cols, leaving a 25-col body — unreadable.
   Amendment: when `threadWidth - 24 - 1 < 32`, the sender column compresses
   from 12 to 8 and the gutter to 20. Applies at 80×24 and 120×40 here.
2. **The media card needs a fallback.** The three-line framed card needs a
   40-col body. Below that it collapses to one line:
   `▣ auth-p95-260…  184 KB · png`. Visible in `frame-80x24.txt` and
   `frame-120x40.txt`.
3. **The code pane truncates horizontally, it does not wrap.** At a 29-col pane
   the SQL lines end in `…`. `y` still yanks the full untruncated text.

## Regenerating

The fixtures are generated output, not hand-drawn. Once the Go renderer exists
it becomes the generator: add a `-update` flag to the test that writes
`Model.View()` back out with ANSI stripped, and delete the reference copies.
Until then, treat them as read-only.
