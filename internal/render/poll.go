package render

import (
	"strconv"
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// pollBarWidth is the full-scale width of an option's bar, from the design
// record's gallery. Every option in a poll is drawn against the same scale,
// so the bars compare to each other rather than each filling its own row.
const pollBarWidth = 19

// pollPercentWidth is the tail on an option row: two spaces of gap, then a
// right-aligned percentage wide enough for "100%".
const pollPercentWidth = 2 + 4

// pollBarMinWidth is the narrowest bar worth drawing. Below it the bar
// stops being a comparison and becomes a rounding artefact — at four cells
// every share between 13% and 37% is one block — so the percentages are
// kept and the bars dropped.
const pollBarMinWidth = 6

// renderPoll draws a poll: its question, one row per option, and a footer
// of the facts that decide how much the numbers above it are worth.
//
// Bars are drawn ONLY when the server sent tallies. A poll that hides its
// results until it closes sends none, and a row of empty bars there would
// state that every option has no votes — which is a result, and a false
// one. Such a poll renders as its options and nothing else, which is
// exactly what the voter can see.
func renderPoll(poll *telegram.Poll, roles theme.Roles, width int) []string {
	if poll == nil || width < 1 {
		return nil
	}

	blockW := min(width, maxBlockWidth)
	question := lipgloss.NewStyle().Foreground(roles.Fg)

	var out []string
	for _, line := range cell.WrapLines(poll.Question, blockW) {
		// Truncate as well as wrap: a rune wider than the whole column is
		// emitted whole rather than dropped, and one cell of body is a
		// width the grid will clamp to but not one it will forgive.
		out = append(out, question.Render(cell.Truncate(line, blockW)))
	}
	out = append(out, pollOptionRows(poll, roles, blockW)...)

	if footer := pollFooter(poll); footer != "" {
		out = append(out, lipgloss.NewStyle().Foreground(roles.Faint).
			Render(cell.Truncate(footer, blockW)))
	}
	return out
}

// pollOptionRows draws one row per option: the state mark, the answer, and
// — when there are results — a scaled bar and its percentage.
//
// Parts drop in a fixed order as the pane narrows: first the bar, which can
// be read off the number beside it; then the percentage; then the mark. The
// answer's own text is the last thing standing, because a row that has lost
// it has lost the thing being voted on.
func pollOptionRows(poll *telegram.Poll, roles theme.Roles, blockW int) []string {
	const markW = 2 // the mark and the space after it

	mark := lipgloss.NewStyle().Foreground(roles.Cyan)
	inert := lipgloss.NewStyle().Foreground(roles.Ghost)
	label := lipgloss.NewStyle().Foreground(roles.Fg)
	filled := lipgloss.NewStyle().Foreground(roles.Cyan)
	empty := lipgloss.NewStyle().Foreground(roles.Border)
	percent := lipgloss.NewStyle().Foreground(roles.Faint)

	// The mark is dropped once it costs more than it is worth: at four
	// cells of answer it is still a state; at one it is two thirds of the
	// row spent saying nothing about what the row is for.
	const minLabelWithMark = 4

	if blockW < markW+minLabelWithMark {
		out := make([]string, 0, len(poll.Options))
		for _, option := range poll.Options {
			out = append(out, label.Render(cell.Truncate(option.Text, blockW)))
		}
		return out
	}

	// Every drop leaves at least one cell for the answer.
	room := blockW - markW
	showPercent := poll.ResultsKnown && room >= 1+pollPercentWidth
	if showPercent {
		room -= pollPercentWidth
	}
	barW := 0
	if showPercent && room >= 1+2+pollBarMinWidth {
		barW = min(pollBarWidth, (room-2)/2)
		room -= barW + 2
	}
	labelW := max(min(room, longestOption(poll.Options)), 1)

	out := make([]string, 0, len(poll.Options))
	for _, option := range poll.Options {
		style := inert
		if option.Chosen || (poll.IsQuiz && option.Correct) {
			style = mark
		}

		// Padded only when something follows it. A row whose answer is the
		// last thing on it has nothing to align against, and the trailing
		// blanks would be invisible until something painted over them.
		text := cell.Truncate(option.Text, labelW)
		if barW > 0 || showPercent {
			text = cell.Fit(text, labelW)
		}

		row := style.Render(pollMark(option, poll.IsQuiz)) + " " + label.Render(text)
		if barW > 0 {
			row += "  " + pollBar(option.Percent, barW, filled, empty)
		}
		if showPercent {
			row += percent.Render(cell.PadLeft(strconv.Itoa(int(option.Percent))+"%", pollPercentWidth))
		}
		out = append(out, row)
	}
	return out
}

// pollMark is an option's state glyph: filled when the local user picked
// it, a check when a quiz says it was the right answer, and an empty ring
// otherwise.
func pollMark(option *telegram.PollOption, quiz bool) string {
	switch {
	case quiz && option.Correct:
		return "✓"
	case option.Chosen:
		return "◉"
	default:
		return "○"
	}
}

// pollBar draws one option's share against the shared scale.
//
// The fill is ROUNDED, not truncated: a truncating bar draws nothing at all
// for every share below one cell's worth, so the option with the fewest
// votes and the option with none look identical.
func pollBar(percent int32, barW int, filled, empty lipgloss.Style) string {
	n := int((int64(percent)*int64(barW) + 50) / 100)
	n = min(max(n, 0), barW)
	return filled.Render(strings.Repeat("█", n)) +
		empty.Render(strings.Repeat("░", barW-n))
}

// longestOption is the display width of the widest answer, so a poll whose
// answers are all short does not push its bars to the far side of a wide
// pane.
func longestOption(options []*telegram.PollOption) int {
	widest := 0
	for _, option := range options {
		widest = max(widest, cell.Width(option.Text))
	}
	return widest
}

// pollFooter is the line under the options: how many people voted, how the
// poll behaves, and when it ends.
//
// Every part is omitted when it is not known. A poll whose total the server
// withheld says nothing about votes rather than "0 votes", which would be a
// count this client never received.
func pollFooter(poll *telegram.Poll) string {
	var facts []string

	// "votes" only where a vote and a voter are the same thing. A
	// multiple-choice poll's total counts PEOPLE, and six votes cast by
	// three of them reported as "3 votes" is a number that matches
	// nothing on the rows above it.
	unit, plural := "vote", "votes"
	if poll.MultipleChoice {
		unit, plural = "voter", "voters"
	}

	switch {
	case poll.TotalVoterCount == 1:
		facts = append(facts, "1 "+unit)
	case poll.TotalVoterCount > 1:
		facts = append(facts, strconv.Itoa(int(poll.TotalVoterCount))+" "+plural)
	case poll.ResultsKnown:
		facts = append(facts, "no "+plural+" yet")
	}

	if poll.IsQuiz {
		facts = append(facts, "quiz")
	}
	if poll.MultipleChoice {
		facts = append(facts, "multiple answers")
	}
	if poll.IsAnonymous {
		facts = append(facts, "anonymous")
	} else {
		facts = append(facts, "public")
	}

	switch {
	case poll.IsClosed:
		facts = append(facts, "closed")
	case poll.CloseDate > 0:
		facts = append(facts, "closes "+FormatTimestamp(poll.CloseDate))
	}

	return strings.Join(facts, " · ")
}
