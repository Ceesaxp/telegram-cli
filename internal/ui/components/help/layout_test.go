package help

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// longSections is a keymap too tall for any card: enough rows that the
// scrolling and column behaviour below are exercised rather than assumed.
func longSections(n int) []Section {
	var out []Section
	for i := range 4 {
		var b []Binding
		for j := range n {
			b = append(b, Binding{
				Keys: "ctrl+" + string(rune('a'+j%26)),
				Desc: "does a thing number " + string(rune('a'+j%26)),
			})
		}
		out = append(out, Section{Title: "Section " + string(rune('A'+i)), Bindings: b})
	}
	return out
}

// hidden matches the scroll indicator: an arrow followed by a count. The
// footer's own "↑↓/jk: scroll" hint carries bare arrows, so a bare-glyph
// match would find the hint and pass whatever the indicator did.
var (
	hidden   = regexp.MustCompile(`[↑↓]\d+`)
	bothEnds = regexp.MustCompile(`↑\d+ ↓\d+`)
)

func card(t *testing.T, w, h int, sections []Section) Model {
	t.Helper()
	m := New(theme.DarkRoles(false))
	m.SetSections(sections)
	m.SetSize(w, h)
	m.SetVisible(true)
	return m
}

// A card that scrolls has to say it is scrolled.
//
// The footer already advertised the keys; what it did not say was that there
// was anything to use them on — so a binding below the fold was
// indistinguishable from a binding that does not exist. That is exactly how
// "} / {" came to be reported as missing from a card that listed it.
func TestAScrolledCardSaysWhatIsHidden(t *testing.T) {
	m := card(t, 100, 24, longSections(12))

	// A COUNT after the arrow, not a bare arrow: the footer's own
	// "↑↓/jk: scroll" hint contains both glyphs already.
	view := ansi.Strip(m.View())
	if !hidden.MatchString(view) {
		t.Errorf("a card with more below does not say how much:\n%s", lastLine(view))
	}

	// Scrolled to the middle it reports both directions.
	for range 6 {
		m, _ = m.Update(pressDown())
	}
	view = ansi.Strip(m.View())
	if !bothEnds.MatchString(view) {
		t.Errorf("a card scrolled into the middle does not report both ends:\n%s",
			lastLine(view))
	}
}

// And a card that fits says nothing, rather than "↓0 more".
func TestACardThatFitsSaysNothing(t *testing.T) {
	m := card(t, 100, 40, []Section{{
		Title:    "Small",
		Bindings: []Binding{{Keys: "q", Desc: "quit"}},
	}})

	if view := ansi.Strip(m.View()); hidden.MatchString(view) {
		t.Errorf("a card that fits claims to be scrolled:\n%s", lastLine(view))
	}
}

// Two columns halve the scrolling on a terminal wide enough for them. This
// keymap is long, and on a single column most of it sits below the fold.
func TestAWideCardUsesTwoColumns(t *testing.T) {
	sections := longSections(6)

	narrow := ansi.Strip(card(t, 100, 30, sections).View())
	wide := ansi.Strip(card(t, 160, 30, sections).View())

	narrowRow := widestRow(narrow)
	wideRow := widestRow(wide)
	if wideRow <= narrowRow {
		t.Fatalf("the wide card is %d cells against the narrow card's %d — "+
			"it is not using the extra width", wideRow, narrowRow)
	}

	// The proof it is two COLUMNS and not one stretched row: a binding from
	// the back half of the keymap appears on the same line as one from the
	// front half.
	for _, line := range strings.Split(wide, "\n") {
		if strings.Count(line, "does a thing") >= 2 {
			return
		}
	}
	t.Errorf("no row carries two bindings, so the card is one wide column:\n%s", wide)
}

// A narrow card must not try: two columns of 20 cells is worse than one of 40.
func TestANarrowCardStaysSingleColumn(t *testing.T) {
	wide := ansi.Strip(card(t, 90, 30, longSections(6)).View())
	for _, line := range strings.Split(wide, "\n") {
		if strings.Count(line, "does a thing") >= 2 {
			t.Errorf("a 90-column card split into two columns:\n%s", line)
			return
		}
	}
}

func widestRow(view string) int {
	widest := 0
	for _, line := range strings.Split(view, "\n") {
		if w := cell.Width(line); w > widest {
			widest = w
		}
	}
	return widest
}

func lastLine(view string) string {
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) < 2 {
		return view
	}
	return strings.Join(lines[len(lines)-3:], "\n")
}

// Every row of the card is painted with the PANEL surface, edge to edge.
//
// Divergence 19 arriving a second time: the overlays were still drawing from
// the pre-2.0 theme when the panels were fixed, and were migrated to the
// palette afterwards without the fix that went with it. Each row was
// assembled out of styled spans and handed to a background style, where the
// first span's own ESC[0m takes the surface with it — this card was painting
// 23 of 112 cells on a binding row.
//
// Asserted on the PANEL specifically rather than on "something": the app
// fills the screen behind an overlay with bg, which satisfies a paint check
// while leaving the card's interior the wrong colour.
func TestEveryCardRowIsPaintedWithPanel(t *testing.T) {
	r := theme.DarkRoles(false)
	m := card(t, 120, 40, longSections(6))
	panelBg := "48;5;" + string(r.Panel)

	for i, line := range strings.Split(m.View(), "\n") {
		if p := cell.PaintedWidth(line); p != cell.Width(line) {
			t.Errorf("row %d: painted %d of %d cells", i, p, cell.Width(line))
		}
		if !strings.Contains(line, panelBg) {
			t.Errorf("row %d is not on the panel surface: %s", i,
				strings.ReplaceAll(line, "\x1b", "ESC"))
		}
	}
}
