package help

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A card open when the palette changes is redrawn in the new one.
func TestSetRolesRedrawsTheCardInTheNewPalette(t *testing.T) {
	trueColour(t)
	before, _ := theme.MarkerRoles()
	after, known := theme.SecondMarkerRoles()

	m := New(before)
	m.SetSize(100, 30)
	m.SetSections([]Section{
		{Title: "Global", Bindings: []Binding{{Keys: "ctrl+q", Desc: "Quit"}}},
		{Title: "Chat view", Bindings: []Binding{{Keys: "j / k", Desc: "Move"}}},
	})
	m.SetVisible(true)
	_ = m.View()
	m.SetRoles(after)

	view := m.View()
	found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
	if len(found) == 0 {
		t.Fatalf("the help card drew no colour at all:\n%s", ansi.Strip(view))
	}
	for _, c := range found {
		if _, ok := known[c[1]]; !ok {
			t.Errorf("after SetRoles the help card drew rgb(%s), "+
				"which is not in the palette it was given", c[1])
		}
	}
}

// trueColour pins a true-colour profile for one test, so a marker palette
// reads back exactly rather than quantised to the 256 the package's other
// tests are pinned to.
func trueColour(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}
