package render

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// customReactionMark stands in for a custom emoji reaction, whose artwork
// is a document this client does not fetch.
//
// A neutral mark rather than a substitute emoji. The count is real and the
// thing counted is unknown; drawing somebody else's 👍 over it would make
// the chip say something nobody said.
const customReactionMark = "✱"

// renderReactions draws a message's reaction chips, wrapped to width.
//
// Widths are budgeted with cell.Reserve rather than cell.Width. A reaction
// is an emoji by definition, and an emoji is the one string whose drawn
// width the tables and the terminal disagree about: a base-plus-selector
// pair measures 2 and is drawn as 1 by a terminal that does not compose it,
// a ZWJ family measures 2 and is drawn as 6. Reserving the larger of the
// two is what keeps a row of chips inside the body column on both.
func renderReactions(reactions []*telegram.Reaction, roles theme.Roles, width int) []string {
	if len(reactions) == 0 || width < 1 {
		return nil
	}

	frame := lipgloss.NewStyle().Foreground(roles.Ghost)
	chosen := lipgloss.NewStyle().Foreground(roles.Cyan)
	count := lipgloss.NewStyle().Foreground(roles.Faint)

	var (
		out  []string
		line string
		used int
	)
	for _, reaction := range reactions {
		if reaction == nil || reaction.Count <= 0 {
			continue
		}

		text := reactionChip(reaction)
		chipW := cell.Reserve(text)

		style := frame
		if reaction.Chosen {
			style = chosen
		}
		styled := style.Render("[") +
			reactionMark(reaction) + " " +
			count.Render(strconv.Itoa(int(reaction.Count))) +
			style.Render("]")

		switch {
		case line == "":
			line, used = styled, chipW
		case used+1+chipW <= width:
			line, used = line+" "+styled, used+1+chipW
		default:
			out = append(out, line)
			line, used = styled, chipW
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// reactionChip is a chip's plain text, used to measure it. It has to be
// built by the same code that styles one, or the measurement drifts from
// the drawing the first time either changes.
func reactionChip(reaction *telegram.Reaction) string {
	return "[" + reactionMark(reaction) + " " + strconv.Itoa(int(reaction.Count)) + "]"
}

// reactionMark is the emoji a chip shows, or the stand-in for a custom one.
func reactionMark(reaction *telegram.Reaction) string {
	if reaction.Emoji == "" {
		return customReactionMark
	}
	return reaction.Emoji
}
