// Package frame assembles the TUI 2.0 screen from panel content and a
// layout budget.
//
// It exists to make one property true by construction: every rendered row is
// exactly the terminal width. Panels hand it whatever they draw — too short,
// too long, styled, full of wide runes — and the frame fits each line to its
// region before joining regions with one-cell rules.
//
// That inversion is what makes the borderless design safe to adopt
// incrementally. The old frame relied on Lipgloss borders to absorb whatever
// slop a panel produced; remove the borders and every panel has to be
// exact-width on its own, which is a large simultaneous change. Here the
// frame does the fitting, so a panel that is not yet exact simply gets
// padded or clipped instead of shearing the screen.
package frame

import (
	"strings"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/layout"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// Column is one region's content: the lines it drew, the width it was
// budgeted, and the surface it is drawn on. Lines shorter than Height are
// padded with blanks; longer ones are clipped.
//
// Surface is the frame's second job, and the reason panels do not paint
// their own base colour. A panel can only fill the rows it drew; the frame
// also owns the rows it did NOT draw — the blank padding under a short chat
// list, the empty-state views that go through lipgloss's own Height, and the
// whole column in single-panel mode. Painting in one place is what makes
// "no cell falls through to the terminal's default" true of the screen
// rather than of most of it.
//
// Panels still paint their exceptions, and those win: [cell.Fill] reopens
// the surface before each span's own sequences, so a selected chat row that
// filled itself sel stays sel underneath a column filled with panel.
type Column struct {
	Width   int
	Surface lipgloss.Color
	Lines   []string
}

// Screen is everything the frame needs to draw.
type Screen struct {
	Layout layout.Layout
	Roles  theme.Roles

	// TopBar and HintBar are already-rendered full-width rows. Empty when
	// the layout says they are not shown.
	TopBar  string
	HintBar string

	// Body columns, left to right. A column with Width 0 is skipped along
	// with its rule.
	ChatList Column
	Thread   Column
	Rail     Column
}

// Render returns the complete screen: Layout.Height rows, each exactly
// Layout.Width cells.
func Render(s Screen) string {
	l := s.Layout
	rows := make([]string, 0, l.Height)

	if l.TopBar {
		rows = append(rows, cell.Fill(s.Roles.Chrome, s.TopBar, l.Width))
	}

	rule := lipgloss.NewStyle().
		Foreground(s.Roles.Rule).
		Background(s.Roles.Bg).
		Render("│")

	for y := range l.BodyHeight {
		var b strings.Builder

		if l.SinglePanel {
			// One column owns the width. The caller decides which panel is
			// showing — including its surface, since the thread arrives as
			// ChatList in this mode and is not drawn on panel.
			b.WriteString(fitRow(s.ChatList, y, l.Width))
		} else {
			b.WriteString(fitRow(s.ChatList, y, l.ChatListWidth))
			b.WriteString(rule)
			b.WriteString(fitRow(s.Thread, y, l.ThreadWidth))
			if l.RailWidth > 0 {
				b.WriteString(rule)
				b.WriteString(fitRow(s.Rail, y, l.RailWidth))
			}
		}

		rows = append(rows, cell.Fit(b.String(), l.Width))
	}

	if l.HintBar {
		rows = append(rows, cell.Fill(s.Roles.Chrome, s.HintBar, l.Width))
	}

	return strings.Join(rows, "\n")
}

// fitRow returns row y of a column, fitted to width and painted on the
// column's surface. A column that drew fewer lines than the body is padded
// with blanks rather than short-changing the frame — a missing row is a torn
// frame, and a blank one is merely empty.
//
// The padding goes through the same fill as the content, which is the point:
// a chat list that drew twelve rows into a forty-row body used to leave
// twenty-eight rows of terminal default below it, and that band is exactly
// what makes a panel look like it stops early.
func fitRow(c Column, y, width int) string {
	line := ""
	if y < len(c.Lines) {
		line = c.Lines[y]
	}
	return cell.Fill(c.Surface, line, width)
}

// Lines splits a panel's rendered output into rows for a [Column]. It exists
// so callers do not each re-derive how to turn a View() string into rows,
// including the trailing-newline case that would otherwise add a phantom
// blank row at the bottom of a panel.
func Lines(view string) []string {
	view = strings.TrimSuffix(view, "\n")
	if view == "" {
		return nil
	}
	return strings.Split(view, "\n")
}
