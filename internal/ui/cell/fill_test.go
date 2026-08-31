package cell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// A row assembled the way every panel assembles one: plain lead, a styled
// span that closes itself, more plain text, another styled span. Under the
// old spelling the background died at the first of those resets.
func styledRow() string {
	return " " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("239")).Render("▌") +
		" " +
		lipgloss.NewStyle().Foreground(lipgloss.Color("110")).Render("@") +
		" title" +
		lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("18:51")
}

func TestFillPaintsEveryCell(t *testing.T) {
	const width = 38
	got := Fill(theme.DarkRoles(false).Panel, styledRow(), width)

	if w := Width(got); w != width {
		t.Fatalf("Fill width = %d, want %d", w, width)
	}
	if p := PaintedWidth(got); p != width {
		t.Errorf("background covers %d of %d cells; it dies at column %d.\n%s",
			p, width, p, escape(got))
	}
}

// The bug this whole change exists to kill. Asserting on the OLD spelling
// keeps the reason for [Fill] in the test suite rather than only in a commit
// message: if someone reintroduces the wrap, this test says what it costs.
func TestWrappingAStyledLineLosesTheBackgroundAtTheFirstReset(t *testing.T) {
	const width = 38
	old := lipgloss.NewStyle().Background(theme.DarkRoles(false).Panel).
		Render(Fit(styledRow(), width))

	p := PaintedWidth(old)
	if p >= width {
		t.Fatalf("the old spelling painted %d of %d cells — if lipgloss now "+
			"reopens the background after a reset, Fill can be retired", p, width)
	}
	// The leading space and the bar glyph, and then the bar's span closes
	// and takes the background with it. Everything from the sigil rightwards
	// — title, time, and the padding out to 38 — is drawn on the terminal's
	// default.
	if p != 2 {
		t.Errorf("background survived %d cells, want 2", p)
	}
}

// A span with its own background must keep it: the unread badge is drawn on
// cyan inside a row filled with panel, and the row's fill must not repaint
// over it.
func TestFillDoesNotOverpaintASpansOwnBackground(t *testing.T) {
	r := theme.DarkRoles(false)
	badge := lipgloss.NewStyle().Background(r.Cyan).Foreground(r.Bg).Render(" 3 ")

	got := Fill(r.Panel, "ab"+badge, 10)

	if p := PaintedWidth(got); p != 10 {
		t.Fatalf("background covers %d of 10 cells\n%s", p, escape(got))
	}
	if !strings.Contains(got, "48;5;73") {
		t.Errorf("the badge's own background is gone:\n%s", escape(got))
	}
}

// Nesting is how the frame and the panels divide the work: the frame fills a
// column with its surface, and a row that painted itself keeps what it chose.
func TestANestedFillWins(t *testing.T) {
	r := theme.DarkRoles(false)
	row := Fill(r.Sel, " "+lipgloss.NewStyle().Foreground(r.Cyan).Render("▌")+" x", 12)
	got := Fill(r.Panel, row, 12)

	if p := PaintedWidth(got); p != 12 {
		t.Fatalf("background covers %d of 12 cells\n%s", p, escape(got))
	}
	// Sel is 235 and Panel is 233; every painted run must end up on sel,
	// which it does because the inner fill's sequences come after the
	// outer's within each segment.
	for _, seg := range strings.Split(got, "\x1b[0m") {
		if seg == "" {
			continue
		}
		if i, j := strings.LastIndex(seg, "48;5;233"), strings.LastIndex(seg, "48;5;235"); i > j {
			t.Errorf("panel overpaints sel in segment %q", escape(seg))
		}
	}
}

func TestFillPadsAndClampsToWidth(t *testing.T) {
	r := theme.DarkRoles(false)
	tests := []struct {
		name  string
		in    string
		width int
	}{
		{"pads a short line", "ab", 10},
		{"clamps a long line", strings.Repeat("x", 40), 10},
		{"pads an empty line", "", 10},
		{"handles wide runes", "四字四字四字", 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fill(r.Panel, tt.in, tt.width)
			if w := Width(got); w != tt.width {
				t.Errorf("width = %d, want %d", w, tt.width)
			}
			if p := PaintedWidth(got); p != tt.width {
				t.Errorf("painted = %d, want %d", p, tt.width)
			}
		})
	}
}

func TestFillAtNonPositiveWidthIsEmpty(t *testing.T) {
	for _, w := range []int{0, -1} {
		if got := Fill(theme.DarkRoles(false).Panel, "abc", w); got != "" {
			t.Errorf("Fill(width=%d) = %q, want empty", w, got)
		}
	}
}

// --- PaintedWidth -------------------------------------------------------

func TestPaintedWidthCountsToTheFirstUnpaintedCell(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"no escapes at all", "abcd", 0},
		{"background then text", "\x1b[48;5;233mabcd", 4},
		{"reset ends the run", "\x1b[48;5;233mab\x1b[0mcd", 2},
		{"ESC[m is a reset too", "\x1b[48;5;233mab\x1b[mcd", 2},
		{"49 cancels the background", "\x1b[48;5;233mab\x1b[49mcd", 2},
		{"a foreground alone paints nothing", "\x1b[38;5;110mabcd", 0},
		{"reopened after a reset", "\x1b[48;5;233mab\x1b[0m\x1b[48;5;233mcd", 4},
		{"wide runes count as cells", "\x1b[48;5;233m四字", 4},
		{"basic background codes", "\x1b[41mab", 2},
		{"bright background codes", "\x1b[101mab", 2},
		{"truecolour background", "\x1b[48;2;11;13;16mab", 2},
		// The reason applySGR parses the introducers instead of scanning:
		// "100" here is an argument to 38, not a bright-black background.
		{"a truecolour foreground is not a background", "\x1b[38;2;16;100;7mab", 0},
		{"a 256 foreground is not a background", "\x1b[38;5;46mab", 0},
		{"combined foreground and background", "\x1b[38;5;232;48;5;73mab", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PaintedWidth(tt.in); got != tt.want {
				t.Errorf("PaintedWidth(%s) = %d, want %d", escape(tt.in), got, tt.want)
			}
		})
	}
}

func escape(s string) string { return strings.ReplaceAll(s, "\x1b", "ESC") }
