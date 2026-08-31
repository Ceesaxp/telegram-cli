package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
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
