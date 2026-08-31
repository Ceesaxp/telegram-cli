package cell

import (
	"strings"
	"testing"
)

const uri = "https://example.com/a/fairly/long/path"

func readable(s string) string {
	s = strings.ReplaceAll(s, "\x1b\\", "ST")
	s = strings.ReplaceAll(s, "\x1b", "ESC")
	return strings.ReplaceAll(s, "\a", "BEL")
}

func TestOpenLinkReportsTheSequenceLeftOpen(t *testing.T) {
	open := "\x1b]8;;" + uri + "\x1b\\"

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"nothing at all", "plain text", ""},
		{"opened and left open", "a" + open + "b", open},
		{"opened and closed", "a" + open + "b" + LinkClose, ""},
		{"closed then reopened", open + "a" + LinkClose + open + "b", open},
		{"a second link replaces the first",
			open + "a\x1b]8;;https://other\x1b\\b", "\x1b]8;;https://other\x1b\\"},
		{"BEL terminator", "a\x1b]8;;https://e.com\ab", "\x1b]8;;https://e.com\a"},
		{"params before the uri", "a\x1b]8;id=7;https://e.com\x1b\\b",
			"\x1b]8;id=7;https://e.com\x1b\\"},
		{"a different OSC is not a link", "a\x1b]0;window title\x1b\\b", ""},
		{"unterminated", "a\x1b]8;;https://", ""},
		{"SGR is not a link", "\x1b[38;5;73mabc\x1b[0m", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OpenLink(tt.in); got != tt.want {
				t.Errorf("OpenLink(%s) = %s, want %s",
					readable(tt.in), readable(got), readable(tt.want))
			}
		})
	}
}

// The defect divergence 14 recorded, and the reason it said OSC 8 could not
// ship: `ansi.Wrap` breaks between a link's open and its close and repairs
// neither, so the first row's trailing padding — and the panel rule, and the
// column beside it — become part of the link.
func TestWrapLinesClosesAndReopensAHyperlink(t *testing.T) {
	s := "before " + Link(uri, "the link text runs on for a while") + " after"
	lines := WrapLines(s, 20)

	if len(lines) < 2 {
		t.Fatalf("want at least two lines, got %d", len(lines))
	}
	for i, line := range lines {
		if open := OpenLink(line); open != "" {
			t.Errorf("line %d leaves a hyperlink open (%s):\n%s",
				i, readable(open), readable(line))
		}
	}

	// The middle of the link still IS a link, not merely un-leaked.
	if !strings.Contains(lines[1], uri) {
		t.Errorf("the continuation line did not reopen the link:\n%s", readable(lines[1]))
	}
	// And the text is unharmed.
	var plain strings.Builder
	for _, line := range lines {
		plain.WriteString(line)
	}
	for _, want := range []string{"before", "the link text", "runs on for a while", "after"} {
		if !strings.Contains(plain.String(), want) {
			t.Errorf("wrapping lost %q", want)
		}
	}
}

// A hyperlink occupies no cells, so it must not change where the wrap falls.
// If it did, a link in a message body would shift the grid's body column.
func TestAHyperlinkCostsNoCells(t *testing.T) {
	plain := "before the link text runs on for a while after"
	linked := "before " + Link(uri, "the link text runs on for a while") + " after"

	if got, want := Width(linked), Width(plain); got != want {
		t.Errorf("Width with a link = %d, without = %d", got, want)
	}

	pLines, lLines := WrapLines(plain, 20), WrapLines(linked, 20)
	if len(pLines) != len(lLines) {
		t.Fatalf("wrapped to %d lines with a link, %d without", len(lLines), len(pLines))
	}
	for i := range pLines {
		if got, want := Width(lLines[i]), Width(pLines[i]); got != want {
			t.Errorf("line %d is %d cells with a link, %d without", i, got, want)
		}
	}
}

// Styles and links are independent modes and both have to survive a break.
func TestWrapLinesRepairsAStyleAndALinkTogether(t *testing.T) {
	s := "\x1b[38;5;73m" + Link(uri, "a styled link long enough to wrap twice over") + "\x1b[0m"
	lines := WrapLines(s, 16)

	if len(lines) < 3 {
		t.Fatalf("want at least three lines, got %d", len(lines))
	}
	for i, line := range lines {
		if open := OpenStyle(line); open != "" {
			t.Errorf("line %d leaves style %q open", i, open)
		}
		if open := OpenLink(line); open != "" {
			t.Errorf("line %d leaves a link open", i)
		}
		if !strings.Contains(line, "38;5;73") {
			t.Errorf("line %d lost its colour:\n%s", i, readable(line))
		}
		if !strings.Contains(line, uri) {
			t.Errorf("line %d lost its link:\n%s", i, readable(line))
		}
	}
}

func TestLinkRefusesAnEmptyURI(t *testing.T) {
	// An empty URI is the CLOSING sequence. Emitting one that closes nothing
	// is how a link leaks in the other direction — every following cell on
	// the row would be un-linked, which is fine, but a terminal that tracks
	// nesting sees a stray close.
	if got := Link("", "text"); got != "text" {
		t.Errorf("Link(\"\", ...) = %s, want the text unchanged", readable(got))
	}
}
