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

// TestSafeLinkURIRefusesWhatATerminalShouldNotBeAskedToOpen.
//
// The unit-level half of the rule; internal/render covers what a rendered
// message does with it. Both exist because this is the one function whose
// input is written by a stranger and whose output goes inside a control
// sequence.
func TestSafeLinkURIRefusesWhatATerminalShouldNotBeAskedToOpen(t *testing.T) {
	for _, tc := range []struct {
		name, uri, want string
		ok              bool
	}{
		{"https", "https://example.com/a", "https://example.com/a", true},
		{"http", "http://example.com", "http://example.com", true},
		{"mailto", "mailto:a@example.com", "mailto:a@example.com", true},
		{"tg", "tg://resolve?domain=x", "tg://resolve?domain=x", true},
		{"scheme case is not part of the scheme", "HtTpS://example.com", "https://example.com", true},

		{"file", "file:///etc/passwd", "", false},
		{"javascript", "javascript:alert(1)", "", false},
		{"data", "data:text/html,<script>", "", false},
		{"anything else", "vscode://x/y", "", false},
		{"no scheme", "example.com", "", false},
		{"empty", "", "", false},
		{"not a URI at all", "://///", "", false},
		// url.Parse refuses ASCII control bytes rather than letting them
		// through to be encoded, which is the safer of the two and worth
		// pinning: sanitizeTerminal strips them upstream, and this is what
		// happens if it ever stops.
		{"a control byte in the path", "https://example.com/\ttab", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SafeLinkURI(tc.uri)
			if ok != tc.ok {
				t.Fatalf("SafeLinkURI(%q) ok = %v, want %v", tc.uri, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("SafeLinkURI(%q) = %q, want %q", tc.uri, got, tc.want)
			}
		})
	}
}

// TestSafeLinkURIIsPrintableASCII: OSC 8 can carry nothing else.
//
// The QUERY cases are the ones that matter. url.String encodes a path and a
// fragment and stops — a query string, a mailto's opaque part and a tg://
// query all come back with their raw bytes — and those are precisely where a
// Telegram link carries non-ASCII. A test that only exercised paths passed
// against a build with no encoder at all, which is how this list got its
// second half.
func TestSafeLinkURIIsPrintableASCII(t *testing.T) {
	for _, uri := range []string{
		"https://example.com/a b",
		"https://example.com/café",
		"https://example.com/π",
		"https://example.com/search?q=café",
		"https://example.com/search?q=日本語&n=1",
		"mailto:café@example.com",
		"tg://resolve?domain=café",
	} {
		got, ok := SafeLinkURI(uri)
		if !ok {
			t.Errorf("SafeLinkURI(%q) refused it outright", uri)
			continue
		}
		for i := range len(got) {
			if c := got[i]; c < 0x21 || c > 0x7e {
				t.Errorf("SafeLinkURI(%q) = %q, byte %#x at %d is not printable ASCII", uri, got, c, i)
				break
			}
		}
	}
}

// TestTheLengthCapIsEnforcedAfterEncoding, not before: percent-encoding is
// what makes a URI longer, so a cap applied to the input would let an
// encoded one past it.
func TestTheLengthCapIsEnforcedAfterEncoding(t *testing.T) {
	// In the QUERY, where url.String leaves the bytes alone: 700 two-byte
	// runes are 1400 bytes raw and 4200 encoded, so the cap sees a different
	// number depending on when it looks.
	uri := "https://example.com/x?q="
	for range 700 {
		uri += "é"
	}
	if len(uri) > maxLinkURI {
		t.Fatalf("precondition: the raw URI is already %d bytes", len(uri))
	}
	if _, ok := SafeLinkURI(uri); ok {
		t.Error("a URI that only exceeds the cap once encoded was accepted")
	}
}

// TestLinkKeepsTheTextWhenItRefusesTheURI. The refusal must not cost the
// reader the words.
func TestLinkKeepsTheTextWhenItRefusesTheURI(t *testing.T) {
	const text = "the docs"

	if got := Link("javascript:alert(1)", text); got != text {
		t.Errorf("Link with a refused URI = %q, want the bare text", got)
	}
	if got := Link("", text); got != text {
		t.Errorf("Link with no URI = %q, want the bare text", got)
	}
	got := Link("https://example.com", text)
	if got == text {
		t.Error("Link with a good URI emitted no sequence")
	}
	if !strings.Contains(got, text) {
		t.Errorf("Link dropped the text: %q", got)
	}
}
