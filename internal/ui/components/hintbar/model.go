// Package hintbar renders the frame's bottom chrome row: context-sensitive
// key hints on the left, counters on the right.
//
// It replaces the old status bar's hint strip. Connection state moved to the
// top bar, so this row is purely "what can I press here" plus "how much is
// there", and it changes with the interaction mode rather than only with
// focus.
package hintbar

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// MinGap is the smallest space allowed between the last hint and the right
// group. Derived from the goldens: at 80 columns the bar keeps four hints
// and exactly five cells of gap, and a fifth hint would not fit.
const MinGap = 5

// Hint is one "key label" pair. Key is rendered in the accent colour and
// label in faint, so the eye can scan keys without reading the labels.
type Hint struct {
	Key   string
	Label string
}

func (h Hint) String() string { return h.Key + " " + h.Label }

// Model is the hint bar.
type Model struct {
	roles theme.Roles
	width int

	hints []Hint
	right string

	// notice, when set, owns the whole row. A transient error or progress
	// message is more important than a hint the user already knows, and the
	// design gives it the row for four seconds (docs/tui-2.0.md).
	notice      string
	noticeStyle lipgloss.Style
}

func New(roles theme.Roles) Model {
	return Model{roles: roles}
}

func (m *Model) SetWidth(w int)    { m.width = w }
func (m *Model) SetHints(h []Hint) { m.hints = h }
func (m *Model) SetRight(s string) { m.right = s }
func (m *Model) ClearNotice()      { m.notice = "" }

// SetNotice hands the row to a transient message. kind selects the colour:
// "error" is red, anything else amber for progress.
func (m *Model) SetNotice(text, kind string) {
	m.notice = text
	colour := m.roles.Amber
	if kind == "error" {
		colour = m.roles.Red
	}
	m.noticeStyle = lipgloss.NewStyle().Foreground(colour).Background(m.roles.Chrome)
}

// View renders the row at exactly the configured width.
//
// The layout is: one leading space, hints joined by two spaces, padding, the
// right group, one trailing space. Hints are taken IN ORDER and the bar
// keeps the longest prefix that still leaves [MinGap] cells before the right
// group — so a hint is dropped whole, from the right, and never truncated
// mid-word.
//
// Because it is a prefix, removing a hint from the middle of the set lets
// the next one in at narrow widths. That is the intended behaviour and it is
// what the goldens show: dropping "t thread" gained "e edit" at 100 columns
// rather than just widening the gap.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	if m.notice != "" {
		return cell.FitLine(m.noticeStyle, " "+m.notice, m.width)
	}

	// No background on any of these: chrome is this row's surface and the
	// frame fills it.
	keyStyle := lipgloss.NewStyle().Foreground(m.roles.Cyan)
	labelStyle := lipgloss.NewStyle().Foreground(m.roles.Faint)
	rightStyle := lipgloss.NewStyle().Foreground(m.roles.Ghost)

	rightW := cell.Width(m.right)
	if m.right != "" {
		rightW++ // the trailing space
	}

	// Longest prefix of hints that still leaves MinGap before the right
	// group. Measured on the plain text, since styling adds no cells.
	kept := 0
	for n := len(m.hints); n >= 1; n-- {
		if 1+plainWidth(m.hints[:n])+MinGap+rightW <= m.width {
			kept = n
			break
		}
	}

	var b strings.Builder
	b.WriteString(" ")
	for i, h := range m.hints[:kept] {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(keyStyle.Render(h.Key))
		b.WriteString(" ")
		b.WriteString(labelStyle.Render(h.Label))
	}

	left := b.String()
	gap := m.width - cell.Width(left) - rightW
	if gap < 0 {
		gap = 0
	}

	out := left + strings.Repeat(" ", gap)
	if m.right != "" {
		out += rightStyle.Render(m.right) + " "
	}
	return cell.Fit(out, m.width)
}

// plainWidth measures the hints as they will be drawn: joined by two spaces,
// each key and label separated by one.
func plainWidth(hints []Hint) int {
	w := 0
	for i, h := range hints {
		if i > 0 {
			w += 2
		}
		w += cell.Width(h.String())
	}
	return w
}
