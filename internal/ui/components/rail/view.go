// Package rail draws the TUI 2.0 right-hand context rail: what is pinned in
// the open chat, who is in it, and what has been shared there.
//
// It replaces the modal group-info overlay. The point of the change is that
// context stops being something you leave the conversation to look at — the
// overlay covered the chat list, so checking who was in a group meant losing
// sight of the group.
package rail

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// The rail's grid, measured out of the goldens at 30 cells:
//
//	col  0      leading space
//	col  1      row glyph, or the first letter of a section heading
//	col  2      space
//	cols 3..    row text
//	right       trailing field, right-aligned, ending one cell short
//	last        blank
//
// A heading starts at column 1, where a row's glyph is — so a heading reads
// as the thing the rows below it hang from rather than as another row.
const (
	railGlyphCol = 1
	railTextCol  = 3
	railTrailW   = 1

	// railMinGap is the gap between a row's text and its right-hand field.
	// Without it a full-width name runs into the size beside it and the two
	// read as one string.
	railMinGap = 1
)

// RowKind selects a row's glyph and colour, so the palette stays inside this
// package rather than being decided by whoever assembled the data.
type RowKind int

const (
	// RowPinned is a pinned message: amber bullet, muted text, ghost author.
	RowPinned RowKind = iota
	// RowMemberOnline and RowMemberOffline are people, filled or hollow.
	RowMemberOnline
	RowMemberOffline
	// RowFile is a shared document.
	RowFile
	// RowFileImage is a shared file that is a picture. It gets the mark
	// the media card gives a photo, so the same file is the same glyph
	// wherever it is drawn.
	RowFileImage
	// RowLink is a shared link.
	RowLink
	// RowMore is the "+N more" remainder under a capped list.
	RowMore
	// RowNote is an honest state line: loading, empty, or unavailable.
	RowNote
)

// Row is one line of a section.
type Row struct {
	Kind RowKind
	// Text is the row's main content, elided when it does not fit.
	Text string
	// Right is the trailing field: a role, a last-seen, a size. Empty for
	// rows that have none.
	Right string
	// ID is the identity a colour is hashed from, for member rows. Zero
	// elsewhere.
	ID int64
}

// Section is one titled group of rows.
type Section struct {
	// Title is the heading, drawn upper case.
	Title string
	// Count is shown after the title as "· N" when non-zero. It is the
	// TOTAL, which is often larger than len(Rows) — that difference is the
	// whole reason to show it.
	Count int
	Rows  []Row
}

// Model is the rail.
type Model struct {
	width  int
	height int
	roles  theme.Roles

	store *store.Store
	tg    *telegram.Client

	// chatID is the chat the rail is pointing at, 0 when it is closed.
	chatID int64

	// gen is bumped by Invalidate. Every in-flight request carries the
	// generation it was started for, and a result from an older one is
	// dropped rather than folded into a cache it no longer describes.
	gen int

	// data caches each chat's sections. It is a map so the value copies
	// bubbletea makes of this model all share one — the same reason
	// chatview holds its render cache by pointer.
	data map[int64]*chatData
}

// New builds a rail with a default palette, so a Model that never has
// SetRoles called still renders.
func New(roles theme.Roles) Model {
	return Model{roles: roles, width: 30, data: map[int64]*chatData{}}
}

// SetSize sets the rail's dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetRoles supplies the TUI 2.0 semantic palette.
func (m *Model) SetRoles(r theme.Roles) { m.roles = r }

// View renders the rail as height rows, each exactly width cells.
func (m Model) View() string {
	if m.width < 1 || m.height < 1 {
		return ""
	}

	var lines []string
	for i, section := range m.Sections() {
		if i > 0 {
			// A blank row between sections. Sections are the rail's
			// structure, and without space around them a heading is just
			// another row that happens to be upper case.
			lines = append(lines, m.blank())
		}
		lines = append(lines, m.heading(section))
		for _, row := range section.Rows {
			lines = append(lines, m.row(row))
		}
	}

	for len(lines) < m.height {
		lines = append(lines, m.blank())
	}
	return strings.Join(lines[:m.height], "\n")
}

// blank is a row the rail did not fill. It carries no colour of its own:
// panel is this column's surface and the frame paints it, including the rows
// below the last section.
func (m Model) blank() string {
	return strings.Repeat(" ", m.width)
}

// heading draws a section title, with its total when there is one.
func (m Model) heading(s Section) string {
	r := m.roles
	text := strings.ToUpper(s.Title)
	if s.Count > 0 {
		text += " · " + strconv.Itoa(s.Count)
	}
	line := strings.Repeat(" ", railGlyphCol) +
		lipgloss.NewStyle().Foreground(r.Faint).Render(
			cell.Truncate(text, m.width-railGlyphCol-railTrailW))
	return cell.Fit(line, m.width)
}

// row draws one line: glyph, text, and a right-aligned trailing field.
//
// The text gives way to the trailing field, never the other way round. A
// truncated filename is still recognisable; a missing size is a fact the
// reader cannot recover from this screen at all — the same rule the collapsed
// media card follows.
func (m Model) row(row Row) string {
	r := m.roles
	glyph, glyphColour := m.glyphFor(row)

	rightW := cell.Width(row.Right)
	if rightW > 0 {
		rightW += railMinGap
	}
	textW := m.width - railTextCol - rightW - railTrailW
	if textW < 1 {
		textW = 1
	}

	line := strings.Repeat(" ", railGlyphCol) +
		lipgloss.NewStyle().Foreground(glyphColour).Render(glyph) + " " +
		lipgloss.NewStyle().Foreground(m.textColour(row)).
			Render(cell.Fit(cell.Truncate(row.Text, textW), textW))

	if row.Right != "" {
		line += strings.Repeat(" ", railMinGap) +
			lipgloss.NewStyle().Foreground(r.Faint).Render(row.Right)
	}
	return cell.Fit(line, m.width)
}

func (m Model) glyphFor(row Row) (string, lipgloss.Color) {
	r := m.roles
	switch row.Kind {
	case RowPinned:
		return "▪", r.Amber
	case RowMemberOnline:
		return "●", r.Green
	case RowMemberOffline:
		return "○", r.Ghost
	case RowFile:
		return "▤", r.Amber
	case RowFileImage:
		return "▣", r.Amber
	case RowLink:
		return "▹", r.Cyan
	default:
		// RowMore and RowNote: a middle dot, because neither is a thing you
		// can act on — one counts what is not shown and the other says why
		// nothing is.
		return "·", r.Ghost
	}
}

// textColour: a member's name carries their identity colour, the same hash
// the thread grid uses, so the same person is the same colour in both.
func (m Model) textColour(row Row) lipgloss.Color {
	r := m.roles
	switch row.Kind {
	case RowMemberOnline, RowMemberOffline:
		if row.ID != 0 {
			return theme.SenderColour(row.ID, r)
		}
		return r.Fg
	case RowPinned:
		return r.Dim
	case RowMore, RowNote:
		return r.Ghost
	default:
		return r.Fg
	}
}
