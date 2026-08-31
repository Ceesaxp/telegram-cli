package frame

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/layout"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile. Without one lipgloss resolves a test
// binary to Ascii, Render becomes the identity function, and every
// assertion in this file about a background passes because no background
// was ever emitted — see cell.PaintedWidth.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	os.Exit(m.Run())
}

func roles() theme.Roles { return theme.DarkRoles(false) }

// panelLines fakes what a panel hands the frame: short lines, over-long
// lines, and — the case that matters — lines built from spans that close
// themselves with a reset. Every panel in this app produces the third kind.
func panelLines(n int, tag string) []string {
	r := roles()
	out := make([]string, n)
	for i := range out {
		out[i] = " " +
			lipgloss.NewStyle().Foreground(r.Cyan).Render("▌") + " " +
			lipgloss.NewStyle().Foreground(r.Bright).Render(tag) +
			" plain tail " +
			lipgloss.NewStyle().Foreground(r.Faint).Render("12:00")
	}
	return out
}

func screenAt(width, height int, railEnabled bool) (layout.Layout, string) {
	r := roles()
	l := layout.Compute(width, height, 1, railEnabled)
	s := Screen{
		Layout: l,
		Roles:  r,
		TopBar: " " + lipgloss.NewStyle().Foreground(r.Cyan).Render("tg") + " 1:all",
		HintBar: " " + lipgloss.NewStyle().Foreground(r.Cyan).Render("q") + " " +
			lipgloss.NewStyle().Foreground(r.Faint).Render("quit"),
		ChatList: Column{Width: l.ChatListWidth, Surface: r.Panel, Lines: panelLines(3, "list")},
		Thread:   Column{Width: l.ThreadWidth, Surface: r.Bg, Lines: panelLines(3, "thread")},
		Rail:     Column{Width: l.RailWidth, Surface: r.Panel, Lines: panelLines(2, "rail")},
	}
	return l, Render(s)
}

// The whole point of the change. Every cell of every row carries a
// background, so nothing on the screen falls through to whatever the
// terminal's own default happens to be.
//
// It is asserted here rather than per panel because it is a property of the
// frame: panels hand over lines full of self-closing spans, and drew fewer
// rows than the body, and the frame is what makes the result continuous
// regardless.
func TestEveryRowIsPaintedEdgeToEdge(t *testing.T) {
	sizes := []struct {
		name          string
		width, height int
		rail          bool
	}{
		{"narrow, single panel", 80, 24, false},
		{"normal", 100, 30, false},
		{"wide with the rail", 137, 40, true},
		{"very wide", 200, 60, true},
		{"short enough to drop the bars", 100, 10, false},
	}

	for _, sz := range sizes {
		t.Run(sz.name, func(t *testing.T) {
			l, out := screenAt(sz.width, sz.height, sz.rail)
			for y, row := range strings.Split(out, "\n") {
				if w := cell.Width(row); w != l.Width {
					t.Fatalf("row %d is %d cells, want %d", y, w, l.Width)
				}
				if p := cell.PaintedWidth(row); p != l.Width {
					t.Errorf("row %d: background covers %d of %d cells, dying at column %d\n%s",
						y, p, l.Width, p, strings.ReplaceAll(row, "\x1b", "ESC"))
				}
			}
		})
	}
}

// The rows a panel did not draw are the frame's too. A chat list with three
// chats in a forty-row body used to leave a band of terminal default beneath
// it, which reads as the panel stopping early rather than as it being empty.
func TestPaddingRowsArePaintedWithTheColumnSurface(t *testing.T) {
	r := roles()
	l, out := screenAt(120, 40, false)
	rows := strings.Split(out, "\n")

	// Row 0 is the top bar and the panels only drew three lines, so this is
	// well past anything a panel rendered.
	last := rows[len(rows)-2]
	if p := cell.PaintedWidth(last); p != l.Width {
		t.Fatalf("a padding row is painted for %d of %d cells", p, l.Width)
	}
	if !strings.Contains(last, "48;5;"+string(r.Panel)) {
		t.Errorf("the chat list's padding is not on panel:\n%s",
			strings.ReplaceAll(last, "\x1b", "ESC"))
	}
	if !strings.Contains(last, "48;5;"+string(r.Bg)) {
		t.Errorf("the thread's padding is not on bg:\n%s",
			strings.ReplaceAll(last, "\x1b", "ESC"))
	}
}

