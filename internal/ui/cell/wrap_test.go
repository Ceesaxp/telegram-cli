package cell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Escapes here are written by hand rather than through lipgloss: the point
// of these tests is the byte-level shape of the output, and lipgloss renders
// to whatever colour profile the test binary happens to resolve.
const (
	bold  = "\x1b[1m"
	green = "\x1b[38;5;42m"
	reset = "\x1b[0m"
)

// TestWrapLinesReopensAStyleOnEveryLine is the bug WrapLines exists to fix.
//
// ansi.Wrap leaves the opening sequence on the first line and the reset on
// the last, and a terminal does not reset at a newline. In a multi-column
// frame the rows of one panel are not adjacent on screen, so whatever a body
// line leaves open bleeds through its trailing padding, across the panel
// rule, and into the next column.
func TestWrapLinesReopensAStyleOnEveryLine(t *testing.T) {
	styled := "plain " + green + "one two three four five six" + reset + " tail"

	lines := WrapLines(styled, 12)
	if len(lines) < 3 {
		t.Fatalf("test needs several wrapped lines, got %d", len(lines))
	}

	for i, line := range lines {
		if open := OpenStyle(line); open != "" {
			t.Errorf("line %d leaves %q open: %q", i, open, line)
		}
	}
	// Every line carrying styled words must carry the style, not just the
	// first one.
	for i, line := range lines {
		text := ansi.Strip(line)
		if !strings.Contains(text, "one") && !strings.Contains(text, "two") &&
			!strings.Contains(text, "three") && !strings.Contains(text, "four") &&
			!strings.Contains(text, "five") && !strings.Contains(text, "six") {
			continue
		}
		if !strings.Contains(line, green) {
			t.Errorf("line %d carries styled text with no style: %q", i, line)
		}
	}
}

// TestWrapLinesLeavesPlainTextAlone: an unstyled body must not grow escape
// sequences it never had.
func TestWrapLinesLeavesPlainTextAlone(t *testing.T) {
	for _, line := range WrapLines(strings.Repeat("word ", 12), 15) {
		if strings.Contains(line, "\x1b") {
			t.Fatalf("plain text gained an escape sequence: %q", line)
		}
	}
}

// TestWrapLinesMeasuresTheSameAsWrap: reopening a style must not cost cells.
func TestWrapLinesMeasuresTheSameAsWrap(t *testing.T) {
	styled := bold + strings.Repeat("word ", 12) + reset
	for i, line := range WrapLines(styled, 15) {
		if got := Width(line); got > 15 {
			t.Fatalf("line %d is %d cells: %q", i, got, line)
		}
	}
}

func TestWrapLinesZeroWidth(t *testing.T) {
	if got := WrapLines("anything", 0); got != nil {
		t.Fatalf("expected nil at zero width, got %q", got)
	}
}

func TestOpenStyle(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"plain":            {"nothing here", ""},
		"closed":           {bold + "x" + reset, ""},
		"open":             {bold + "x", bold},
		"reopened":         {bold + "x" + reset + green + "y", green},
		"two open":         {bold + green + "x", bold + green},
		"short reset":      {bold + "x" + "\x1b[m", ""},
		"unterminated":     {"x\x1b[1", ""},
		"non-SGR is inert": {"\x1b[2Jx", ""},
	}
	for name, tc := range cases {
		if got := OpenStyle(tc.in); got != tc.want {
			t.Errorf("%s: OpenStyle(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}
}
