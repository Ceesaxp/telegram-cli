package chatlist

import (
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/sigil"
	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
	"github.com/charmbracelet/lipgloss"
)

// The TUI 2.0 chat row is two lines on a fixed grid (docs/tui-2.0.md, "Top
// bar, chat list, and hint bar"). Offsets below are the ones measured out of
// the golden fixtures at a 38-cell column:
//
//	col  0      selection bar          selection bar
//	col  1      type sigil             (indent)
//	col  2      space                  (indent)
//	cols 3..    title                  preview
//	right       relative time (5)      @ chip and unread badge, right-aligned
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

	mark, markColour := m.rowSigil(item)

	title := item.Title
	// A row that has something to say about itself says it in words rather
	// than only by being dimmer, because "dimmer" is not readable in
	// isolation — you cannot tell a muted row from an ordinary one without
	// another row to compare it to. The title gives way to the marker,
	// never the other way round.
	if marker := m.rowTitleMarker(item); marker != "" {
		title = cell.Truncate(title, textW-cell.Width(marker)) + marker
	} else {
		title = cell.Truncate(title, textW)
	}

	titleColour := r.Dim
	switch {
	case selected:
		titleColour = r.Bright
	// A mention lifts the title even in a muted chat: it is the one thing
	// muting does not silence, as the @ chip says below.
	case (item.Badge != "" && !item.Muted) || item.Mention:
		titleColour = r.Fg
	}

	line1 := barStyle.Render(bar) +
		lipgloss.NewStyle().Foreground(markColour).Render(mark) +
		" " +
		lipgloss.NewStyle().Foreground(titleColour).Render(cell.Fit(title, textW)) +
		lipgloss.NewStyle().Foreground(r.Faint).Render(cell.Fit(item.Meta, rowTimeW))

	// --- line two: indent, preview, trail ---------------------------
	trail := m.renderTrail(item)
	trailW := cell.Width(trail)

	previewW := textW + rowTimeW - trailW
	if trailW > 0 {
		previewW-- // at least one cell between the preview and the trail
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
	if trailW > 0 {
		line2 += " " + trail
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

// rowSigil is the mark in column one and the colour it carries — the whole
// "what kind of thing is this" signal, since TUI 2.0 has no avatars.
//
// Split out of renderRow because the list draws rows from more than one
// source and each source knows its own marks. It is the seam the topic sigil
// hangs off; see [Model.topicSigil].
func (m Model) rowSigil(item widgets.ListItem) (string, lipgloss.Color) {
	if t := m.topicFor(item.ID); t != nil {
		return m.topicSigil(t)
	}
	return sigil.For(telegram.ChatType(item.Kind), item.Saved, m.roles)
}

// rowTitleMarker is the word a row appends to its own title when the title
// alone would not say what the row is: " muted" for a chat whose
// notifications are off, " closed" for a topic that cannot be posted to.
//
// The same shape for both, deliberately. A closed topic is the chat list's
// existing "this row is not quite an ordinary one" vocabulary, a quiet word
// after the title that reads on a terminal with no colour at all, and one
// more glyph in the sigil column would have been a second alphabet.
//
// Split out of renderRow for rowSigil's reason. The marker is applied by the
// caller, which owns the width budget it has to fit inside.
func (m Model) rowTitleMarker(item widgets.ListItem) string {
	if t := m.topicFor(item.ID); t != nil {
		if t.Closed {
			return " closed"
		}
		return ""
	}
	if item.Muted {
		return " muted"
	}
	return ""
}

// renderTrail is what sits right of the preview on row two: the chips that
// say the chat is waiting for the reader, the @ first and then the unread
// badge, a cell apart. The preview gives way to it.
func (m Model) renderTrail(item widgets.ListItem) string {
	chips := make([]string, 0, 2)
	if item.Mention {
		chips = append(chips, m.renderMention())
	}
	if badge := m.renderBadge(item); badge != "" {
		chips = append(chips, badge)
	}
	return strings.Join(chips, " ")
}

// renderMention draws the @ chip for a chat with an unread mention of the
// reader. It is always in the unmuted badge's colours: muting a chat
// silences it, but a mention is the one thing Telegram lets through, so
// the chip says so even on a row that is otherwise subdued.
func (m Model) renderMention() string {
	return m.badgeStyle(false).Render("@")
}

// renderBadge draws the unread chip.
//
// The brackets ARE the padding now — square for a chat that will interrupt
// you, round for one you have muted — so the width is the same as the
// spaces they replaced and the mute state survives being read on a terminal
// with no colour, or by somebody who cannot tell the two backgrounds apart.
//
// A muted chat's badge is present but subdued rather than absent: the count
// still matters, it just is not asking for attention.
func (m Model) renderBadge(item widgets.ListItem) string {
	if item.Badge == "" {
		return ""
	}
	return m.badgeStyle(item.Muted).Render(item.Badge)
}

// badgeStyle is the chip's colouring: cyan for a chat that will interrupt
// you, subdued for one you have muted.
func (m Model) badgeStyle(muted bool) lipgloss.Style {
	if muted {
		return lipgloss.NewStyle().Background(m.roles.Sel).Foreground(m.roles.Dim)
	}
	return lipgloss.NewStyle().Background(m.roles.Cyan).Foreground(m.roles.Bg)
}

// renderHeader is the chat list's first row, whichever list it is showing:
// the filter line over chats, the forum's name over topics. One row either
// way — see [Model.headerHeight].
func (m Model) renderHeader(width int) string {
	if m.inForum() {
		return m.renderForumHeader(width)
	}
	return m.renderFilterHeader(width)
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

	count := m.headerCount()
	queryW := width - rowTextCol - cell.Width(count) - rowTrailW
	if queryW < 0 {
		queryW = 0
	}

	line := " " +
		lipgloss.NewStyle().Foreground(r.Amber).Render("/") + " " +
		queryStyle.Render(cell.Fit(cell.Truncate(query, queryW), queryW)) +
		lipgloss.NewStyle().Foreground(r.Ghost).Render(count)

	return cell.Fit(line, width)
}

// backGlyph is the way out of a forum, in the place the filter's slash has
// over the chats: whatever leads this row is what the row is about.
const backGlyph = "‹"

// renderForumHeader is the first row while the list is drilled into a
// forum: the way back, the forum's name, the live query when there is one,
// and the same shown/total count the chats get.
//
// Both the name and the query, on one row, because dropping either would
// leave the row lying about the other. A query nobody can see is a field
// being typed into blind; a query with no forum beside it has stopped
// saying WHICH list it is narrowing, which is the whole reason this header
// replaced the filter line. They are separated by the same amber slash the
// chat list leads with, so the glyph means one thing in both states.
//
// Under pressure the NAME gives way and the query is kept whole: the reader
// pressed enter on that forum a moment ago and can see its topics, while
// the query is live text they are still editing.
func (m Model) renderForumHeader(width int) string {
	r := m.roles

	count := m.headerCount()
	textW := width - rowTextCol - cell.Width(count) - rowTrailW
	if textW < 0 {
		textW = 0
	}

	// The query takes room only once there is one. With no filter the row
	// is the forum's name and nothing else, which is the mock in
	// docs/topics.md.
	query, filtering := m.headerQuery()
	queryW := 0
	if filtering {
		queryW = cell.Width(query) + 3 // " / " and the query
	}

	nameW := textW - queryW
	if nameW < 0 {
		nameW = 0
	}
	name := cell.Truncate(m.ForumTitle(), nameW)

	field := lipgloss.NewStyle().Foreground(r.Bright).Render(name)
	used := cell.Width(name)
	if filtering {
		field += " " +
			lipgloss.NewStyle().Foreground(r.Amber).Render("/") + " " +
			lipgloss.NewStyle().Foreground(r.Fg).Render(query)
		used += queryW
	}
	if pad := textW - used; pad > 0 {
		field += strings.Repeat(" ", pad)
	}

	line := " " +
		lipgloss.NewStyle().Foreground(r.Amber).Render(backGlyph) + " " +
		field +
		lipgloss.NewStyle().Foreground(r.Ghost).Render(count)

	return cell.Fit(line, width)
}

// headerQuery is the filter as the header draws it — the applied query plus
// a block cursor while the input is open — and whether there is one to draw
// at all.
func (m Model) headerQuery() (string, bool) {
	if m.filter == "" && !m.filterInput.Focused {
		return "", false
	}
	if m.filterInput.Focused {
		return m.filter + "█", true
	}
	return m.filter, true
}

// headerCount is the "shown/total" at the right edge: how many rows the
// list is drawing over how many it had before the query. The same pair
// either side of the drill-in, because it describes the list rather than
// what the list is made of.
func (m Model) headerCount() string {
	return itoa(len(m.list.Items)) + "/" + itoa(m.folderTotal())
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
