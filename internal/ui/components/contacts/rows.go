package contacts

import (
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/sigil"
	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
	"github.com/charmbracelet/lipgloss"
)

// Contacts borrows the chat list's column, so it draws the chat list's grid
// (docs/tui-2.0.md, "Top bar, chat list, and hint bar"). It used to draw an
// overlay instead — a bold "Contacts" heading over the widget's default rows,
// each painting its own panel background — which read as a different
// application in the same column, from the era when every overlay had its own
// palette.
//
// The offsets are the chat list's, deliberately identical rather than
// approximately so: the two surfaces swap into one region, and a reader
// whose eye has learned where a name starts should not have to relearn it
// because the column changed what it is listing.
//
//	col  0      selection bar          selection bar
//	col  1      type sigil             (indent)
//	col  2      space                  (indent)
//	cols 3..    name                   @username
//	right       online dot (5)         blank
//	last        blank                  blank
const (
	rowBarCol    = 0 // selection bar
	rowSigilCol  = 1
	rowTextCol   = 3 // name and username both start here
	rowStatusW   = 5 // online field, the chat list's relative-time column
	rowTrailW    = 1 // blank cell at the right edge
	onlineMarker = "●"
)

// renderRow draws one contact as two exact-width lines.
//
// Every field has a budget and every budget is in display cells, so a name
// full of emoji cannot push the status out of its column.
func (m Model) renderRow(item widgets.ListItem, selected, focused bool, width int) []string {
	r := m.roles

	textW := width - rowTextCol - rowStatusW - rowTrailW
	if textW < 1 {
		textW = 1
	}

	bar := " "
	barStyle := lipgloss.NewStyle().Foreground(r.Ghost)
	if selected {
		bar = "▌"
		if focused {
			// Focus is the cyan bar and nothing else, exactly as in the
			// chat list — TUI 2.0 has no focused-panel border to carry it.
			barStyle = lipgloss.NewStyle().Foreground(r.Cyan)
		}
	}

	// Every contact is a private chat, so every row takes the private
	// sigil. Reading it from sigil.For rather than writing "@" here is
	// what keeps it the same glyph and the same blue as the chat list's
	// private rows, including through a theme change.
	mark, markColour := sigil.For(telegram.ChatTypePrivate, false, r)

	nameColour := r.Fg
	if selected {
		nameColour = r.Bright
	}

	// The status field is the chat list's time column: fixed width, so the
	// names above and below stay aligned whether or not anyone is online.
	status := ""
	if item.Online {
		status = onlineMarker
	}
	statusStyle := lipgloss.NewStyle().Foreground(r.Green)

	line1 := barStyle.Render(bar) +
		lipgloss.NewStyle().Foreground(markColour).Render(mark) +
		" " +
		lipgloss.NewStyle().Foreground(nameColour).
			Render(cell.Fit(cell.Truncate(item.Title, textW), textW)) +
		statusStyle.Render(cell.Fit(status, rowStatusW))

	// Line two is the username, in the preview's place. A contact without
	// one gets an empty row rather than a collapsed one: the grid is what
	// makes the column scannable, and a list whose rows are sometimes one
	// line and sometimes two is not a grid.
	usernameW := textW + rowStatusW
	username := lipgloss.NewStyle().Foreground(r.Faint).
		Render(cell.Fit(cell.Truncate(item.Subtitle, usernameW), usernameW))

	line2 := barStyle.Render(bar) + strings.Repeat(" ", rowTextCol-1) + username

	// Only the selected row paints. Panel is the column's surface and the
	// frame fills it, including the rows below the last contact — filling
	// it again here would be a second mechanism for one rule, and the one
	// that cannot cover the padding.
	if selected {
		return []string{
			cell.Fill(r.Sel, line1, width),
			cell.Fill(r.Sel, line2, width),
		}
	}
	return []string{cell.Fit(line1, width), cell.Fit(line2, width)}
}

// renderFilterHeader is the column's first row while contacts is up: an
// amber slash, the live query or a placeholder naming the surface, and the
// matching/total count at the right edge.
//
// The same shape as the chat list's header, for the same reason the rows
// are: one column, one grammar. The placeholder is what says which list you
// are looking at, now that the bold "Contacts" heading is gone — it names
// the surface in the row that also lets you narrow it, rather than spending
// a row on a title.
func (m Model) renderFilterHeader(width int) string {
	r := m.roles

	query := m.filter
	queryStyle := lipgloss.NewStyle().Foreground(r.Fg)
	if query == "" && !m.filterInput.Focused {
		query = "filter contacts…"
		queryStyle = lipgloss.NewStyle().Foreground(r.Dim)
	}
	if m.filterInput.Focused {
		query += "█"
	}

	count := itoa(len(m.list.Items)) + "/" + itoa(len(m.all))
	countW := cell.Width(count)

	queryW := width - rowTextCol - countW - rowTrailW
	if queryW < 0 {
		queryW = 0
	}

	line := " " +
		lipgloss.NewStyle().Foreground(r.Amber).Render("/") + " " +
		queryStyle.Render(cell.Fit(cell.Truncate(query, queryW), queryW)) +
		lipgloss.NewStyle().Foreground(r.Ghost).Render(count)

	return cell.Fit(line, width)
}

// itoa is strconv.Itoa without the import, matching the chat list's own.
func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
