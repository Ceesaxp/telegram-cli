package cell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// --- Measurement --------------------------------------------------------

// TestWidthCountsCellsNotRunes is the premise the whole package rests on.
// Each case is a shape that a len() or per-rune measurement gets wrong, and
// each one shears a panel in a different way.
func TestWidthCountsCellsNotRunes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"ascii", "abcd", 4},
		{"cjk is double width", "四字", 4},
		{"cjk mixed with ascii", "a四b", 4},
		// 7 runes, 1 grapheme, 2 cells. Summing runewidth.RuneWidth over
		// the runes gives 8 — the mistake that mis-padded a golden.
		{"zwj family emoji", "\U0001F468‍\U0001F469‍\U0001F467‍\U0001F466", 2},
		{"combining marks add nothing", "é", 1},
		{"ansi escapes occupy no cells", "\x1b[38;5;73mabcd\x1b[0m", 4},
		{"empty", "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Width(tc.in); got != tc.want {
				t.Errorf("Width(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestMaxWidthMeasuresTheWidestLine: Width sees a newline as nothing, so a
// block measured with it would be reported far too narrow. Anything sizing
// a box around multi-line content needs MaxWidth.
func TestMaxWidthMeasuresTheWidestLine(t *testing.T) {
	block := "ab\nabcdef\nabc"
	if got := MaxWidth(block); got != 6 {
		t.Errorf("MaxWidth = %d, want 6", got)
	}
	if got := MaxWidth("四字\nab"); got != 4 {
		t.Errorf("MaxWidth with wide runes = %d, want 4", got)
	}
	// Single-line input must agree with Width, or callers switching
	// between them would see a silent change.
	if MaxWidth("abcd") != Width("abcd") {
		t.Error("MaxWidth and Width disagree on single-line input")
	}
}

// --- Cutting and padding ------------------------------------------------

func TestTruncate(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		max        int
		want       string
		wantAtMost int
	}{
		{"under budget is untouched", "abc", 10, "abc", 10},
		{"exactly at budget is untouched", "abcd", 4, "abcd", 4},
		{"over budget gains an ellipsis", "abcdef", 4, "abc" + Ellipsis, 4},
		{"zero width is empty", "abc", 0, "", 0},
		{"negative width is empty", "abc", -3, "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.in, tc.max)
			if got != tc.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
			if w := Width(got); w > tc.wantAtMost {
				t.Errorf("Truncate(%q, %d) width = %d, want <= %d", tc.in, tc.max, w, tc.wantAtMost)
			}
		})
	}
}

// TestTruncateNeverExceedsBudgetOnWideRunes is the case a rune-counting
// truncate gets wrong in the dangerous direction: it would keep 3 CJK
// runes for a 3-cell budget and draw 6 cells, overflowing the column.
//
// The emoji case came from help.truncatePlain's own test, which moved here
// when that duplicate was folded into cell.Truncate.
func TestTruncateNeverExceedsBudgetOnWideRunes(t *testing.T) {
	for _, wide := range []string{
		"四字熟語入門",
		"📢📢📢📢📢", // 5 runes, 10 display cells
	} {
		for budget := 1; budget <= 12; budget++ {
			if w := Width(Truncate(wide, budget)); w > budget {
				t.Errorf("Truncate(%q, %d) width = %d, want <= %d", wide, budget, w, budget)
			}
		}
	}
}

func TestClampAddsNoEllipsis(t *testing.T) {
	if got := Clamp("abcdef", 4); got != "abcd" {
		t.Errorf("Clamp = %q, want %q", got, "abcd")
	}
	if got := Clamp("abc", 10); got != "abc" {
		t.Errorf("Clamp under budget = %q, want %q", got, "abc")
	}
	if got := Clamp("abc", 0); got != "" {
		t.Errorf("Clamp(_, 0) = %q, want empty", got)
	}
}

