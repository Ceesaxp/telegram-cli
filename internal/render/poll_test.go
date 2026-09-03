package render

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/charmbracelet/x/ansi"
)

// designRecordPoll is the poll the block gallery draws.
func designRecordPoll() *telegram.Poll {
	return &telegram.Poll{
		Question:        "Ship 0.4.2 tonight?",
		TotalVoterCount: 11,
		ResultsKnown:    true,
		IsAnonymous:     true,
		Options: []*telegram.PollOption{
			{Text: "Yes, tag it", VoterCount: 7, Percent: 64, Chosen: true},
			{Text: "Wait for keymap", VoterCount: 3, Percent: 27},
			{Text: "Abstain", VoterCount: 1, Percent: 9},
		},
	}
}

func TestPollDrawsQuestionOptionsBarsAndFooter(t *testing.T) {
	lines := renderPoll(designRecordPoll(), testRoles(), 76)
	joined := ansi.Strip(strings.Join(lines, "\n"))

	for _, want := range []string{
		"Ship 0.4.2 tonight?",
		"Yes, tag it", "Wait for keymap", "Abstain",
		"64%", "27%", "9%",
		"11 votes", "anonymous",
		"█", "░",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q from:\n%s", want, joined)
		}
	}

	// The chosen option is marked and the others are not.
	if strings.Count(joined, "◉") != 1 {
		t.Errorf("want exactly one chosen mark:\n%s", joined)
	}
	if strings.Count(joined, "○") != 2 {
		t.Errorf("want two unchosen marks:\n%s", joined)
	}
}

// TestPollBarsShareOneScale is what makes the bars readable: a 64% bar is
// longer than a 27% one, and every bar is the same total length.
func TestPollBarsShareOneScale(t *testing.T) {
	lines := renderPoll(designRecordPoll(), testRoles(), 76)

	var widths, fills []int
	for _, line := range lines {
		plain := ansi.Strip(line)
		if !strings.ContainsAny(plain, "█░") {
			continue
		}
		fill := strings.Count(plain, "█")
		widths = append(widths, fill+strings.Count(plain, "░"))
		fills = append(fills, fill)
	}

	if len(widths) != 3 {
		t.Fatalf("got %d bars, want 3", len(widths))
	}
	for i := 1; i < len(widths); i++ {
		if widths[i] != widths[0] {
			t.Fatalf("bars are %v cells wide — they do not share a scale", widths)
		}
	}
	if !(fills[0] > fills[1] && fills[1] > fills[2]) {
		t.Fatalf("fills %v do not follow the shares 64/27/9", fills)
	}
}

// TestASmallShareStillDrawsABar. A truncating bar shows nothing for every
// share below one cell's worth, so the least popular option and an option
// with no votes at all look identical.
func TestASmallShareStillDrawsABar(t *testing.T) {
	poll := &telegram.Poll{
		Question:     "?",
		ResultsKnown: true,
		Options: []*telegram.PollOption{
			{Text: "many", VoterCount: 96, Percent: 96},
			{Text: "few", VoterCount: 4, Percent: 4},
			{Text: "none", VoterCount: 0, Percent: 0},
		},
	}

	fills := map[string]int{}
	for _, line := range renderPoll(poll, testRoles(), 76) {
		plain := ansi.Strip(line)
		for _, name := range []string{"many", "few", "none"} {
			if strings.Contains(plain, name) {
				fills[name] = strings.Count(plain, "█")
			}
		}
	}

	if fills["few"] == 0 {
		t.Errorf("a 4%% share drew no bar at all: %v", fills)
	}
	if fills["none"] != 0 {
		t.Errorf("an option with no votes drew %d cells of bar", fills["none"])
	}
}

// TestAPollWithHiddenResultsDrawsNoBars. Telegram sends no tallies for a
// poll that hides its results until it closes. Empty bars there would state
// that every option has no votes, which is a result and a false one.
func TestAPollWithHiddenResultsDrawsNoBars(t *testing.T) {
	poll := &telegram.Poll{
		Question:    "Which release?",
		IsAnonymous: true,
		Options: []*telegram.PollOption{
			{Text: "0.4.2"}, {Text: "0.5.0, once the keymap lands"},
		},
	}

	joined := ansi.Strip(strings.Join(renderPoll(poll, testRoles(), 76), "\n"))
	for _, invented := range []string{"█", "░", "%", "votes"} {
		if strings.Contains(joined, invented) {
			t.Fatalf("a result was invented (%q):\n%s", invented, joined)
		}
	}
	for _, want := range []string{"Which release?", "0.4.2", "0.5.0, once the keymap lands", "anonymous"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q:\n%s", want, joined)
		}
	}
	// The answer column is padded to align the bars that are not there.
	for i, line := range renderPoll(poll, testRoles(), 76) {
		if strings.HasSuffix(ansi.Strip(line), " ") {
			t.Errorf("line %d ends in padding: %q", i, ansi.Strip(line))
		}
	}
}

