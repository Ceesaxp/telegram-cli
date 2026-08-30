// Package cell is the single source of terminal geometry: measuring text in
// display cells and cutting or padding it to an exact width.
//
// Every layout decision in the UI must go through this package. The rule it
// enforces is that a terminal's unit is the *cell*, which is neither a byte,
// nor a rune, nor a grapheme:
//
//   - "四字" is 2 runes and 4 cells.
//   - A ZWJ family emoji is 7 runes, 1 grapheme, and 2 cells — but summing
//     runewidth.RuneWidth over its runes says 8. That mistake is what
//     produced the one defect found when the TUI 2.0 goldens were first
//     reviewed (docs/tui-2.0.md, decision 11).
//   - A styled string carries escape sequences that occupy no cells at all.
//
// [Width] handles all three. Measuring with len(), len([]rune(s)), or a
// per-rune width sum does not, and each failure mode shears a panel in a way
// that is invisible in ASCII-only testing.
//
// Rune indexing is still correct for things that are not geometry — cursor
// positions in a text buffer, Telegram's rune-indexed entity offsets, a
// parser's cursor. This package is about how wide something *draws*, not
// where it sits in a string.
package cell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Ellipsis is the marker appended by [Truncate] when it had to cut.
const Ellipsis = "…"

// Width returns the display width of a single line in terminal cells.
// ANSI escape sequences are ignored and grapheme clusters are measured as
// the terminal draws them.
//
// For multi-line text use [MaxWidth]; this counts a newline as nothing and
// so would under-report a block.
func Width(s string) int { return ansi.StringWidth(s) }

// MaxWidth returns the width of the widest line in possibly-multi-line
// text — the width of the box that would contain it.
func MaxWidth(s string) int {
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		if w := Width(line); w > widest {
			widest = w
		}
	}
	return widest
}

// Truncate clamps s to at most maxWidth display cells, appending an
// ellipsis when it actually had to cut something. It is ANSI-safe, so an
// already-styled string is not corrupted mid-escape.
//
// This is the primary truncation used for user-supplied text — chat titles,
// message previews, folder labels — where the reader benefits from seeing
// that something was elided.
func Truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if Width(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, Ellipsis)
}

// Clamp is [Truncate] without the ellipsis: a hard cap for cases where the
// cut is a safety net rather than something to show the reader, or where
// the caller has already added its own marker.
func Clamp(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if Width(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, "")
}

// ClampLeft removes width cells from the *start* of s, keeping the tail.
//
// This is what a single-line input field needs when the cursor has run past
// the right edge: the visible window slides right by dropping leading cells,
// so the cursor stays in view.
func ClampLeft(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.TruncateLeft(s, width, "")
}

// Wrap breaks s onto lines of at most width cells, preferring word
// boundaries. An unbroken token longer than width is hard-broken rather
// than allowed to overflow, since a row wider than its column shears the
// frame.
//
// Wrapping is ANSI-aware in the sense that escape sequences are not counted
// as cells and are not cut in half. It does NOT make each line
// independently styled — see [WrapLines], which is what a caller composing
// rows out of the result almost always wants.
func Wrap(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Wrap(s, width, "")
}

// WrapLines wraps s to width and returns lines that are each self-contained:
// a style spanning a break is closed at the end of one line and reopened at
// the start of the next.
//
// [Wrap] alone is not enough for anything drawn into a column. It leaves the
// opening sequence on the first line and the reset on the last, so the lines
// between carry no styling of their own — and a terminal does not reset at a
// newline. In a multi-column frame the rows of one panel are not adjacent on
// screen: whatever a body line leaves open bleeds through its own trailing
// padding, across the panel rule, and into the next column, for as many rows
// as it takes to reach the reset.
//
// This is the ONLY correct way to wrap styled text into rows. There is no
// version of the bug that shows up in a single-column layout, which is why
// it survives review so easily.
func WrapLines(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	lines := strings.Split(Wrap(s, width), "\n")

	open := ""
	for i, line := range lines {
		if open != "" {
			line = open + line
		}
		open = OpenStyle(line)
		if open != "" {
			line += "\x1b[0m"
		}
		lines[i] = line
	}
	return lines
}

