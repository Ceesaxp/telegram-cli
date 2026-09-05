package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
)

func msgFT(text string, entities ...*telegram.TextEntity) *telegram.FormattedText {
	return &telegram.FormattedText{Text: text, Entities: entities}
}

func linkURLEntity(off, length int32) *telegram.TextEntity {
	return &telegram.TextEntity{Offset: off, Length: length, Type: &telegram.TextEntityTypeURL{}}
}

func linkTextURLEntity(off, length int32, url string) *telegram.TextEntity {
	return &telegram.TextEntity{Offset: off, Length: length, Type: &telegram.TextEntityTypeTextURL{URL: url}}
}

func linkTextMsg(f *telegram.FormattedText) *telegram.Message {
	return &telegram.Message{ID: 1, Content: &telegram.MessageText{Text: f}}
}

func TestMessageLinksInReadingOrder(t *testing.T) {
	// Entities deliberately out of order: Telegram does not promise they
	// are sorted, and "the second link" has to mean the second one down the
	// screen or cycling is a lottery.
	m := linkTextMsg(msgFT("see one and two here",
		linkTextURLEntity(12, 3, "https://second.example"),
		linkTextURLEntity(4, 3, "https://first.example"),
	))

	links := MessageLinks(m)
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2: %+v", len(links), links)
	}
	if links[0].Text != "one" || links[0].URI != "https://first.example" {
		t.Errorf("first link = %+v", links[0])
	}
	if links[1].Text != "two" || links[1].URI != "https://second.example" {
		t.Errorf("second link = %+v", links[1])
	}
}

// The visible text and the destination are allowed to disagree — that is
// what a text_url IS — and both have to survive, because the reader decides
// whether to follow it by looking at the destination.
func TestMessageLinksKeepTextAndDestinationApart(t *testing.T) {
	m := linkTextMsg(msgFT("click here", linkTextURLEntity(6, 4, "https://elsewhere.example/path")))

	links := MessageLinks(m)
	if len(links) != 1 {
		t.Fatalf("got %d links, want 1", len(links))
	}
	if links[0].Text != "here" {
		t.Errorf("Text = %q, want the visible run", links[0].Text)
	}
	if links[0].URI != "https://elsewhere.example/path" {
		t.Errorf("URI = %q, want the entity's own destination", links[0].URI)
	}
}

// A bare URL entity may cover text with no scheme, and an email is a mailto
// the client has to build — both handled by entityURI, which is shared with
// the renderer so a link cannot be drawn one way and opened another.
func TestMessageLinksResolveBareAndEmail(t *testing.T) {
	m := linkTextMsg(msgFT("example.com or a@b.co",
		linkURLEntity(0, 11),
		&telegram.TextEntity{Offset: 15, Length: 6, Type: &telegram.TextEntityTypeEmailAddress{}},
	))

	links := MessageLinks(m)
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2: %+v", len(links), links)
	}
	if links[0].URI != "https://example.com" {
		t.Errorf("bare URL resolved to %q", links[0].URI)
	}
	if links[1].URI != "mailto:a@b.co" {
		t.Errorf("email resolved to %q", links[1].URI)
	}
}

// A scheme this client will not hand to the platform opener is still LISTED
// and still cycled past. A key that silently skips something the reader can
// plainly see underlined is a key that looks broken.
func TestUnopenableSchemesAreListedButNotOpenable(t *testing.T) {
	m := linkTextMsg(msgFT("grab it", linkTextURLEntity(0, 4, "ftp://files.example/x")))

	links := MessageLinks(m)
	if len(links) != 1 {
		t.Fatalf("an ftp link was dropped from the list entirely: %+v", links)
	}
	if links[0].Openable() {
		t.Error("ftp:// reported as openable")
	}
	if links[0].SafeURI() != "" {
		t.Errorf("SafeURI = %q, want empty for a refused scheme", links[0].SafeURI())
	}

	ok := linkTextMsg(msgFT("grab it", linkTextURLEntity(0, 4, "https://files.example/x")))
	if got := MessageLinks(ok)[0]; !got.Openable() || got.SafeURI() == "" {
		t.Errorf("https link not openable: %+v", got)
	}
}

// A link under a photo is a link.
func TestMessageLinksReadCaptions(t *testing.T) {
	m := &telegram.Message{ID: 1, Content: &telegram.MessagePhoto{
		Caption: msgFT("see docs", linkTextURLEntity(4, 4, "https://docs.example")),
	}}
	if links := MessageLinks(m); len(links) != 1 || links[0].URI != "https://docs.example" {
		t.Fatalf("caption links = %+v", links)
	}
}

// The animation caption was being dropped entirely by captionOf, so a GIF
// sent with something written under it drew the card and lost the words.
func TestAnimationCaptionsAreNotDropped(t *testing.T) {
	m := &telegram.Message{ID: 1, Content: &telegram.MessageAnimation{
		Caption: msgFT("see docs", linkTextURLEntity(4, 4, "https://docs.example")),
	}}
	if links := MessageLinks(m); len(links) != 1 {
		t.Fatalf("animation caption links = %+v", links)
	}
	if got := captionOf(m.Content); got == nil || got.Text != "see docs" {
		t.Errorf("captionOf dropped the animation's caption: %+v", got)
	}
}

func TestNoLinksIsNoLinks(t *testing.T) {
	if links := MessageLinks(linkTextMsg(msgFT("just words"))); len(links) != 0 {
		t.Errorf("got %+v, want none", links)
	}
	if links := MessageLinks(nil); links != nil {
		t.Errorf("nil message produced %+v", links)
	}
}

// The armed link is drawn as a selection, and only that link: arming one of
// two must not light up both.
func TestArmedLinkIsMarkedInTheBody(t *testing.T) {
	f := msgFT("see one and two here",
		linkTextURLEntity(4, 3, "https://first.example"),
		linkTextURLEntity(12, 3, "https://second.example"),
	)
	roles := theme.DarkRoles(false)

	plainOut := strings.Join(renderBlocks(f, roles, 60, textOpts{}), "\n")
	armed := strings.Join(renderBlocks(f, roles, 60, textOpts{armedLo: 4, armedHi: 7}), "\n")

	if plainOut == armed {
		t.Fatal("arming a link changed nothing in the output")
	}
	// The text itself is untouched — this is a mark, not a rewrite.
	if ansi.Strip(plainOut) != ansi.Strip(armed) {
		t.Errorf("arming changed the text:\n plain %q\n armed %q", ansi.Strip(plainOut), ansi.Strip(armed))
	}

	other := strings.Join(renderBlocks(f, roles, 60, textOpts{armedLo: 12, armedHi: 15}), "\n")
	if other == armed {
		t.Error("arming the second link produced the same output as arming the first")
	}
}
