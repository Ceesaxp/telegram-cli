package render

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/charmbracelet/x/ansi"
)

func linked(text string, entities ...*telegram.TextEntity) *telegram.FormattedText {
	return &telegram.FormattedText{Text: text, Entities: entities}
}

func entity(off, length int32, t telegram.TextEntityType) *telegram.TextEntity {
	return &telegram.TextEntity{Offset: off, Length: length, Type: t}
}

func TestLinkEntitiesCarryTheirDestination(t *testing.T) {
	tests := []struct {
		name string
		ft   *telegram.FormattedText
		want string
	}{
		{
			"a text_url uses its own field",
			linked("see the docs", entity(4, 8, &telegram.TextEntityTypeTextURL{URL: "https://example.com/docs"})),
			"https://example.com/docs",
		},
		{
			"a bare url is its own destination",
			linked("go to https://example.com/x now", entity(6, 21, &telegram.TextEntityTypeURL{})),
			"https://example.com/x",
		},
		{
			"a schemeless url gets https",
			linked("go to example.com now", entity(6, 11, &telegram.TextEntityTypeURL{})),
			"https://example.com",
		},
		{
			"an email becomes a mailto",
			linked("write to a@b.com now", entity(9, 7, &telegram.TextEntityTypeEmailAddress{})),
			"mailto:a@b.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderInline(tt.ft, testRoles(), textOpts{links: true})
			if !strings.Contains(got, "\x1b]8;;"+tt.want+"\x1b\\") {
				t.Errorf("no link to %q in:\n%s", tt.want, readable(got))
			}
			if open := cell.OpenLink(got); open != "" {
				t.Errorf("the link was left open: %s", readable(open))
			}
		})
	}
}

// A hyperlink whose target disagrees with its visible text is the shape of a
// phishing link. The only two sources allowed are the entity's own field and
// the text it covers, so there is nowhere for a third URL to come from.
func TestAMentionIsNotLinked(t *testing.T) {
	ft := linked("hello @someone", entity(6, 8, &telegram.TextEntityTypeMention{}))
	got := RenderInline(ft, testRoles(), textOpts{links: true})
	if strings.Contains(got, "\x1b]8;") {
		t.Errorf("a mention was rendered as a hyperlink:\n%s", readable(got))
	}
}

func TestHyperlinksAreOffUnlessAskedFor(t *testing.T) {
	ft := linked("see the docs", entity(4, 8, &telegram.TextEntityTypeTextURL{URL: "https://example.com"}))

	off := RenderInline(ft, testRoles(), textOpts{})
	if strings.Contains(off, "\x1b]8;") {
		t.Errorf("OSC 8 emitted with links off:\n%s", readable(off))
	}
	// The affordance is the cyan underline, and it is there either way —
	// OSC 8 only adds the click.
	if !strings.Contains(off, "4m") && !strings.Contains(off, ";4") {
		t.Errorf("the link is not underlined without OSC 8:\n%s", readable(off))
	}
}

// The composer preview draws what WILL be sent. A clickable link in a draft
// invites a click on something that does not exist yet.
func TestTheComposerPreviewEmitsNoHyperlinks(t *testing.T) {
	ft := linked("see the docs", entity(4, 8, &telegram.TextEntityTypeTextURL{URL: "https://example.com"}))
	for _, line := range RenderText(ft, testRoles(), 40) {
		if strings.Contains(line, "\x1b]8;") {
			t.Errorf("the preview emitted OSC 8:\n%s", readable(line))
		}
	}
}

// The reason divergence 14 held OSC 8 back, now the reason it can ship: a
// link that wraps must close on each line and reopen on the next, or the
// row's padding and the panel rule beside it join the link.
func TestALinkWrappedAcrossBodyLinesLeaksNothing(t *testing.T) {
	const uri = "https://example.com/a/quite/long/destination"
	text := "start " + strings.Repeat("linkword ", 8) + "end"
	ft := linked(text, entity(6, int32(len([]rune(text))-6-4), &telegram.TextEntityTypeTextURL{URL: uri}))

	lines := renderBlocks(ft, testRoles(), 24, textOpts{links: true})
	if len(lines) < 3 {
		t.Fatalf("want a link spanning several lines, got %d", len(lines))
	}

	var linkedLines int
	for i, line := range lines {
		if open := cell.OpenLink(line); open != "" {
			t.Errorf("body line %d leaves a hyperlink open:\n%s", i, readable(line))
		}
		if strings.Contains(line, uri) {
			linkedLines++
		}
		if w := ansi.StringWidth(line); w > 24 {
			t.Errorf("body line %d is %d cells, over the 24 budget", i, w)
		}
	}
	if linkedLines < 2 {
		t.Errorf("the link was reopened on %d lines, want at least 2", linkedLines)
	}
}

// A link costs no cells, so turning hyperlinks on must not move a single
// word. If it did, the same message would wrap differently on two terminals.
func TestHyperlinksDoNotChangeTheLayout(t *testing.T) {
	const uri = "https://example.com/a/quite/long/destination"
	text := "start " + strings.Repeat("linkword ", 8) + "end"
	ft := linked(text, entity(6, int32(len([]rune(text))-6-4), &telegram.TextEntityTypeTextURL{URL: uri}))

	plain := renderBlocks(ft, testRoles(), 24, textOpts{})
	withLinks := renderBlocks(ft, testRoles(), 24, textOpts{links: true})

	if len(plain) != len(withLinks) {
		t.Fatalf("%d lines with links, %d without", len(withLinks), len(plain))
	}
	for i := range plain {
		if got, want := ansi.Strip(withLinks[i]), ansi.Strip(plain[i]); got != want {
			t.Errorf("line %d differs:\n with links: %q\n without:    %q", i, got, want)
		}
	}
}

