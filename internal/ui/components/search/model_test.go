package search

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

func TestComputeGeometryCapsAtMax(t *testing.T) {
	g := computeGeometry(300, 100)
	if g.boxWidth != maxBoxWidth {
		t.Errorf("boxWidth = %d, want %d (max cap)", g.boxWidth, maxBoxWidth)
	}
	if g.boxHeight != maxBoxHeight {
		t.Errorf("boxHeight = %d, want %d (max cap)", g.boxHeight, maxBoxHeight)
	}
}

func TestComputeGeometryFloorsWhenWindowAffordsIt(t *testing.T) {
	// Window is below the floor in raw terms (w-8 < minBoxWidth,
	// h-6 < minBoxHeight) but still large enough to actually contain the
	// floored box, so the floor should apply as normal.
	g := computeGeometry(45, 16)
	if g.boxWidth != minBoxWidth {
		t.Errorf("boxWidth = %d, want %d (min floor)", g.boxWidth, minBoxWidth)
	}
	if g.boxHeight != minBoxHeight {
		t.Errorf("boxHeight = %d, want %d (min floor)", g.boxHeight, minBoxHeight)
	}
}

// TestComputeGeometryClampsFloorToWindow is the regression test for the
// overflow bug: lipgloss.Place (the caller) does not clip, so if the box
// floor (40x12) were applied unconditionally on a window smaller than that,
// the box would overflow/smear past the terminal edge instead of degrading
// to fit. Below the nice-to-have floor, the box degrades to the window's
// own size — down to structuralMinWidth/structuralMinHeight, the hard floor
// below which DialogBox's own fixed border+padding chrome (and the fixed
// title/input/tabs/hint rows) can no longer be rendered without the box
// silently overflowing *itself*, which is worse than exceeding a
// pathologically small window.
func TestComputeGeometryClampsFloorToWindow(t *testing.T) {
	tests := []struct {
		name          string
		w, h          int
		wantBoxWidth  int
		wantBoxHeight int
		// exceedsWindow marks cases below the structural floor, where the
		// box is expected to exceed the window — there is no smaller
		// renderable size.
		exceedsWindow bool
	}{
		{name: "width just under floor", w: 39, h: 40, wantBoxWidth: 39, wantBoxHeight: maxBoxHeight},
		{name: "height just under floor", w: 120, h: 11, wantBoxWidth: maxBoxWidth, wantBoxHeight: 11},
		{name: "both under floor", w: 39, h: 11, wantBoxWidth: 39, wantBoxHeight: 11},
		{name: "pathologically tiny", w: 5, h: 5, wantBoxWidth: structuralMinWidth, wantBoxHeight: structuralMinHeight, exceedsWindow: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := computeGeometry(tc.w, tc.h)
			if g.boxWidth != tc.wantBoxWidth {
				t.Errorf("boxWidth = %d, want %d", g.boxWidth, tc.wantBoxWidth)
			}
			if g.boxHeight != tc.wantBoxHeight {
				t.Errorf("boxHeight = %d, want %d", g.boxHeight, tc.wantBoxHeight)
			}
			if !tc.exceedsWindow {
				// The box must never exceed a window that affords at
				// least the structural minimum.
				if g.boxWidth > tc.w {
					t.Errorf("boxWidth = %d exceeds window width %d", g.boxWidth, tc.w)
				}
				if g.boxHeight > tc.h {
					t.Errorf("boxHeight = %d exceeds window height %d", g.boxHeight, tc.h)
				}
			}
			// Derived dims must never go non-positive, however small the
			// window.
			if g.innerWidth < 1 || g.inputWidth < 1 || g.listHeight < 1 {
				t.Errorf("derived dims not floored: innerWidth=%d inputWidth=%d listHeight=%d",
					g.innerWidth, g.inputWidth, g.listHeight)
			}
		})
	}
}

func TestComputeGeometryMidSize(t *testing.T) {
	// A typical window: box should sit strictly inside it, never stretch
	// to fill it (the bug: DialogBox.Width(m.width-4) on the full window).
	g := computeGeometry(120, 40)
	if g.boxWidth >= 120 {
		t.Errorf("boxWidth = %d, want < window width 120", g.boxWidth)
	}
	if g.boxHeight >= 40 {
		t.Errorf("boxHeight = %d, want < window height 40", g.boxHeight)
	}
	// The input's own bordered rendering width must match the other rows'
	// content width exactly, or the outer box's wrap forces a hard-wrap
	// across border characters (the reported artifact).
	if got, want := g.inputWidth+inputChromeW, g.innerWidth; got != want {
		t.Errorf("bordered input width = %d, want %d (innerWidth)", got, want)
	}
}

