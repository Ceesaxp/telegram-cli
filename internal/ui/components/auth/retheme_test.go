package auth

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A new palette reaches everything the sign-in screen draws — the input
// field included, whose styles are built ahead of drawing and held by the
// widget, so a setter that only swapped the palette would leave the field in
// the old one.
func TestSetRolesRedrawsTheSignInScreenInTheNewPalette(t *testing.T) {
	trueColour(t)
	before, _ := theme.MarkerRoles()
	after, known := theme.SecondMarkerRoles()

	m := New(before, nil)
	m.SetSize(80, 24)
	_ = m.View()
	m.SetRoles(after)

	view := m.View()
	found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
	if len(found) == 0 {
		t.Fatalf("the sign-in screen drew no colour at all:\n%s", ansi.Strip(view))
	}
	for _, c := range found {
		if _, ok := known[c[1]]; !ok {
			t.Errorf("after SetRoles the sign-in screen drew rgb(%s), "+
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