func TestPollFooterStatesOnlyKnownFacts(t *testing.T) {
	cases := map[string]struct {
		poll *telegram.Poll
		want []string
		not  []string
	}{
		"one vote": {
			poll: &telegram.Poll{TotalVoterCount: 1, ResultsKnown: true, IsAnonymous: true},
			want: []string{"1 vote", "anonymous"},
			not:  []string{"1 votes"},
		},
		"results known, nobody voted": {
			poll: &telegram.Poll{ResultsKnown: true, IsAnonymous: true},
			want: []string{"no votes yet"},
		},
		"total withheld": {
			poll: &telegram.Poll{IsAnonymous: true},
			want: []string{"anonymous"},
			not:  []string{"vote", "0"},
		},
		"public quiz, multiple answers, closed": {
			poll: &telegram.Poll{
				TotalVoterCount: 4, ResultsKnown: true,
				IsQuiz: true, MultipleChoice: true, IsClosed: true,
			},
			// "voters", not "votes": four people cast as many votes as
			// they liked, and the total counts the people.
			want: []string{"4 voters", "quiz", "multiple answers", "public", "closed"},
			not:  []string{"4 votes", "anonymous", "closes"},
		},
		"one voter of a multiple-choice poll": {
			poll: &telegram.Poll{
				TotalVoterCount: 1, ResultsKnown: true,
				MultipleChoice: true, IsAnonymous: true,
			},
			want: []string{"1 voter"},
			not:  []string{"1 vote ", "votes"},
		},
		"closes at a time": {
			poll: &telegram.Poll{IsAnonymous: true, CloseDate: 1_700_000_000},
			want: []string{"closes "},
			not:  []string{"closed"},
		},
	}

	for name, tc := range cases {
		got := pollFooter(tc.poll)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %q is missing %q", name, got, want)
			}
		}
		for _, not := range tc.not {
			if strings.Contains(got, not) {
				t.Errorf("%s: %q should not mention %q", name, got, not)
			}
		}
	}
}

// TestAClosedPollSaysClosedRatherThanWhenItClosed. Both facts are true; only
// one of them is still useful.
func TestAClosedPollSaysClosedRatherThanWhenItClosed(t *testing.T) {
	got := pollFooter(&telegram.Poll{IsClosed: true, CloseDate: 1_700_000_000, IsAnonymous: true})
	if !strings.Contains(got, "closed") || strings.Contains(got, "closes") {
		t.Fatalf("footer = %q", got)
	}
}

// TestANarrowPollKeepsItsPercentages. The bar is what gives way first: it
// can be read off the number beside it, and the number cannot be read off
// anything.
func TestANarrowPollKeepsItsPercentages(t *testing.T) {
	for width := 12; width <= 30; width++ {
		joined := ansi.Strip(strings.Join(renderPoll(designRecordPoll(), testRoles(), width), "\n"))
		for _, want := range []string{"64%", "27%", "9%"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("at width %d, %q is gone:\n%s", width, want, joined)
			}
		}
	}
}

