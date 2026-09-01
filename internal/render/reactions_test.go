package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
)

// galleryReactions is the row the block gallery draws.
func galleryReactions() []*telegram.Reaction {
	return []*telegram.Reaction{
		{Emoji: "👍", Count: 3},
		{Emoji: "👀", Count: 5, Chosen: true},
		{Emoji: "🔥", Count: 2},
		{Emoji: "🚀", Count: 4},
	}
}

func TestReactionChipsShowTheEmojiAndTheCount(t *testing.T) {
	joined := ansi.Strip(strings.Join(renderReactions(galleryReactions(), testRoles(), 76), "\n"))

	for _, want := range []string{"[👍 3]", "[👀 5]", "[🔥 2]", "[🚀 4]"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q from %q", want, joined)
		}
	}
}

// TestAReactionYouLeftIsMarked. The chip's FRAME carries it: the emoji is
// the sender's and the count is everyone's, so the only part of the chip
// this client may colour is the part it drew itself.
func TestAReactionYouLeftIsMarked(t *testing.T) {
	render := func(chosen bool) string {
		lines := renderReactions([]*telegram.Reaction{
			{Emoji: "👀", Count: 5, Chosen: chosen},
		}, testRoles(), 76)
		if len(lines) != 1 {
			t.Fatalf("got %d lines, want 1", len(lines))
		}
		return lines[0]
	}

	mine, theirs := render(true), render(false)
	if mine == theirs {
		t.Fatalf("a reaction you left is drawn exactly like one you did not: %q", mine)
	}
	if ansi.Strip(mine) != ansi.Strip(theirs) {
		t.Fatalf("the mark changed the chip's text, not its colour: %q vs %q",
			ansi.Strip(mine), ansi.Strip(theirs))
	}
}

// TestACustomEmojiReactionAdmitsWhatItCannotShow. Its artwork is a document
// this client does not fetch; drawing somebody else's 👍 over it would make
// the chip say something nobody said.
func TestACustomEmojiReactionAdmitsWhatItCannotShow(t *testing.T) {
	chip := func(reaction *telegram.Reaction) string {
		return ansi.Strip(strings.Join(renderReactions(
			[]*telegram.Reaction{reaction}, testRoles(), 76), "\n"))
	}

	custom := chip(&telegram.Reaction{CustomEmojiID: 77, Count: 4})
	if !strings.Contains(custom, "4") {
		t.Fatalf("the count, which is real, is missing: %q", custom)
	}

	// Asserted against the emoji themselves rather than against the mark
	// this package chose: the guarantee is that a custom reaction is not
	// drawn as a standard one, whichever standard one is picked.
	for _, standard := range []string{"👍", "👎", "❤", "🔥", "👀", "🎉", "😁"} {
		if custom == chip(&telegram.Reaction{Emoji: standard, Count: 4}) {
			t.Fatalf("a custom reaction is drawn as %s: %q", standard, custom)
		}
		if strings.Contains(custom, standard) {
			t.Fatalf("a custom reaction borrowed %s: %q", standard, custom)
		}
	}
}

// TestReactionsWithNoCountAreNotDrawn. Telegram removes a reaction by
// sending it with a count of zero, and "[👍 0]" is a chip for something
// nobody did.
func TestReactionsWithNoCountAreNotDrawn(t *testing.T) {
	got := renderReactions([]*telegram.Reaction{
		{Emoji: "👍", Count: 0},
		{Emoji: "👀", Count: -1},
		nil,
	}, testRoles(), 76)

	if len(got) != 0 {
		t.Fatalf("got %q, want nothing", got)
	}
}

func TestNoReactionsDrawNothing(t *testing.T) {
	if got := renderReactions(nil, testRoles(), 76); got != nil {
		t.Fatalf("got %q, want nil", got)
	}
	if got := renderReactions(galleryReactions(), testRoles(), 0); got != nil {
		t.Fatalf("at zero width, got %q, want nil", got)
	}
}

