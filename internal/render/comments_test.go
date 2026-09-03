package render

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/charmbracelet/x/ansi"
)

// TestAChannelPostSaysItHasADiscussion.
//
// A channel is a broadcast: nobody can answer a post in the channel itself.
// A client that does not say the answers exist makes a channel look like a
// place where nothing can be said back.
func TestAChannelPostSaysItHasADiscussion(t *testing.T) {
	cases := map[string]struct {
		comments *telegram.Comments
		want     string
	}{
		"several":     {&telegram.Comments{Count: 12, ChatID: -100777}, "12 comments"},
		"exactly one": {&telegram.Comments{Count: 1, ChatID: -100777}, "1 comment"},
		"none yet":    {&telegram.Comments{Count: 0, ChatID: -100777}, "no comments yet"},
	}
	for name, tc := range cases {
		got := ansi.Strip(strings.Join(renderComments(tc.comments, testRoles(), 60), "\n"))
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want it to say %q", name, got, tc.want)
		}
		// "1 comments" CONTAINS "1 comment", so the plural has to be
		// ruled out rather than the singular merely looked for.
		if tc.want == "1 comment" && strings.Contains(got, "1 comments") {
			t.Errorf("%s: got %q, want the singular", name, got)
		}
		if !strings.Contains(got, "t to open") {
			t.Errorf("%s: got %q, want the key that gets there", name, got)
		}
	}
}

// TestNoDiscussionDrawsNothing. Most messages are not channel posts, and a
// row on every one of them would be noise where it is not information.
func TestNoDiscussionDrawsNothing(t *testing.T) {
	if got := renderComments(nil, testRoles(), 60); got != nil {
		t.Errorf("got %q, want nothing", got)
	}
	if got := renderComments(&telegram.Comments{Count: 3}, testRoles(), 0); got != nil {
		t.Errorf("at zero width, got %q, want nothing", got)
	}
}

// TestAKeyIsOfferedOnlyWhereItLeadsSomewhere. Telegram sometimes reports a
// discussion without naming the group it is in, and a key advertised on a
// row that cannot act is worse than a row that only counts.
func TestAKeyIsOfferedOnlyWhereItLeadsSomewhere(t *testing.T) {
	got := ansi.Strip(strings.Join(
		renderComments(&telegram.Comments{Count: 3}, testRoles(), 60), "\n"))

	if !strings.Contains(got, "3 comments") {
		t.Fatalf("got %q, want the count", got)
	}
	if strings.Contains(got, "t to open") {
		t.Fatalf("got %q, want no key — there is nowhere to go", got)
	}
}

// TestUnreadCommentsSayNew, in the same colour the unread divider uses,
// because it is the same fact.
func TestUnreadCommentsSayNew(t *testing.T) {
	read := renderComments(&telegram.Comments{Count: 3, ChatID: -1}, testRoles(), 60)
	fresh := renderComments(&telegram.Comments{Count: 3, ChatID: -1, Unread: true}, testRoles(), 60)

	if strings.Join(read, "") == strings.Join(fresh, "") {
		t.Fatal("a discussion with something new in it draws like one without")
	}
	if !strings.Contains(ansi.Strip(strings.Join(fresh, "")), "new") {
		t.Errorf("got %q, want it to say so", ansi.Strip(strings.Join(fresh, "")))
	}
}

// TestTheCommentRowFitsAndClosesItsStyles at every width a pane can be.
func TestTheCommentRowFitsAndClosesItsStyles(t *testing.T) {
	comments := []*telegram.Comments{
		{Count: 0, ChatID: -1},
		{Count: 1, ChatID: -1},
		{Count: 4821, ChatID: -1, Unread: true},
		{Count: 7},
	}
	for width := 1; width <= 120; width++ {
		for _, c := range comments {
			for i, line := range renderComments(c, testRoles(), width) {
				if got := cell.Width(line); got > width {
					t.Fatalf("%+v at width %d: line %d is %d cells: %q",
						*c, width, i, got, ansi.Strip(line))
				}
				if open := cell.OpenStyle(line); open != "" {
					t.Fatalf("%+v at width %d: line %d leaves %q open", *c, width, i, open)
				}
			}
		}
	}
}

// TestTheDiscussionRowComesBeforeTheChips. The comments row is a way OUT of
// this message and the chips are a fact about it, and a row that leads
// somewhere reads as the end of the block.
func TestTheDiscussionRowComesBeforeTheChips(t *testing.T) {
	r := newTestRenderer()
	lines := r.RenderBody(&telegram.Message{
		ID: 1, Content: &telegram.MessageText{Text: formatted("shipped")},
		Comments:  &telegram.Comments{Count: 12, ChatID: -100777},
		Reactions: []*telegram.Reaction{{Emoji: "🚀", Count: 4}},
	}, store.NewStore(), BodyOptions{Width: 60})

	joined := ansi.Strip(strings.Join(lines, "\n"))
	comments, chips := strings.Index(joined, "12 comments"), strings.Index(joined, "[🚀 4]")
	if comments < 0 || chips < 0 {
		t.Fatalf("a part is missing:\n%s", joined)
	}
	if chips < comments {
		t.Fatalf("the chips were drawn above the way out:\n%s", joined)
	}
}
