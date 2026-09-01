package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
)

// galleryPreview is the link preview the block gallery draws.
func galleryPreview() *telegram.WebPage {
	return &telegram.WebPage{
		URL:         "https://lwn.net/Articles/1",
		DisplayURL:  "lwn.net/Articles/1",
		SiteName:    "lwn.net",
		Title:       "Backpressure without queues",
		Description: "Why bounded channels beat unbounded buffers in practice.",
	}
}

func TestAPreviewDrawsHostTitleAndDescription(t *testing.T) {
	lines := renderWebPage(galleryPreview(), testRoles(), 76)
	joined := ansi.Strip(strings.Join(lines, "\n"))

	for _, want := range []string{
		"lwn.net",
		"Backpressure without queues",
		"Why bounded channels beat unbounded buffers in practice.",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q from:\n%s", want, joined)
		}
	}
	for i, line := range lines {
		if !strings.HasPrefix(ansi.Strip(line), "│ ") {
			t.Errorf("line %d has no rule: %q", i, ansi.Strip(line))
		}
	}
}

// TestAPreviewFallsBackToTheDisplayedURL. Telegram omits the site name for
// pages that do not declare one, and a preview whose first line is blank
// reads as a rendering fault.
func TestAPreviewFallsBackToTheDisplayedURL(t *testing.T) {
	page := galleryPreview()
	page.SiteName = ""

	joined := ansi.Strip(strings.Join(renderWebPage(page, testRoles(), 76), "\n"))
	if !strings.Contains(joined, "lwn.net/Articles/1") {
		t.Fatalf("no host line:\n%s", joined)
	}
}

// TestALongPreviewStaysShort, per the design record: "host, title, and at
// most two description lines". A description is a paragraph written for a
// browser, and letting it run means one link pushing the messages around it
// off the screen.
func TestALongPreviewStaysShort(t *testing.T) {
	page := galleryPreview()
	page.Title = strings.Repeat("a title that keeps going ", 20)
	page.Description = strings.Repeat("a paragraph that keeps going ", 30)

	lines := renderWebPage(page, testRoles(), 40)
	// One host line, at most two of title, at most two of description.
	if len(lines) > 1+2+previewDescriptionLines {
		t.Fatalf("got %d lines:\n%s", len(lines), ansi.Strip(strings.Join(lines, "\n")))
	}
}

// TestADescriptionsOwnLineBreaksDoNotSpendTheBudget. A description arrives
// wrapped for whatever column the page author had in mind; kept, those
// breaks make the two-line budget land after a dozen words.
func TestADescriptionsOwnLineBreaksDoNotSpendTheBudget(t *testing.T) {
	page := galleryPreview()
	page.Description = "one\ntwo\nthree\nfour\nfive\nsix"

	joined := ansi.Strip(strings.Join(renderWebPage(page, testRoles(), 76), "\n"))
	for _, word := range []string{"one", "two", "three", "four", "five", "six"} {
		if !strings.Contains(joined, word) {
			t.Errorf("%q fell off the end of the budget:\n%s", word, joined)
		}
	}
}

// TestAPreviewWithNothingInItDrawsNothing.
func TestAPreviewWithNothingInItDrawsNothing(t *testing.T) {
	if got := renderWebPage(nil, testRoles(), 76); got != nil {
		t.Fatalf("got %q, want nil", got)
	}
	if got := renderWebPage(galleryPreview(), testRoles(), 0); got != nil {
		t.Fatalf("at zero width, got %q, want nil", got)
	}
}

// TestAPreviewFitsAndClosesItsStyles at every width a pane can be.
func TestAPreviewFitsAndClosesItsStyles(t *testing.T) {
	pages := map[string]*telegram.WebPage{
		"gallery":  galleryPreview(),
		"no title": {DisplayURL: "example.com", Description: strings.Repeat("word ", 60)},
		"wide runes": {
			SiteName:    "你好世界",
			Title:       strings.Repeat("你好世界🎉", 6),
			Description: strings.Repeat("你好世界🎉", 20),
		},
		"one long word": {SiteName: "x", Title: strings.Repeat("y", 200)},
	}

	for width := 4; width <= 140; width++ {
		for name, page := range pages {
			for i, line := range renderWebPage(page, testRoles(), width) {
				if got := cell.Width(line); got > width {
					t.Fatalf("%s at width %d: line %d is %d cells: %q",
						name, width, i, got, ansi.Strip(line))
				}
				if open := cell.OpenStyle(line); open != "" {
					t.Fatalf("%s at width %d: line %d leaves %q open", name, width, i, open)
				}
			}
		}
	}
}

