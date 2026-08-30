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

	// transport and devices are DEFERRED placeholders (decision 7). Neither
	// has a source in the current connection state, so they are set from a
	// literal and must be wired or removed before release — see TODO.md.
	// They exist now only so the shrink order and the goldens are settled.
	transport string
	devices   string
}

func New(roles theme.Roles) Model {
	return Model{roles: roles, status: "connecting"}
}

func (m *Model) SetWidth(w int)        { m.width = w }
func (m *Model) SetFolders(f []Folder) { m.folders = f }
func (m *Model) SetClock(s string)     { m.clock = s }
func (m *Model) SetPlaceholders(transport, devices string) {
	m.transport, m.devices = transport, devices
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

	base := lipgloss.NewStyle().Background(m.roles.Chrome)
	markStyle := lipgloss.NewStyle().Foreground(m.roles.Cyan).Bold(true).Background(m.roles.Chrome)
	ghostStyle := lipgloss.NewStyle().Foreground(m.roles.Ghost).Background(m.roles.Chrome)
	activeStyle := lipgloss.NewStyle().Foreground(m.roles.Bright).Background(m.roles.Chrome)
	idleStyle := lipgloss.NewStyle().Foreground(m.roles.Dim).Background(m.roles.Chrome)
	dotStyle := lipgloss.NewStyle().Foreground(m.dotColour()).Background(m.roles.Chrome)
	faintStyle := lipgloss.NewStyle().Foreground(m.roles.Faint).Background(m.roles.Chrome)

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
	tabsW := 0
	for i, f := range m.folders {
		label := itoa(i+1) + ":" + f.Name
		if tabsW+1+cell.Width(label) > tabBudget {
			// Whole tabs drop, and the last one is clipped only if not even
			// one fits — a half-written folder name is worse than a missing
			// one, since it reads as a different folder.
			break
		}
		style := idleStyle
		if f.Active {
			style = activeStyle
		}
		tabs.WriteString(base.Render(" "))
		tabs.WriteString(style.Render(label))
		tabsW += 1 + cell.Width(label)
	}

	gap := m.width - leftW - tabsW - rightW
	if gap < 0 {
		gap = 0
	}

	out := left + tabs.String() + base.Render(strings.Repeat(" ", gap)) + right
	return cell.Fit(out, m.width)
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

	var spans []tabSpan
	used := 0
	for i, f := range m.folders {
		label := itoa(i+1) + ":" + f.Name
		w := 1 + cell.Width(label)
		if used+w > budget {
			break
		}
		// The leading space belongs to the gap, not the label, so clicking
		// between two tabs hits neither.
		spans = append(spans, tabSpan{
			index: i,
			start: leftW + used + 1,
			end:   leftW + used + w,
		})
		used += w
	}
	return spans
}

// rightGroup renders the widest connection group that fits in budget, and
// returns it with its display width.
func (m Model) rightGroup(budget int, dot, faint, ghost lipgloss.Style) (string, int) {
	sep := " · "

	// Widest to narrowest. Each candidate is the plain text after the dot.
	candidates := []string{}
	if m.transport != "" && m.devices != "" {
		candidates = append(candidates, m.status+sep+m.transport+sep+m.devices)
	}
	if m.transport != "" {
		candidates = append(candidates, m.status+sep+m.transport)
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