func readable(s string) string {
	s = strings.ReplaceAll(s, "\x1b\\", "ST")
	return strings.ReplaceAll(s, "\x1b", "ESC")
}

// TestATerminalIsOnlyAskedToOpenSchemesAMessageCouldMean.
//
// A terminal hands an OSC 8 URI straight to the platform opener, so the
// SCHEME decides what runs when the reader clicks — and the URI came from
// whoever sent the message. sanitizeTerminal strips the bytes that would
// break out of the sequence; this is about what happens once the sequence
// parses correctly.
//
// The refusal keeps the styling. The run still reads as a link, because it
// is one and saying otherwise would hide where it claims to go; it just
// cannot be clicked into the platform opener.
func TestATerminalIsOnlyAskedToOpenSchemesAMessageCouldMean(t *testing.T) {
	for _, tc := range []struct {
		name, uri string
		clickable bool
	}{
		{"https", "https://example.com/docs", true},
		{"http", "http://example.com", true},
		{"mailto", "mailto:someone@example.com", true},
		{"tg", "tg://resolve?domain=telegram", true},
		{"file", "file:///etc/passwd", false},
		{"javascript", "javascript:alert(1)", false},
		{"a scheme nobody has heard of", "custom-handler://run/this", false},
		{"no scheme at all", "example.com/docs", false},
		{"upper-case is still the scheme", "HTTPS://example.com", true},
		{"upper-case does not smuggle one in", "FILE:///etc/passwd", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ft := linked("see the docs", entity(4, 8, &telegram.TextEntityTypeTextURL{URL: tc.uri}))
			got := RenderInline(ft, testRoles(), textOpts{links: true})

			if clickable := strings.Contains(got, "\x1b]8;;"); clickable != tc.clickable {
				t.Errorf("%s: clickable=%v, want %v:\n%s", tc.uri, clickable, tc.clickable, readable(got))
			}
			// Either way it is still styled as a link.
			if !strings.Contains(got, "4m") && !strings.Contains(got, ";4") {
				t.Errorf("%s: the run lost its underline:\n%s", tc.uri, readable(got))
			}
			if !strings.Contains(ansi.Strip(got), "the docs") {
				t.Errorf("%s: the visible text was dropped", tc.uri)
			}
		})
	}
}

// TestAnEmittedURIIsPrintableASCII. OSC 8 has no way to carry anything else,
// and a terminal handed a raw byte may truncate the sequence, ignore it, or
// misparse it — and a truncated URI is a link to somewhere nobody wrote.
func TestAnEmittedURIIsPrintableASCII(t *testing.T) {
	for _, tc := range []struct{ name, uri, want string }{
		{"a space", "https://example.com/a b", "https://example.com/a%20b"},
		{"non-ASCII", "https://example.com/café", "https://example.com/caf%C3%A9"},
		{"already encoded stays put", "https://example.com/a%20b", "https://example.com/a%20b"},
		// url.String leaves a query alone, so this is the half that needs
		// the encoder rather than the half net/url already covers.
		{"non-ASCII in a query", "https://example.com/s?q=café", "https://example.com/s?q=caf%C3%A9"},
		{"a tg link's query", "tg://resolve?domain=café", "tg://resolve?domain=caf%C3%A9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ft := linked("see the docs", entity(4, 8, &telegram.TextEntityTypeTextURL{URL: tc.uri}))
			got := RenderInline(ft, testRoles(), textOpts{links: true})

			want := "\x1b]8;;" + tc.want + "\x1b\\"
			if !strings.Contains(got, want) {
				t.Errorf("emitted %s\nwant the sequence to carry %q", readable(got), tc.want)
			}
			// The expectation itself must be what the rule describes, or
			// this test would pin the wrong bytes and still pass.
			for i := range len(tc.want) {
				if c := tc.want[i]; c < 0x21 || c > 0x7e {
					t.Fatalf("the expected URI carries a non-printable byte %#x at %d", c, i)
				}
			}
		})
	}
}

// TestAnOverlongURIIsNotEmitted. The convention's own bound is about 2083
// bytes; past it a terminal may truncate, and half a URI is a link to
// somewhere else.
func TestAnOverlongURIIsNotEmitted(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", 2100)
	ft := linked("see the docs", entity(4, 8, &telegram.TextEntityTypeTextURL{URL: long}))

	got := RenderInline(ft, testRoles(), textOpts{links: true})
	if strings.Contains(got, "\x1b]8;;") {
		t.Error("an over-long URI was emitted")
	}
	if !strings.Contains(ansi.Strip(got), "the docs") {
		t.Error("the visible text was dropped with it")
	}
}

// TestABareURLEntityStillLinks. The schemeless case is handled before this
// gate by entityURI, which adds https — the gate must not undo that.
func TestABareURLEntityStillLinks(t *testing.T) {
	ft := linked("go to example.com now", entity(6, 11, &telegram.TextEntityTypeURL{}))
	got := RenderInline(ft, testRoles(), textOpts{links: true})

	if !strings.Contains(got, "\x1b]8;;https://example.com\x1b\\") {
		t.Errorf("a bare URL no longer links:\n%s", readable(got))
	}
}
