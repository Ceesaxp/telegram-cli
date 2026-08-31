package help

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

func sampleSections() []Section {
	return []Section{
		{
			Title: "Navigation",
			Bindings: []Binding{
				{Keys: "Alt+1/2/3", Desc: "Focus chat list / view / composer"},
				{Keys: "↑↓ / jk", Desc: "Move selection"},
				{Keys: "←→ / 1-9", Desc: "Cycle / jump folders (chat list focused)"},
			},
		},
		{
			Title: "Search",
			Bindings: []Binding{
				{Keys: "/", Desc: "Open search"},
				{Keys: "Esc", Desc: "Close overlay"},
			},
		},
		{
			Title: "Contacts",
			Bindings: []Binding{
				{Keys: "Alt+C", Desc: "Toggle contacts"},
			},
		},
	}
}

func TestGeometryCapsAndFloors(t *testing.T) {
	// Very large window: the WIDTH caps, because a keymap read across 200
	// columns is a keymap nobody reads. The height does not — a cap there
	// hides bindings behind a scroll while leaving the screen half empty,
	// which is how "} / {" became a binding this card knew about and never
	// showed.
	g := computeGeometry(400, 200)
	if g.boxWidth != maxTwoColWidth {
		t.Errorf("boxWidth = %d, want the two-column cap %d at a huge window",
			g.boxWidth, maxTwoColWidth)
	}

	// A window too narrow for a second column caps at the single-column
	// width instead — the wider cap exists to hold two columns, not to let
	// one stretch.
	if g := computeGeometry(100, 40); g.boxWidth > maxBoxWidth {
		t.Errorf("boxWidth = %d at 100 columns, want at most %d",
			g.boxWidth, maxBoxWidth)
	}
	if g.boxHeight != 200-6 {
		t.Errorf("boxHeight = %d, want the window height less its margin (%d)",
			g.boxHeight, 200-6)
	}

	// A window that CAN afford the structural floor (exactly it, in this
	// case): the box floors at the structural minimum rather than
	// shrinking to something DialogBox's fixed chrome can't render.
	g = computeGeometry(structuralMinWidth, structuralMinHeight)
	if g.boxWidth < structuralMinWidth {
		t.Errorf("boxWidth = %d, want >= structural floor %d when the window affords it", g.boxWidth, structuralMinWidth)
	}
	if g.boxHeight < structuralMinHeight {
		t.Errorf("boxHeight = %d, want >= structural floor %d when the window affords it", g.boxHeight, structuralMinHeight)
	}
	if g.innerWidth < 1 || g.contentH < 1 {
		t.Errorf("innerWidth=%d contentH=%d, want both >= 1", g.innerWidth, g.contentH)
	}

	// A truly pathological window, narrower than the structural floor
	// itself: the window wins instead (see computeGeometry's final
	// re-clamp) — the box must never exceed the window, even though that
	// means it ends up smaller than the structural floor.
	g = computeGeometry(5, 5)
	if g.boxWidth != 5 {
		t.Errorf("boxWidth = %d, want exactly 5 (the window) below the structural floor", g.boxWidth)
	}
	if g.boxHeight != 5 {
		t.Errorf("boxHeight = %d, want exactly 5 (the window) below the structural floor", g.boxHeight)
	}
	if g.innerWidth < 1 || g.contentH < 1 {
		t.Errorf("innerWidth=%d contentH=%d, want both >= 1 even at a pathological window", g.innerWidth, g.contentH)
	}
}

// TestGeometryNeverExceedsWindow covers both realistic sizes and truly
// pathological ones (down to 0x0): the structural floor is a preference
// that applies only when the window can afford it, never a guarantee
// that overrides the window itself — see computeGeometry's final
// re-clamp for why.
func TestGeometryNeverExceedsWindow(t *testing.T) {
	dims := [][2]int{
		{100, 40}, {60, 20}, {30, 15}, // realistic sizes
		{8, 8}, {5, 5}, {1, 1}, {0, 0}, // pathological sizes
	}
	for _, d := range dims {
		w, h := d[0], d[1]
		g := computeGeometry(w, h)
		if g.boxWidth > w {
			t.Errorf("computeGeometry(%d,%d): boxWidth %d > window width %d", w, h, g.boxWidth, w)
		}
		if g.boxHeight > h {
			t.Errorf("computeGeometry(%d,%d): boxHeight %d > window height %d", w, h, g.boxHeight, h)
		}
		if g.boxWidth < 0 || g.boxHeight < 0 {
			t.Errorf("computeGeometry(%d,%d): negative box dimensions %dx%d", w, h, g.boxWidth, g.boxHeight)
		}
	}
}

