package composer

import (
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/charmbracelet/lipgloss"
)

// The picker's rows (issue #41).
//
// The composer draws them and the host paints them: over the bottom of the
// thread, directly above the composer. They are not part of View and never
// count in Rows, so opening and closing the picker cannot move the composer
// or re-lay the thread above it.
//
//	col 0   selection bar
//	col 1   presence mark: ● online now, ◦ otherwise or not known
//	col 2   space
//	col 3.. name, padded to the longest on screen · gap · @username, "bot"
//	last    blank
const (
	// mentionPickerRows is the most members the picker lists at once.
	mentionPickerRows = 5
	// mentionRowLead is the bar, the mark and the space after them.
	mentionRowLead = 3
	// mentionRowGap separates the name from the @username.
	mentionRowGap = 2
	// mentionRowTrail is the blank cell at the right edge.
	mentionRowTrail = 1
)

// MentionPicker renders the open picker as rows of exactly width cells, no
// more than maxRows of them, ready to paint above the composer. ok is false
// when there is nothing to paint.
//
// The matches come best first, five at most, scrolled so the selected one is
// always on screen. On a short terminal the host passes a smaller maxRows,
// and the picker draws fewer rows rather than spill onto the composer.
//
// A one-line status follows them when there is something to say and a row
// to say it in; see mentionStatus. It never takes a row a match could have.
func (m Model) MentionPicker(width, maxRows int) (lines []string, ok bool) {
	s := m.mention
	if !s.active || width < 1 || maxRows < 1 {
		return nil, false
	}
	n := min(len(s.results), mentionPickerRows, maxRows)
	first := min(max(s.selected-n+1, 0), len(s.results)-n)
	shown := s.results[first : first+n]

	nameW, tagW := mentionColumns(shown, width)
	for i, u := range shown {
		lines = append(lines, m.mentionRow(u, first+i == s.selected, width, nameW, tagW))
	}
	if status, colour := m.mentionStatus(); status != "" && len(lines) < maxRows {
		lines = append(lines, m.mentionStatusRow(status, colour, width))
	}
	return lines, len(lines) > 0
}

// mentionStatus is the picker's one line about itself, if it has one.
//
// With nothing to offer there is always one — searching, or nobody matched —
// because an empty picker would look closed, and an Enter that then did not
// send would look broken. A failed search says so under the matches it
// failed to add to. Waiting says nothing under matches already on screen:
// the list would grow and shrink by a row on every key.
func (m Model) mentionStatus() (string, lipgloss.Color) {
	s := m.mention
	switch {
	case s.failed:
		return "⚠ member search failed", m.roles.Amber
	case len(s.results) > 0:
		return "", ""
	case s.loading:
		return "searching…", m.roles.Dim
	default:
		return "no matches", m.roles.Dim
	}
}

// mentionStatusRow draws the status line, indented to the names.
func (m Model) mentionStatusRow(text string, colour lipgloss.Color, width int) string {
	avail := max(width-mentionRowLead-mentionRowTrail, 0)
	line := strings.Repeat(" ", mentionRowLead) +
		lipgloss.NewStyle().Foreground(colour).Render(cell.Truncate(text, avail))
	return cell.Fill(m.roles.Panel, line, width)
}

// mentionColumns splits a row between the names and the @usernames, from the
// rows on screen: the names padded to the longest so the usernames line up
// after them, and — when the two do not fit — the usernames given up to two
// fifths of the row, the name being what a member is recognised by.
func mentionColumns(users []*telegram.User, width int) (nameW, tagW int) {
	avail := max(width-mentionRowLead-mentionRowTrail, 0)
	names, tags := 0, 0
	for _, u := range users {
		names = max(names, cell.Width(displayName(u)))
		tags = max(tags, cell.Width(mentionTag(u)))
	}
	switch {
	case tags == 0:
		return avail, 0
	case names == 0:
		// Known only by their usernames: those go where the names would.
		return 0, avail
	}
	tagW = min(tags, max(avail-names-mentionRowGap, avail*2/5))
	nameW = min(names, avail-mentionRowGap-tagW)
	if nameW < 1 || tagW < 1 {
		// Too narrow for two columns: the name alone.
		return avail, 0
	}
	return nameW, tagW
}

// mentionTag is what follows a member's name: the @username to type, and
// "bot" for a bot.
func mentionTag(u *telegram.User) string {
	handle, kind := mentionTagParts(u)
	return strings.TrimSpace(handle + " " + kind)
}

func mentionTagParts(u *telegram.User) (handle, kind string) {
	if u.Username != "" {
		handle = "@" + u.Username
	}
	if u.IsBot {
		kind = "bot"
	}
	return handle, kind
}

// mentionTagCell draws a member's tag in at most width cells: the @username
// in the mention colour, "bot" after it and dimmer. When the two do not
// fit, "bot" is what gives way — the username is what gets typed.
func (m Model) mentionTagCell(u *telegram.User, width int) string {
	handle, kind := mentionTagParts(u)
	blue := lipgloss.NewStyle().Foreground(m.roles.Blue)
	faint := lipgloss.NewStyle().Foreground(m.roles.Faint)
	switch {
	case handle == "" && kind == "":
		return ""
	case handle == "":
		return faint.Render(cell.Truncate(kind, width))
	case kind == "" || cell.Width(handle)+1+cell.Width(kind) > width:
		return blue.Render(cell.Truncate(handle, width))
	default:
		return blue.Render(handle) + faint.Render(" "+kind)
	}
}

// mentionRow draws one member.
//
// The selected row is marked the way the chat list marks its own: a bar at
// the edge, which reads without colour, the selection surface behind it,
// and the name brightened.
func (m Model) mentionRow(u *telegram.User, selected bool, width, nameW, tagW int) string {
	r := m.roles
	fg := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

	bar, barColour, nameColour, surface := " ", r.Ghost, r.Fg, r.Panel
	if selected {
		bar, barColour, nameColour, surface = "▌", r.Cyan, r.Bright, r.Sel
	}
	mark, markColour := "◦", r.Ghost
	if _, online := u.Status.(*telegram.UserStatusOnline); online {
		mark, markColour = "●", r.Green
	}

	line := fg(barColour).Render(bar) + fg(markColour).Render(mark) + " "
	if nameW > 0 {
		line += fg(nameColour).Render(cell.Fit(cell.Truncate(displayName(u), nameW), nameW))
		if tagW > 0 {
			line += strings.Repeat(" ", mentionRowGap)
		}
	}
	if tagW > 0 {
		line += m.mentionTagCell(u, tagW)
	}
	return cell.Fill(surface, line, width)
}