func newTestModel(w, h int) Model {
	m := New(store.NewStore(), nil, theme.ForName("dark"))
	m.SetSize(w, h)
	m.SetVisible(true)
	return m
}

func TestViewHintPresent(t *testing.T) {
	m := newTestModel(120, 40)
	view := m.View()
	if !strings.Contains(view, "Esc: close") {
		t.Errorf("View() missing Esc affordance hint, got:\n%s", view)
	}
	if !strings.Contains(view, "Enter: search") {
		t.Errorf("View() missing Enter affordance hint, got:\n%s", view)
	}
}

func TestViewEmptyStateMessages(t *testing.T) {
	m := newTestModel(120, 40)

	// Before any search: "type to search".
	if view := m.View(); !strings.Contains(view, "type to search") {
		t.Errorf("View() before search should hint to type, got:\n%s", view)
	}

	// After a search that comes back empty: "no results for ...".
	m.query = "nothing-matches-this"
	nm, _ := m.Update(searchResultsMsg{tab: TabChats, items: nil})
	m = nm
	view := m.View()
	if !strings.Contains(view, "no results") {
		t.Errorf("View() after empty search should say no results, got:\n%s", view)
	}
	if !strings.Contains(view, "nothing-matches-this") {
		t.Errorf("View() after empty search should echo the query, got:\n%s", view)
	}
}

func TestEscClosesRegardlessOfFocus(t *testing.T) {
	for _, listFocused := range []bool{false, true} {
		m := newTestModel(120, 40)
		m.list.Focused = listFocused
		m.input.Focused = !listFocused

		nm, _ := m.Update(escKeyMsg())
		if nm.IsVisible() {
			t.Errorf("Esc with list.Focused=%v did not close the overlay", listFocused)
		}
	}
}

// TestViewRenderBounds is a smoke test on the overlay's rendered box: its
// line count must equal the computed box height, and no line may exceed the
// computed box width. Both were violated by the original bug — the box
// stretched to the full window, and the input row (with its own border)
// overflowed the box's actual wrap width, causing lipgloss to hard-wrap
// border characters mid-line.
func TestViewRenderBounds(t *testing.T) {
	// All sizes here have enough width for the tab bar to fit on its own
	// row (innerWidth >= ~33); widgets.Tabs wraps below that (a known,
	// accepted degradation at pathological widths — see
	// TestComputeGeometryClampsFloorToWindow's "pathologically tiny" case,
	// which is covered at the geometry level instead, not here, since the
	// resulting tab-wrap row-count blowup is orthogonal to what this test
	// checks).
	sizes := [][2]int{
		{120, 40},
		{300, 100},
		{40, 15},
		// Below-floor windows that still afford the structural minimum:
		// the box must degrade to fit exactly inside the window rather
		// than overflow it (lipgloss.Place, the caller, does not clip).
		{39, 40},
		{120, 11},
		{39, 11},
	}

	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		m := newTestModel(w, h)

		// The box itself must never be larger than the window it's
		// placed in.
		if m.geo.boxWidth > w {
			t.Errorf("size (%d,%d): boxWidth = %d exceeds window width", w, h, m.geo.boxWidth)
		}
		if m.geo.boxHeight > h {
			t.Errorf("size (%d,%d): boxHeight = %d exceeds window height", w, h, m.geo.boxHeight)
		}

		view := m.View()
		lines := strings.Split(view, "\n")

		// The render must actually be the computed box size — never
		// wider/taller than its own boxWidth/boxHeight, and never a
		// hard-wrapped, broken-border mess (the original bug).
		if len(lines) != m.geo.boxHeight {
			t.Errorf("size (%d,%d): View() has %d lines, want boxHeight=%d",
				w, h, len(lines), m.geo.boxHeight)
		}
		for i, line := range lines {
			if lw := ansi.StringWidth(line); lw > m.geo.boxWidth {
				t.Errorf("size (%d,%d): line %d width = %d, want <= boxWidth=%d\nline: %q",
					w, h, i, lw, m.geo.boxWidth, line)
			}
			// The rendered box must fit the window exactly at these
			// sizes, not just its own boxWidth accounting.
			if lw := ansi.StringWidth(line); lw > w {
				t.Errorf("size (%d,%d): line %d width = %d exceeds window width\nline: %q",
					w, h, i, lw, line)
			}
		}
	}
}

func escKeyMsg() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEscape}
}
