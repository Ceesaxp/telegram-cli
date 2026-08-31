package chatlist

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/sigil"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

// The TUI 2.0 chat row is two lines on a fixed grid (docs/tui-2.0.md, "Top
// bar, chat list, and hint bar"). Offsets below are the ones measured out of
// the golden fixtures at a 38-cell column:
//
//	col  0      selection bar          selection bar
//	col  1      type sigil             (indent)
//	col  2      space                  (indent)
//	cols 3..    title                  preview
//	right       relative time (5)      unread badge, right-aligned
//	last        blank                  blank
//
// The time sits in a FIXED five-cell field rather than being right-aligned
// against the column edge, which is what the goldens show: at 38 cells it
// starts at column 32 whether it reads "2m" or "14m". A fixed field keeps
// the times aligned with each other down the list, which is the point of
// having a column at all.
const (
	rowBarCol   = 0 // selection bar
	rowSigilCol = 1
	rowTextCol  = 3 // title and preview both start here
	rowTimeW    = 5 // relative-time field
	rowTrailW   = 1 // blank cell at the right edge
)

// renderRow draws one chat as two exact-width lines.
//
// Every field has a budget and every budget is in display cells, so a title
// full of emoji cannot push the time out of its column — the failure mode
// this whole grid exists to prevent.
func (m Model) renderRow(item widgets.ListItem, selected, focused bool, width int) []string {
	r := m.roles

	textW := width - rowTextCol - rowTimeW - rowTrailW
	if textW < 1 {
		textW = 1
	}

	// --- line one: bar, sigil, title, time --------------------------
	bar := " "
	barStyle := lipgloss.NewStyle().Foreground(r.Ghost)
	if selected {
		bar = "▌"
		if focused {
			// Focus is the cyan bar and nothing else — TUI 2.0 has no
			// focused-panel border to carry it.
			barStyle = lipgloss.NewStyle().Foreground(r.Cyan)
		}
	}

	mark, markColour := sigil.For(telegram.ChatType(item.Kind), item.Saved, r)

	title := item.Title
	// A muted chat says so in words rather than only by being dimmer,
	// because "dimmer" is not readable in isolation — you cannot tell a
	// muted row from an ordinary one without another row to compare it to.
	// The title gives way to the marker, never the other way round.
	if item.Muted {
		const marker = " muted"
		title = cell.Truncate(title, textW-cell.Width(marker)) + marker
	} else {
		title = cell.Truncate(title, textW)
	}

	titleColour := r.Dim
	switch {
	case selected:
		titleColour = r.Bright
	case item.Badge != "" && !item.Muted:
		titleColour = r.Fg
	}

	line1 := barStyle.Render(bar) +
		lipgloss.NewStyle().Foreground(markColour).Render(mark) +
		" " +
		lipgloss.NewStyle().Foreground(titleColour).Render(cell.Fit(title, textW)) +
		lipgloss.NewStyle().Foreground(r.Faint).Render(cell.Fit(item.Meta, rowTimeW))

	// --- line two: indent, preview, badge ---------------------------
	badge := m.renderBadge(item)
	badgeW := cell.Width(badge)

	previewW := textW + rowTimeW - badgeW
	if badgeW > 0 {
		previewW-- // at least one cell between the preview and the badge
	}
	if previewW < 0 {
		previewW = 0
	}

	preview := lipgloss.NewStyle().Foreground(r.Faint).
		Render(cell.Fit(cell.Truncate(item.Subtitle, previewW), previewW))

	// The bar runs down BOTH rows of the chat. A mark on the title line
	// only marks the title; the preview underneath it reads as belonging to
	// no row in particular, which is exactly how a two-line row loses its
	// selection.
	line2 := barStyle.Render(bar) + strings.Repeat(" ", rowTextCol-1) + preview
	if badgeW > 0 {
		line2 += " " + badge
	}

	// Only the selected row paints. Panel is the column's surface and the
	// frame fills it, including the rows below the last chat — filling it
	// again here would be a second mechanism for one rule.
	if selected {
		return []string{
			cell.Fill(r.Sel, line1, width),
			cell.Fill(r.Sel, line2, width),
		}
	}
	return []string{cell.Fit(line1, width), cell.Fit(line2, width)}
}

// renderBadge draws the unread chip: the count with one cell of padding
// either side, so its width is len(count)+2.
//
// A muted chat's badge is present but subdued rather than absent — the count
// still matters, it just is not asking for attention.
func (m Model) renderBadge(item widgets.ListItem) string {
	if item.Badge == "" {
		return ""
	}
	style := lipgloss.NewStyle().Background(m.roles.Cyan).Foreground(m.roles.Bg)
	if item.Muted {
		style = lipgloss.NewStyle().Background(m.roles.Sel).Foreground(m.roles.Dim)
	}
	return style.Render(" " + item.Badge + " ")
}

// renderFilterHeader is the chat list's first row: an amber slash, the live
// query or a placeholder, and the matching/total count at the right edge.
func (m Model) renderFilterHeader(width int) string {
	r := m.roles

	query := m.filter
	queryStyle := lipgloss.NewStyle().Foreground(r.Fg)
	if query == "" && !m.filterInput.Focused {
		query = "filter chats…"
		queryStyle = lipgloss.NewStyle().Foreground(r.Dim)
	}
	if m.filterInput.Focused {
		query += "█"
	}

	count := itoa(len(m.list.Items)) + "/" + itoa(m.storeChatCount())
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

// renderListFooter is the chat list's last row: the motions that work here.
// It is local to the panel, unlike the frame's hint bar, because these keys
// only do anything while this column has focus.
func (m Model) renderListFooter(width int) string {
	r := m.roles
	key := lipgloss.NewStyle().Foreground(r.Cyan)
	label := lipgloss.NewStyle().Foreground(r.Faint)

	hint := func(k, l string) string {
		return key.Render(k) + " " + label.Render(l)
	}

	// While a filter is applied the way OUT of it is the only thing worth
	// the row: the motions are unchanged and already known, but a user who
	// cannot see how to clear a filter is stuck looking at a partial list
	// and wondering where their chats went. The old filter chip carried
	// this hint; the footer inherits it now that the chip is gone.
	var line string
	switch {
	case m.filtering:
		line = " " + hint("esc", "clear") + "  " + hint("enter", "keep")
	case m.filter != "":
		line = " " + hint("esc", "clear filter") + "  " + hint("j/k", "move")
	default:
		line = " " + hint("j/k", "move") + "  " + hint("g/G", "ends") + "  " +
			hint("u", "unread")
	}

	return cell.Fit(line, width)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		n = 0
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