// TestReactionChipsWrapRatherThanOverflow, and they wrap on the width the
// terminal may DRAW them at rather than the width the tables measure.
//
// This is the whole reason the row is budgeted with cell.Reserve. An emoji
// is the one string the two disagree about — a base-plus-selector pair
// measures two cells and is drawn as one by a terminal that does not
// compose it, a ZWJ family measures two and is drawn as six — and a row
// budgeted on the smaller of the two runs off the end of the pane on the
// terminal that draws the larger.
func TestReactionChipsWrapRatherThanOverflow(t *testing.T) {
	reactions := []*telegram.Reaction{
		{Emoji: "👍", Count: 3},
		{Emoji: "‼️", Count: 12},
		{Emoji: "👨‍👩‍👧‍👦", Count: 7},
		{Emoji: "🇬🇧", Count: 1},
		{CustomEmojiID: 9, Count: 100},
	}

	for width := 1; width <= 80; width++ {
		for i, line := range renderReactions(reactions, testRoles(), width) {
			if reserved := cell.Reserve(ansi.Strip(line)); reserved > width {
				t.Fatalf("at width %d, line %d reserves %d cells: %q",
					width, i, reserved, ansi.Strip(line))
			}
			if open := cell.OpenStyle(line); open != "" {
				t.Fatalf("at width %d, line %d leaves %q open", width, i, open)
			}
		}
	}
}

// TestEveryChipReachesTheScreen wherever one fits. A chip dropped for want
// of room is a reaction the reader never learns about, so each takes a line
// of its own rather than being skipped.
func TestEveryChipReachesTheScreen(t *testing.T) {
	reactions := galleryReactions()
	for width := 6; width <= 80; width++ {
		joined := ansi.Strip(strings.Join(renderReactions(reactions, testRoles(), width), "\n"))
		for _, reaction := range reactions {
			if !strings.Contains(joined, reaction.Emoji) {
				t.Fatalf("at width %d, %q was dropped:\n%s", width, reaction.Emoji, joined)
			}
		}
	}
}

// TestAChipTooWideForThePaneIsCutRatherThanSkipped. Below a chip's own
// width there is nothing useful to show, but the reader must still see that
// the message was reacted to at all.
func TestAChipTooWideForThePaneIsCutRatherThanSkipped(t *testing.T) {
	reactions := galleryReactions()
	for width := 1; width <= 5; width++ {
		lines := renderReactions(reactions, testRoles(), width)
		if len(lines) != len(reactions) {
			t.Fatalf("at width %d, %d chips became %d lines",
				width, len(reactions), len(lines))
		}
		for i, line := range lines {
			if reserved := cell.Reserve(ansi.Strip(line)); reserved > width {
				t.Fatalf("at width %d, line %d reserves %d cells: %q",
					width, i, reserved, ansi.Strip(line))
			}
			if open := cell.OpenStyle(line); open != "" {
				t.Fatalf("at width %d, line %d leaves %q open", width, i, open)
			}
		}
	}
}

// TestReactionsRenderBelowTheBody, and below every kind of body — they
// belong to the message, not to its content.
func TestReactionsRenderBelowTheBody(t *testing.T) {
	r := newTestRenderer()
	st := store.NewStore()

	contents := map[string]telegram.MessageContent{
		"text":  &telegram.MessageText{Text: formatted("look at this")},
		"photo": &telegram.MessagePhoto{Photo: &telegram.Photo{ID: 1}},
		"poll":  &telegram.MessagePoll{Poll: designRecordPoll()},
	}

	for name, content := range contents {
		lines := r.RenderBody(&telegram.Message{
			ID: 1, Content: content, Reactions: galleryReactions(),
		}, st, BodyOptions{Width: 76})

		if len(lines) < 2 {
			t.Fatalf("%s: got %d lines", name, len(lines))
		}
		last := ansi.Strip(lines[len(lines)-1])
		if !strings.Contains(last, "[👍 3]") {
			t.Errorf("%s: the reactions are not the last thing drawn: %q", name, last)
		}
	}
}