// TestAPollFitsAndClosesItsStyles at every width a pane can be, down to the
// single cell the thread grid clamps its body column to.
func TestAPollFitsAndClosesItsStyles(t *testing.T) {
	polls := map[string]*telegram.Poll{
		"design record": designRecordPoll(),
		"hidden results": {
			Question: strings.Repeat("long question ", 12),
			Options:  []*telegram.PollOption{{Text: strings.Repeat("long answer ", 8)}},
		},
		"quiz": {
			Question: "2 + 2?", ResultsKnown: true, TotalVoterCount: 3, IsQuiz: true,
			Options: []*telegram.PollOption{
				{Text: "4", VoterCount: 3, Percent: 100, Correct: true, Chosen: true},
				{Text: "5"},
			},
		},
		"wide runes": {
			Question: "你好世界🎉", ResultsKnown: true, TotalVoterCount: 2,
			Options: []*telegram.PollOption{{Text: "你好世界🎉", VoterCount: 2, Percent: 100}},
		},
	}

	for width := 1; width <= 140; width++ {
		for name, poll := range polls {
			for i, line := range renderPoll(poll, testRoles(), width) {
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

// TestAQuizMarksTheRightAnswer.
func TestAQuizMarksTheRightAnswer(t *testing.T) {
	poll := &telegram.Poll{
		Question: "2 + 2?", ResultsKnown: true, TotalVoterCount: 3, IsQuiz: true,
		Options: []*telegram.PollOption{
			{Text: "4", VoterCount: 3, Percent: 100, Correct: true},
			{Text: "5"},
		},
	}
	joined := ansi.Strip(strings.Join(renderPoll(poll, testRoles(), 60), "\n"))
	if !strings.Contains(joined, "✓") {
		t.Fatalf("the right answer is not marked:\n%s", joined)
	}
}

// TestAPollRendersThroughTheBody, so the wiring from content to lines is
// covered and not only the block renderer underneath it.
func TestAPollRendersThroughTheBody(t *testing.T) {
	r := newTestRenderer()
	lines := r.RenderBody(&telegram.Message{
		ID: 1, Content: &telegram.MessagePoll{Poll: designRecordPoll()},
	}, store.NewStore(), BodyOptions{Width: 76})

	joined := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"Ship 0.4.2 tonight?", "64%", "11 votes"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q from the rendered body:\n%s", want, joined)
		}
	}
}

// TestAPollTooNarrowForAMarkKeepsItsAnswers. A handful of cells is not a
// pane, but it is a width the layout arithmetic has to survive: what gives
// way there is the state mark, because a row that spends two of its five
// cells on a ring has stopped saying what is being voted on.
func TestAPollTooNarrowForAMarkKeepsItsAnswers(t *testing.T) {
	poll := &telegram.Poll{
		Question: "?", ResultsKnown: true, TotalVoterCount: 2,
		Options: []*telegram.PollOption{
			{Text: "yes", VoterCount: 2, Percent: 100},
			{Text: "no"},
		},
	}
	for width := 2; width <= 5; width++ {
		lines := renderPoll(poll, testRoles(), width)
		joined := ansi.Strip(strings.Join(lines, "\n"))
		for i, line := range lines {
			if got := cell.Width(line); got > width {
				t.Fatalf("at width %d, line %d is %d cells: %q", width, i, got, ansi.Strip(line))
			}
			if open := cell.OpenStyle(line); open != "" {
				t.Fatalf("at width %d, line %d leaves %q open", width, i, open)
			}
		}
		if !strings.Contains(joined, "y") || !strings.Contains(joined, "n") {
			t.Fatalf("at width %d the answers are gone:\n%s", width, joined)
		}
		if strings.ContainsAny(joined, "○◉") {
			t.Fatalf("at width %d a mark crowded out the answer:\n%s", width, joined)
		}
	}
}

// TestAPollIsCappedAtTheBlockWidth. Every block begins at the body column
// and is capped to the smaller of body width and 84 columns: a poll
// stretched across a 200-column terminal makes the eye travel the whole
// width to get from an answer to its bar.
func TestAPollIsCappedAtTheBlockWidth(t *testing.T) {
	poll := designRecordPoll()
	poll.Question = strings.Repeat("question ", 40)
	for _, option := range poll.Options {
		option.Text = strings.Repeat("answer ", 20)
	}

	for _, line := range renderPoll(poll, testRoles(), 200) {
		if got := cell.Width(line); got > maxBlockWidth {
			t.Fatalf("a line is %d cells wide: %q", got, ansi.Strip(line))
		}
	}
}

// TestShortAnswersDoNotPushTheirBarsAcrossThePane. The answer column is the
// width of the longest answer, not of whatever is left over: bars a screen
// away from the words they belong to are two separate lists.
func TestShortAnswersDoNotPushTheirBarsAcrossThePane(t *testing.T) {
	poll := &telegram.Poll{
		Question: "?", ResultsKnown: true, TotalVoterCount: 2,
		Options: []*telegram.PollOption{
			{Text: "a", VoterCount: 1, Percent: 50},
			{Text: "b", VoterCount: 1, Percent: 50},
		},
	}
	for _, line := range renderPoll(poll, testRoles(), 76) {
		plain := ansi.Strip(line)
		if !strings.ContainsAny(plain, "█░") {
			continue
		}
		if got := cell.Width(line); got > 40 {
			t.Fatalf("a one-letter answer spans %d cells: %q", got, plain)
		}
	}
}

// TestBarsStartAtTheSameColumn. Answers are not the same length, and bars
// that start where each answer happens to end are three separate scales the
// reader has to compare by eye.
func TestBarsStartAtTheSameColumn(t *testing.T) {
	starts := map[int]bool{}
	for _, line := range renderPoll(designRecordPoll(), testRoles(), 76) {
		plain := ansi.Strip(line)
		i := strings.IndexAny(plain, "█░")
		if i < 0 {
			continue
		}
		starts[cell.Width(plain[:i])] = true
	}

	if len(starts) != 1 {
		t.Fatalf("bars start at %d different columns: %v", len(starts), starts)
	}
}