// The surfaces are different colours and have to stay that way: the list
// and the rail are panel, the thread between them is bg. A single fill for
// the whole body would be simpler and would erase the column structure the
// borderless design has instead of borders.
func TestColumnsKeepTheirOwnSurfaces(t *testing.T) {
	r := roles()
	_, out := screenAt(137, 40, true)
	row := strings.Split(out, "\n")[2]

	for _, want := range []struct {
		name   string
		colour lipgloss.Color
	}{
		{"panel, for the list and the rail", r.Panel},
		{"bg, for the thread", r.Bg},
	} {
		if !strings.Contains(row, "48;5;"+string(want.colour)) {
			t.Errorf("no %s in the body row:\n%s", want.name,
				strings.ReplaceAll(row, "\x1b", "ESC"))
		}
	}
}

// A panel that paints an exception — a selected chat row, the composer's
// panel strip inside the thread column — must keep it. cell.Fill reopens the
// surface before each span rather than after, which is what lets the inner
// choice win.
func TestAPanelsOwnFillSurvivesTheColumnFill(t *testing.T) {
	r := roles()
	l := layout.Compute(100, 30, 1, false)
	selected := cell.Fill(r.Sel,
		" "+lipgloss.NewStyle().Foreground(r.Cyan).Render("▌")+" Ksenia",
		l.ChatListWidth)

	out := Render(Screen{
		Layout:   l,
		Roles:    r,
		ChatList: Column{Width: l.ChatListWidth, Surface: r.Panel, Lines: []string{selected}},
		Thread:   Column{Width: l.ThreadWidth, Surface: r.Bg},
	})

	// Cut at the column rule, whose own sequence would otherwise arrive as a
	// segment of the chat list's.
	rule := lipgloss.NewStyle().Foreground(r.Rule).Background(r.Bg).Render("│")
	row := strings.Split(out, "\n")[1]
	prefix := row[:strings.Index(row, rule)]
	for _, seg := range strings.Split(prefix, "\x1b[0m") {
		if seg == "" {
			continue
		}
		panel := strings.LastIndex(seg, "48;5;"+string(r.Panel))
		sel := strings.LastIndex(seg, "48;5;"+string(r.Sel))
		if sel < 0 {
			t.Fatalf("the selected row lost its sel background:\n%s",
				strings.ReplaceAll(prefix, "\x1b", "ESC"))
		}
		if panel > sel {
			t.Errorf("the column fill overpaints sel in %q",
				strings.ReplaceAll(seg, "\x1b", "ESC"))
		}
	}
}

// An unset Surface is "do not paint", not a black rectangle. Every caller
// sets one, but a zero value that silently painted would be worse than one
// that visibly does nothing.
func TestAnUnsetSurfacePaintsNothing(t *testing.T) {
	l := layout.Compute(100, 30, 1, false)
	out := Render(Screen{
		Layout:   l,
		Roles:    roles(),
		ChatList: Column{Width: l.ChatListWidth, Lines: []string{"plain"}},
		Thread:   Column{Width: l.ThreadWidth},
	})

	row := strings.Split(out, "\n")[1]
	if cell.Width(row) != l.Width {
		t.Fatalf("row is %d cells, want %d", cell.Width(row), l.Width)
	}
	if p := cell.PaintedWidth(row); p != 0 {
		t.Errorf("an unset surface painted %d cells", p)
	}
}

func TestLinesDropsTheTrailingNewline(t *testing.T) {
	if got := Lines("a\nb\n"); len(got) != 2 {
		t.Errorf("Lines(%q) = %d lines, want 2", "a\nb\n", len(got))
	}
	if got := Lines(""); got != nil {
		t.Errorf("Lines(\"\") = %v, want nil", got)
	}
}