// OpenStyle returns the SGR state left active at the end of s: the
// concatenation of the sequences that have not been cancelled by a reset,
// and "" when s closes everything it opens.
//
// [WrapLines] uses it to reopen a run on the next line. It is exported
// because "this row leaves no style open" is an invariant every component
// that draws into a column has to hold, and asserting it is the only way to
// catch a leak that is invisible in a single-column dump.
//
// Accumulating rather than keeping only the last sequence, because a styled
// run can be opened in pieces — lipgloss emits one combined sequence, but
// nested renders produce several — and dropping the earlier ones would
// reopen a continuation line in half its original style.
func OpenStyle(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return ""
	}

	var open strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			i++
			continue
		}
		j := i + 2
		for j < len(s) && s[j] != 'm' && s[j] != 0x1b {
			j++
		}
		if j >= len(s) || s[j] != 'm' {
			// Not an SGR sequence (a cursor move, say). Nothing here
			// styles text, so it cannot be part of the open state.
			i++
			continue
		}
		seq := s[i : j+1]
		if seq == "\x1b[0m" || seq == "\x1b[m" {
			open.Reset()
		} else {
			open.WriteString(seq)
		}
		i = j + 1
	}
	return open.String()
}

// Pad right-pads s with spaces to width cells. A string already at or over
// width is returned unchanged — callers wanting a hard cap should [Clamp]
// first, or use [Fit], which does both.
func Pad(s string, width int) string {
	w := Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// PadLeft left-pads s with spaces to width cells, i.e. right-aligns it in
// a field of that width. A string already at or over width is returned
// unchanged — [Truncate] first if it must also be capped.
func PadLeft(s string, width int) string {
	w := Width(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}

// Fit clamps s to exactly width cells: truncated without an ellipsis if
// wider, space-padded if narrower.
//
// This is the final guarantee for an assembled row whose individual field
// budgets are each best-effort — it is what makes "every line is exactly
// the frame width" true rather than aspirational.
func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return Pad(Clamp(s, width), width)
}

// FitLine renders s through style, clamped to exactly one line and at most
// totalWidth display cells — padded with the style's own
// background/whitespace if s is shorter.
//
// This exists to close a whole class of bug: lipgloss's Style.Width(w)
// treats w as the TOTAL rendered width, INCLUDING the style's own padding,
// and when content-plus-padding would exceed w, lipgloss WORD-WRAPS the
// content onto additional lines rather than truncating or overflowing (see
// lipgloss Style.Render: "if !inline && width > 0 { wrapAt := width -
// leftPadding - rightPadding; str = ansi.Wrap(str, wrapAt, "") }", applied
// before the padding is even added). Style.MaxWidth doesn't help either —
// it truncates each already-wrapped line individually, so it produces more
// short lines instead of fewer.
//
// Concretely: content built to be exactly totalWidth cells wide and then
// handed to a *padded* style's .Width(totalWidth) silently wraps instead of
// rendering as one line. That single misunderstanding produced four
// independent-looking bugs across this codebase: chat list rows growing past
// their fixed 2-line stride (desyncing row-index math), folder tab
// backgrounds wrapping so a "visible" tab was never actually painted, and
// the status bar hints line splitting mid-word. FitLine is the fix for all
// of them: it truncates s to totalWidth - style.GetHorizontalFrameSize()
// display cells BEFORE handing it to style.Width, so the internal word-wrap
// never has anything to wrap.
//
// s is assumed to be single-line content (no embedded "\n"); render each
// line of a multi-line row through FitLine separately and join with "\n"
// rather than passing multi-line input in.
//
// Note: if totalWidth is smaller than style.GetHorizontalFrameSize(),
// lipgloss cannot shrink the style's padding below its natural size, so the
// result can exceed totalWidth in that degenerate case. No panel in this app
// is ever that narrow.
func FitLine(style lipgloss.Style, s string, totalWidth int) string {
	if totalWidth <= 0 {
		return ""
	}
	budget := totalWidth - style.GetHorizontalFrameSize()
	if budget < 0 {
		budget = 0
	}
	return style.Width(totalWidth).Render(Clamp(s, budget))
}
