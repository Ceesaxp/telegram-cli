package dialog

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A dialog open when the palette changes is redrawn in the new one. It is
// modal, so it is exactly the thing on screen while a switch happens behind
// it.
func TestSetRolesRedrawsTheDialogInTheNewPalette(t *testing.T) {
	trueColour(t)
	before, _ := theme.MarkerRoles()
	after, known := theme.SecondMarkerRoles()

	m := NewConfirm(before, "quit", "Quit", "Discard the draft and quit?")
	_ = m.View()
	m.SetRoles(after)

	view := m.View()
	found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
	if len(found) == 0 {
		t.Fatalf("the dialog drew no colour at all:\n%s", ansi.Strip(view))
	}
	for _, c := range found {
		if _, ok := known[c[1]]; !ok {
			t.Errorf("after SetRoles the dialog drew rgb(%s), "+
				"which is not in the palette it was given", c[1])
		}
	}
}

// trueColour pins a colour profile for one test, so a marker palette reads
// back exactly: under `go test` lipgloss resolves to Ascii and draws no
// colour at all.
func trueColour(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}
