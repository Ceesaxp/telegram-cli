package cell

import "testing"

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
	if !contains(got, text) {
		t.Errorf("Link dropped the text: %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