// TestClampLeftKeepsTheTail: the input-field case, where the window slides
// right so the cursor stays visible. Cells come off the front.
func TestClampLeftKeepsTheTail(t *testing.T) {
	if got := ClampLeft("abcdef", 2); got != "cdef" {
		t.Errorf("ClampLeft = %q, want %q", got, "cdef")
	}
	// A non-positive amount must be a no-op, not an empty string — the
	// caller computes an overflow that is frequently zero.
	if got := ClampLeft("abcdef", 0); got != "abcdef" {
		t.Errorf("ClampLeft(_, 0) = %q, want it unchanged", got)
	}
	// Cells, not runes: dropping 2 cells from a double-width leading rune
	// removes that one rune.
	if w := Width(ClampLeft("四字ab", 2)); w != 4 {
		t.Errorf("ClampLeft(wide, 2) width = %d, want 4", w)
	}
}

func TestWrapRespectsWidth(t *testing.T) {
	for _, line := range strings.Split(Wrap("the quick brown fox jumps", 10), "\n") {
		if w := Width(line); w > 10 {
			t.Errorf("wrapped line %q has width %d > 10", line, w)
		}
	}
}

// TestWrapHardBreaksUnbreakableTokens: a URL longer than the column has no
// word boundary to break on, and letting it overflow would shear the frame.
func TestWrapHardBreaksUnbreakableTokens(t *testing.T) {
	long := strings.Repeat("x", 40)
	out := Wrap(long, 10)
	if !strings.Contains(out, "\n") {
		t.Fatal("an over-long token was not broken at all")
	}
	for _, line := range strings.Split(out, "\n") {
		if w := Width(line); w > 10 {
			t.Errorf("wrapped line %q has width %d > 10", line, w)
		}
	}
}

func TestWrapZeroWidth(t *testing.T) {
	if got := Wrap("abc", 0); got != "" {
		t.Errorf("Wrap(_, 0) = %q, want empty", got)
	}
}

func TestPad(t *testing.T) {
	if got := Pad("ab", 5); got != "ab   " {
		t.Errorf("Pad = %q, want %q", got, "ab   ")
	}
	// Wide runes must be padded by cells, not runes: "四字" is 2 runes but
	// 4 cells, so it needs 1 space to reach 5, not 3.
	if got := Pad("四字", 5); got != "四字 " {
		t.Errorf("Pad(wide) = %q, want %q", got, "四字 ")
	}
	// Over-width input is returned unchanged; Pad does not cut.
	if got := Pad("abcdef", 3); got != "abcdef" {
		t.Errorf("Pad over width = %q, want it unchanged", got)
	}
}

// TestFitIsExact is the property the frame depends on: whatever goes in,
// exactly width cells come out.
func TestFitIsExact(t *testing.T) {
	inputs := []string{
		"", "a", "abcd", "abcdefghij",
		"四字", "四字熟語入門",
		"\U0001F468‍\U0001F469‍\U0001F467‍\U0001F466",
		"écombining",
		"\x1b[31mstyled\x1b[0m",
	}
	for _, in := range inputs {
		for _, width := range []int{1, 2, 3, 5, 8, 20} {
			got := Fit(in, width)
			if w := Width(got); w != width {
				t.Errorf("Fit(%q, %d) width = %d, want exactly %d (%q)", in, width, w, width, got)
			}
		}
	}
}

func TestFitZeroWidth(t *testing.T) {
	if got := Fit("abc", 0); got != "" {
		t.Errorf("Fit(_, 0) = %q, want empty", got)
	}
	if got := Fit("abc", -1); got != "" {
		t.Errorf("Fit(_, -1) = %q, want empty", got)
	}
}

// --- FitLine ------------------------------------------------------------
//
// These moved here with the function itself. They are the regression tests
// for the lipgloss Style.Width wrap trap described in FitLine's doc
// comment, exercised against the real shipped theme styles, because the bug
// is invisible with zero-value styles that carry no padding.