func newVisibleModel(w, h int) Model {
	m := New(theme.DarkRoles(false))
	m.SetSections(sampleSections())
	m.SetSize(w, h)
	m.SetVisible(true)
	return m
}

// TestViewEveryLineWithinBoxWidth is the direct cell-accuracy regression
// test: every rendered line of the overlay — through the real shipped
// theme's DialogBox (which carries border + padding) — must be at most
// the box's own display-cell width, never more, at every window size
// including narrow ones.
func TestViewEveryLineWithinBoxWidth(t *testing.T) {
	for _, dims := range [][2]int{{120, 40}, {80, 24}, {50, 16}, {30, 12}, {20, 8}} {
		w, h := dims[0], dims[1]
		m := newVisibleModel(w, h)

		out := m.View()
		g := computeGeometry(w, h)
		for lineNo, line := range strings.Split(out, "\n") {
			if lw := ansi.StringWidth(line); lw > g.boxWidth {
				t.Errorf("window=%dx%d: line %d has display width %d > box width %d: %q",
					w, h, lineNo, lw, g.boxWidth, line)
			}
		}
	}
}

// TestViewSectionsRenderedInOrder checks section titles and their
// bindings' descriptions appear in the rendered output in the order
// SetSections was given.
func TestViewSectionsRenderedInOrder(t *testing.T) {
	m := newVisibleModel(120, 40) // large enough that nothing scrolls
	out := m.View()

	wantInOrder := []string{"Navigation", "Search", "Contacts"}
	lastIdx := -1
	for _, want := range wantInOrder {
		idx := strings.Index(out, want)
		if idx < 0 {
			t.Fatalf("rendered output missing section title %q:\n%s", want, out)
		}
		if idx < lastIdx {
			t.Fatalf("section %q rendered out of order (index %d before previous index %d)", want, idx, lastIdx)
		}
		lastIdx = idx
	}
}

// TestViewHiddenIsEmpty checks the overlay renders nothing when not
// visible.
func TestViewHiddenIsEmpty(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetSections(sampleSections())
	m.SetSize(80, 24)
	if got := m.View(); got != "" {
		t.Fatalf("View() while not visible = %q, want empty", got)
	}
}

// TestSetVisibleAndSetSectionsResetScroll checks scroll position resets
// to the top both when the overlay is (re-)shown and when its content
// changes.
func TestSetVisibleAndSetSectionsResetScroll(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetSections(sampleSections())
	m.SetSize(30, 10) // narrow enough that content overflows and scrolls
	m.SetVisible(true)

	m, _ = m.Update(pressDown())
	m, _ = m.Update(pressDown())
	if m.offset == 0 {
		t.Fatal("expected scrolling down to move the offset off zero")
	}

	m.SetVisible(false)
	m.SetVisible(true)
	if m.offset != 0 {
		t.Fatalf("SetVisible(true) should reset scroll to top, got offset=%d", m.offset)
	}

	m, _ = m.Update(pressDown())
	if m.offset == 0 {
		t.Fatal("expected scrolling down to move the offset off zero again")
	}
	m.SetSections(sampleSections())
	if m.offset != 0 {
		t.Fatalf("SetSections should reset scroll to top, got offset=%d", m.offset)
	}
}

