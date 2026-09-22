package topbar

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A new palette reaches the whole row: the mark, the folder tabs, the dot
// and the clock.
func TestSetRolesRedrawsTheRowInTheNewPalette(t *testing.T) {
	trueColour(t)
	before, _ := theme.MarkerRoles()
	after, known := theme.SecondMarkerRoles()

	m := New(before)
	m.SetWidth(80)
	m.SetClock("12:40")
	m.SetConnection(Connected, "connected")
	m.SetDevices(2)
	m.SetFolders([]Folder{{Name: "all", Active: true}, {Name: "work"}})
	_ = m.View()
	m.SetRoles(after)

	view := m.View()
	found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
	if len(found) == 0 {
		t.Fatalf("the top bar drew no colour at all:\n%s", ansi.Strip(view))
	}
	for _, c := range found {
		if _, ok := known[c[1]]; !ok {
			t.Errorf("after SetRoles the top bar drew rgb(%s), "+
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