// TestFitLineNeverWraps is the direct regression test for the root-cause
// class named in adversarial review: content built to fill totalWidth,
// then rendered through a real, padded, SHIPPED theme style via a bare
// style.Width(totalWidth) call, silently word-wraps onto extra lines
// instead of rendering as one line — because lipgloss.Style.Width treats
// its argument as the TOTAL width including the style's own padding.
// FitLine must never do this, for any padded style. The shapes below are
// the ones the bug class actually hit — chat list rows, folder tabs, the
// status bar, the unread badge — reduced to what made them dangerous, which
// is the padding and nothing else. They were taken from a shipped theme
// until that theme was deleted; naming the padding directly is a better
// test anyway, since the property is "a style with a frame", not "these six
// styles".
func TestFitLineNeverWraps(t *testing.T) {
	styles := map[string]lipgloss.Style{
		"one cell either side":  paddedStyle(0, 1),
		"two cells either side": paddedStyle(0, 2),
		"asymmetric":            lipgloss.NewStyle().PaddingLeft(2).PaddingRight(1),
		"no padding at all":     lipgloss.NewStyle(),
	}

	for name, style := range styles {
		for _, width := range []int{6, 10, 20, 28, 80} {
			// Deliberately fill the *entire* width before the style's own
			// padding is even considered — exactly the shape of content
			// that triggered the wrap bug at every one of the four sites.
			content := strings.Repeat("x", width)
			out := FitLine(style, content, width)

			if n := lipgloss.Height(out); n != 1 {
				t.Errorf("%s width=%d: FitLine produced %d lines, want 1: %q", name, width, n, out)
			}
			if w := ansi.StringWidth(out); w > width {
				t.Errorf("%s width=%d: FitLine output has display width %d > %d: %q", name, width, w, width, out)
			}
		}
	}
}

// TestFitLineNeverWrapsOnWideRunes is the same trap reached through width
// rather than character count: content that is under budget in runes but
// over it in cells.
func TestFitLineNeverWrapsOnWideRunes(t *testing.T) {
	for _, width := range []int{6, 10, 20, 28} {
		content := strings.Repeat("四", width) // width runes, 2*width cells
		out := FitLine(paddedStyle(0, 1), content, width)

		if n := lipgloss.Height(out); n != 1 {
			t.Errorf("width=%d: produced %d lines, want 1: %q", width, n, out)
		}
		if w := ansi.StringWidth(out); w > width {
			t.Errorf("width=%d: display width %d > %d: %q", width, w, width, out)
		}
	}
}

// TestFitLinePadsShortContent checks short content is padded out to
// exactly totalWidth (through the style's own background/whitespace),
// not left short.
func TestFitLinePadsShortContent(t *testing.T) {
	out := FitLine(paddedStyle(0, 1), "hi", 20)
	if w := ansi.StringWidth(out); w != 20 {
		t.Fatalf("FitLine(ChatListItem, \"hi\", 20) has display width %d, want exactly 20: %q", w, out)
	}
	if n := lipgloss.Height(out); n != 1 {
		t.Fatalf("FitLine(ChatListItem, \"hi\", 20) produced %d lines, want 1", n)
	}
}

// TestFitLineTruncatesOverBudgetContent checks wildly over-budget content
// is truncated to fit, not wrapped or left overflowing.
func TestFitLineTruncatesOverBudgetContent(t *testing.T) {
	out := FitLine(paddedStyle(0, 1), strings.Repeat("y", 200), 40)
	if w := ansi.StringWidth(out); w > 40 {
		t.Fatalf("FitLine over-budget content has display width %d, want <= 40: %q", w, out)
	}
	if n := lipgloss.Height(out); n != 1 {
		t.Fatalf("FitLine over-budget content produced %d lines, want 1: %q", n, out)
	}
}

// TestFitLineZeroWidth checks the degenerate zero/negative width case
// doesn't panic and returns something empty rather than garbage.
func TestFitLineZeroWidth(t *testing.T) {
	if got := FitLine(paddedStyle(0, 1), "hi", 0); got != "" {
		t.Fatalf("FitLine(..., 0) = %q, want empty", got)
	}
	if got := FitLine(paddedStyle(0, 1), "hi", -5); got != "" {
		t.Fatalf("FitLine(..., -5) = %q, want empty", got)
	}
}

// paddedStyle is a style with a frame, which is the only property of a
// style FitLine has to survive.
func paddedStyle(vertical, horizontal int) lipgloss.Style {
	return lipgloss.NewStyle().Padding(vertical, horizontal)
}