// TestScrollClamping is the required scroll-clamping test: Update must
// never move the offset negative, and never past the point where the
// last content line would leave the visible window (which would leave
// blank trailing rows while more content remains above).
func TestScrollClamping(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetSections(sampleSections())
	m.SetSize(30, 10) // narrow/short enough that content overflows
	m.SetVisible(true)

	g := computeGeometry(30, 10)
	total := len(m.bodyLines(g.innerWidth))
	if total <= g.contentH {
		t.Fatalf("test setup: content (%d lines) does not overflow the box (%d lines) — scrolling can't be exercised", total, g.contentH)
	}

	// Scrolling up from the top must clamp at 0, not go negative.
	m, _ = m.Update(pressUp())
	if m.offset != 0 {
		t.Fatalf("scrolling up from the top: offset = %d, want 0", m.offset)
	}

	// Scrolling down far past the end must clamp, not run away.
	for i := 0; i < total+50; i++ {
		m, _ = m.Update(pressDown())
	}
	wantMax := total - g.contentH
	if m.offset != wantMax {
		t.Fatalf("scrolling far past the end: offset = %d, want clamped to %d", m.offset, wantMax)
	}

	// "G" jumps to the end and clamps the same way.
	m.offset = 0
	m, _ = m.Update(pressEnd())
	if m.offset != wantMax {
		t.Fatalf("after 'G': offset = %d, want %d", m.offset, wantMax)
	}

	// "g" jumps back to the top.
	m, _ = m.Update(pressHome())
	if m.offset != 0 {
		t.Fatalf("after 'g': offset = %d, want 0", m.offset)
	}
}

// TestUpdateNoopWhenNotVisible checks Update never mutates scroll state
// while the overlay is hidden.
func TestUpdateNoopWhenNotVisible(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetSections(sampleSections())
	m.SetSize(30, 10)
	// deliberately not visible

	m, _ = m.Update(pressDown())
	if m.offset != 0 {
		t.Fatalf("Update should be a no-op while hidden, got offset=%d", m.offset)
	}
}

func TestKeysColumnWidthCapsForLongKeys(t *testing.T) {
	sections := []Section{{
		Title: "T",
		Bindings: []Binding{
			{Keys: strings.Repeat("x", 200), Desc: "d"},
		},
	}}
	w := keysColumnWidth(sections, 40)
	if w >= 40 {
		t.Fatalf("keysColumnWidth = %d, want capped well under innerWidth 40", w)
	}
}

// Cell-accuracy of the truncate/pad primitives is covered in
// internal/ui/cell, where help.truncatePlain and help.padPlain were folded
// into cell.Truncate and cell.Pad. The emoji case from this file's original
// test moved with them.

// Helpers to build real tea.KeyPressMsg values, mirroring the pattern
// used by chatlist/chatview's own test helpers (different packages, not
// reused directly to avoid a test-only cross-package dependency).

func pressDown() tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}) }
func pressUp() tea.KeyPressMsg   { return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}) }
func pressEnd() tea.KeyPressMsg  { return tea.KeyPressMsg(tea.Key{Code: 'G', Text: "G"}) }
func pressHome() tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: 'g', Text: "g"}) }

var _ = lipgloss.Width // keep lipgloss imported for potential future assertions

// TestSetFooterOverridesDefault checks SetFooter's text appears in the
// rendered output in place of the hardcoded default, and that "" falls
// back to the default rather than rendering blank.
func TestSetFooterOverridesDefault(t *testing.T) {
	m := newVisibleModel(120, 40)
	out := m.View()
	if !strings.Contains(out, defaultFooter) {
		t.Fatalf("expected the default footer %q before SetFooter is called:\n%s", defaultFooter, out)
	}

	custom := "j/k: scroll | q: close"
	m.SetFooter(custom)
	out = m.View()
	if !strings.Contains(out, custom) {
		t.Fatalf("expected the custom footer %q after SetFooter:\n%s", custom, out)
	}
	if strings.Contains(out, defaultFooter) {
		t.Fatalf("default footer %q should not still be present after SetFooter overrides it:\n%s", defaultFooter, out)
	}

	m.SetFooter("")
	out = m.View()
	if !strings.Contains(out, defaultFooter) {
		t.Fatalf("SetFooter(\"\") should restore the default footer %q:\n%s", defaultFooter, out)
	}
}