// TestAPreviewIsDrawnUnderTheMessageText, not over it: the preview is a
// second reading of a link the sender already wrote out, and putting it
// first buries the sentence they typed.
func TestAPreviewIsDrawnUnderTheMessageText(t *testing.T) {
	r := newTestRenderer()
	lines := r.RenderBody(&telegram.Message{ID: 1, Content: &telegram.MessageText{
		Text:    formatted("Read later: Backpressure without queues"),
		WebPage: galleryPreview(),
	}}, store.NewStore(), BodyOptions{Width: 76})

	joined := ansi.Strip(strings.Join(lines, "\n"))
	text := strings.Index(joined, "Read later:")
	rule := strings.Index(joined, "│ ")
	if text < 0 || rule < 0 {
		t.Fatalf("the body is missing a part:\n%s", joined)
	}
	if rule < text {
		t.Fatalf("the preview was drawn above the message text:\n%s", joined)
	}
}

// TestAMessageThatIsOnlyAPreviewStillRenders. A caption can be empty, and
// "[empty]" over a preview that has a title and a description would be this
// client calling something empty while holding it.
func TestAMessageThatIsOnlyAPreviewStillRenders(t *testing.T) {
	r := newTestRenderer()
	lines := r.RenderBody(&telegram.Message{ID: 1, Content: &telegram.MessageText{
		Text: formatted(""), WebPage: galleryPreview(),
	}}, store.NewStore(), BodyOptions{Width: 76})

	joined := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Contains(joined, "[empty]") {
		t.Fatalf("a message with a preview was called empty:\n%s", joined)
	}
	if !strings.Contains(joined, "Backpressure without queues") {
		t.Fatalf("the preview is missing:\n%s", joined)
	}
}

// TestAMessageWithNeitherTextNorPreviewIsStillOneLine. The scroll index is
// built from line counts, so a zero-line message makes it disagree with
// what is on screen.
func TestAMessageWithNeitherTextNorPreviewIsStillOneLine(t *testing.T) {
	r := newTestRenderer()
	lines := r.RenderBody(&telegram.Message{
		ID: 1, Content: &telegram.MessageText{Text: formatted("")},
	}, store.NewStore(), BodyOptions{Width: 76})

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !strings.Contains(ansi.Strip(lines[0]), "[empty]") {
		t.Fatalf("got %q", ansi.Strip(lines[0]))
	}
}

// TestAPreviewIsCappedAtTheBlockWidth, like every other block.
func TestAPreviewIsCappedAtTheBlockWidth(t *testing.T) {
	page := galleryPreview()
	page.Title = strings.Repeat("title ", 60)
	page.Description = strings.Repeat("description ", 60)

	for _, line := range renderWebPage(page, testRoles(), 200) {
		if got := cell.Width(line); got > maxBlockWidth {
			t.Fatalf("a line is %d cells wide: %q", got, ansi.Strip(line))
		}
	}
}

// TestAPreviewsRuleIsTheAccentColour, which is what separates it from a
// blockquote — the quote's ghost rule is somebody's words in the chat, and
// this rule is a link's own words about itself.
func TestAPreviewsRuleIsTheAccentColour(t *testing.T) {
	roles := testRoles()
	lines := renderWebPage(galleryPreview(), roles, 76)
	if len(lines) == 0 {
		t.Fatal("no preview")
	}

	cyan := lipgloss.NewStyle().Foreground(roles.Cyan).Render("│ ")
	ghost := lipgloss.NewStyle().Foreground(roles.Ghost).Render("│ ")
	if !strings.HasPrefix(lines[0], cyan) {
		t.Fatalf("the rule is not the accent colour: %q", lines[0])
	}
	if strings.HasPrefix(lines[0], ghost) {
		t.Fatalf("the rule is a blockquote's: %q", lines[0])
	}
}
