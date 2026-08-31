// Package topbar renders the frame's top chrome row: the app mark, the
// folder tabs, and the connection group with a clock.
//
// The folder tabs move here from the chat list. Selection and key handling
// stay in chatlist — this package only draws — so the tabs can leave the
// list's 38-cell column without the folder keymap changing at all.
package topbar

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// ConnState is the connection dot's meaning.
type ConnState int

const (
	Disconnected ConnState = iota
	Connecting
	Connected
)

// Folder is one tab: a display name and whether it is the active one.
type Folder struct {
	Name   string
	Active bool
}

// Model is the top bar.
type Model struct {
	roles theme.Roles
	width int

	folders []Folder
	state   ConnState
	status  string // "connected", "connecting", ...
	clock   string // "21:04"

	// devices is how many sessions are authorised on the account, or 0
	// while the answer has not arrived. Zero drops the cell rather than
	// drawing "devices 0": every account has at least this session, so a
	// zero is always "not asked yet" and never a fact.
	//
	// This was half of decision 7's placeholder pair. The other half — a
	// transport version — is gone: gotd speaks MTProto 2.0 and nothing
	// else, so the cell could only ever have shown one string, and a
	// constant in a status area is decoration wearing the clothes of
	// information.
	devices int
}

func New(roles theme.Roles) Model {
	return Model{roles: roles, status: "connecting"}
}

func (m *Model) SetWidth(w int)        { m.width = w }
func (m *Model) SetFolders(f []Folder) { m.folders = f }
func (m *Model) SetClock(s string)     { m.clock = s }

// SetDevices sets the authorised-session count. Zero means "unknown" and
// drops the cell.
func (m *Model) SetDevices(n int) {
	if n < 0 {
		n = 0
	}
	m.devices = n
}

// SetConnection sets the dot and its description together, so the two can
// never disagree about whether the client is connected.
func (m *Model) SetConnection(state ConnState, status string) {
	m.state, m.status = state, status
}

// View renders the row at exactly the configured width.
//
// The right group degrades from the left as space runs out: the status
// description truncates first, then the device count is dropped, then the
// transport. The clock is never dropped — it is the one thing on the row
// that is useful when everything else has been cut.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	// No style here carries a background. Chrome is this row's surface and
	// the frame fills it — repeating it on every span was how this row
	// stayed continuous while every other surface in the app was breaking
	// at the first reset, and it is not a pattern worth keeping now that
	// there is one that works.
	markStyle := lipgloss.NewStyle().Foreground(m.roles.Cyan).Bold(true)
	ghostStyle := lipgloss.NewStyle().Foreground(m.roles.Ghost)
	// The active tab is marked by a BACKGROUND, not only by a brighter
	// foreground. Folder names come from Telegram and are very often a
	// single colour emoji, and a colour emoji ignores the foreground it is
	// given — so "the active folder is bright, the others are dim" marked
	// nothing at all on a folder list made of pictures. A background is
	// behind the glyph rather than in it, so it shows whatever the glyph
	// decides to be.
	activeStyle := lipgloss.NewStyle().Foreground(m.roles.Bright).Background(m.roles.Sel)
	idleStyle := lipgloss.NewStyle().Foreground(m.roles.Dim)
	dotStyle := lipgloss.NewStyle().Foreground(m.dotColour())
	faintStyle := lipgloss.NewStyle().Foreground(m.roles.Faint)

	left := " " + markStyle.Render("tg") + " " + ghostStyle.Render("│")
	leftW := 1 + 2 + 1 + 1

	// The right group claims its space BEFORE the tabs get any, because the
	// clock must never be dropped and the tabs are the only variable-length
	// thing on the row. Budgeting the other way round lets a long folder
	// list push the time off the end — which is exactly what it did before
	// TestClockIsNeverDropped caught it.
	right, rightW := m.rightGroup(m.width-leftW, dotStyle, faintStyle, ghostStyle)

	// Folder tabs: "1:all 2:unread ...". Numbered so the digit keys that
	// select them are visible rather than remembered.
	tabBudget := m.width - leftW - rightW
	var tabs strings.Builder
	reserved := 0
	for i, f := range m.folders {
		label := itoa(i+1) + ":" + f.Name
		if reserved+1+reservedWidth(label) > tabBudget {
			// Whole tabs drop, and the last one is clipped only if not even
			// one fits — a half-written folder name is worse than a missing
			// one, since it reads as a different folder.
			break
		}
		style := idleStyle
		if f.Active {
			style = activeStyle
		}
		tabs.WriteString(" ")
		tabs.WriteString(style.Render(label))
		reserved += 1 + reservedWidth(label)
	}

	// The gap is measured from what the tabs ACTUALLY drew, not from what
	// was reserved for them, so an over-reservation shows up as a slightly
	// wider gap rather than as a row that does not add up.
	gap := m.width - leftW - cell.Width(tabs.String()) - rightW
	if gap < 0 {
		gap = 0
	}

	out := left + tabs.String() + strings.Repeat(" ", gap) + right
	return cell.Fit(out, m.width)
}

