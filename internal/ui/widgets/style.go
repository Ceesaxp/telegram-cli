package widgets

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// FitLine renders s through style, clamped to exactly one line and at
// most totalWidth display cells (ansi.StringWidth) — padded with the
// style's own background/whitespace if s is shorter.
//
// This exists to close a whole class of bug: lipgloss's Style.Width(w)
// treats w as the TOTAL rendered width, INCLUDING the style's own
// padding, and when content-plus-padding would exceed w, lipgloss
// WORD-WRAPS the content onto additional lines rather than truncating
// or overflowing (see charmbracelet/lipgloss@v0.12.1/style.go's Render:
// "if !inline && width > 0 { wrapAt := width - leftPadding -
// rightPadding; str = ansi.Wrap(str, wrapAt, "") }", applied before the
// padding is even added). Style.MaxWidth doesn't help either — it
// truncates each already-wrapped line individually, so it produces more
// short lines instead of fewer.
//
// Concretely: content built to be exactly totalWidth cells wide and then
// handed to a *padded* style's .Width(totalWidth) silently wraps instead
// of rendering as one line. That single misunderstanding produced four
// independent-looking bugs across this codebase: chat list rows growing
// past their fixed 2-line stride (desyncing row-index math), folder tab
// backgrounds wrapping so a "visible" tab was never actually painted,
// and the status bar hints line splitting mid-word. FitLine is the fix
// for all of them: it truncates s to totalWidth -
// style.GetHorizontalFrameSize() display cells (via ansi.Truncate, which
// is ANSI-escape-safe so already-colored/styled input isn't corrupted)
// BEFORE handing it to style.Width, so the internal word-wrap never has
// anything to wrap.
//
// s is assumed to be single-line content (no embedded "\n"); render each
// line of a multi-line row through FitLine separately and join with "\n"
// rather than passing multi-line input in.
//
// Note: if totalWidth is smaller than style.GetHorizontalFrameSize(),
// lipgloss cannot shrink the style's padding below its natural size, so
// the result can exceed totalWidth in that degenerate case. No panel in
// this app is ever that narrow.
func FitLine(style lipgloss.Style, s string, totalWidth int) string {
	if totalWidth <= 0 {
		return ""
	}
	budget := totalWidth - style.GetHorizontalFrameSize()
	if budget < 0 {
		budget = 0
	}
	if ansi.StringWidth(s) > budget {
		s = ansi.Truncate(s, budget, "")
	}
	return style.Width(totalWidth).Render(s)
}