// reservedWidth is how much room to keep for a label: its measured width,
// plus one cell for every grapheme whose rendered width the Unicode tables
// cannot be trusted to predict.
//
// Emoji width is not a property of the string. A terminal decides it, and
// terminals disagree — a regional-indicator pair is one flag in some and two
// letter-boxes in others, and a base character followed by U+FE0F is narrow
// where the presentation selector is ignored and wide where it is not. This
// row cannot ask, and there is no environment variable that answers.
//
// So it reserves pessimistically, in one direction only. Over-reserving costs
// a wider gap or one tab dropped early — visible, harmless, and self-evident.
// Under-reserving lets the tabs run past their budget and overwrite the
// connection status beside them, which is what put "nnected" on somebody's
// top bar. Given the choice between a gap and a corrupted row, this takes the
// gap.
//
// Only sequences with a *composition* rule are counted: those are where a
// terminal that composes and a terminal that does not produce different
// widths. A lone wide glyph is measured correctly by everyone.
func reservedWidth(label string) int {
	extra := 0
	for _, r := range label {
		switch {
		case r == 0xFE0F: // VARIATION SELECTOR-16, emoji presentation
			extra++
		case r == 0x200D: // ZERO WIDTH JOINER
			extra++
		case r >= 0x1F1E6 && r <= 0x1F1FF: // REGIONAL INDICATOR
			extra++
		}
	}
	return cell.Width(label) + extra
}

// TabAt returns the index of the folder tab drawn at column x, or -1.
//
// It recomputes the same spans View lays out rather than remembering them,
// so the hit-test cannot drift from the drawing: any change to the tab
// layout has to be made in tabSpans, which both callers go through.
func (m Model) TabAt(x int) int {
	for _, s := range m.tabSpans() {
		if x >= s.start && x < s.end {
			return s.index
		}
	}
	return -1
}

type tabSpan struct {
	index      int
	start, end int // half-open column range
}

// tabSpans computes each visible tab's column range, applying the same
// budget View does — including dropping tabs that do not fit, so a click
// past the last visible tab is not attributed to one that was never drawn.
func (m Model) tabSpans() []tabSpan {
	if m.width <= 0 {
		return nil
	}

	leftW := 1 + 2 + 1 + 1
	_, rightW := m.rightGroup(m.width-leftW,
		lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle())
	budget := m.width - leftW - rightW

	// Two accumulators, because they answer two questions. `reserved` is
	// how much room the tabs were given, which decides how many of them
	// fit; `drawn` is how wide they actually came out, which is where the
	// columns are. Using the reservation for both put every span to the
	// right of the tab it names as soon as one label was over-reserved.
	var spans []tabSpan
	reserved, drawn := 0, 0
	for i, f := range m.folders {
		label := itoa(i+1) + ":" + f.Name
		if reserved+1+reservedWidth(label) > budget {
			break
		}
		w := cell.Width(label)
		// The leading space belongs to the gap, not the label, so clicking
		// between two tabs hits neither.
		spans = append(spans, tabSpan{
			index: i,
			start: leftW + drawn + 1,
			end:   leftW + drawn + 1 + w,
		})
		reserved += 1 + reservedWidth(label)
		drawn += 1 + w
	}
	return spans
}

// rightGroup renders the widest connection group that fits in budget, and
// returns it with its display width.
func (m Model) rightGroup(budget int, dot, faint, ghost lipgloss.Style) (string, int) {
	sep := " · "

	// Widest to narrowest. Each candidate is the plain text after the dot.
	candidates := []string{}
	if m.devices > 0 {
		candidates = append(candidates, m.status+sep+m.deviceLabel())
	}
	candidates = append(candidates, m.status, "")

	for _, text := range candidates {
		// "● " + text + " │ " + clock + " "
		w := 2 + cell.Width(text) + 3 + cell.Width(m.clock) + 1
		if w <= budget {
			out := dot.Render("●") + faint.Render(" "+text) +
				ghost.Render(" │ ") + faint.Render(m.clock) + faint.Render(" ")
			return out, w
		}
	}

	// Nothing but the clock fits; it is never dropped.
	out := faint.Render(m.clock) + faint.Render(" ")
	return out, cell.Width(m.clock) + 1
}

// deviceLabel is the cell's text. Singular for one, because "devices 1" is
// the kind of wording that reads as a placeholder even when it is true.
func (m Model) deviceLabel() string {
	if m.devices == 1 {
		return "1 device"
	}
	return itoa(m.devices) + " devices"
}

func (m Model) dotColour() lipgloss.Color {
	switch m.state {
	case Connected:
		return m.roles.Green
	case Connecting:
		return m.roles.Amber
	default:
		return m.roles.Red
	}
}

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
