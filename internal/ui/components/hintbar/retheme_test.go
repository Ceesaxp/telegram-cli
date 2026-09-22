package hintbar

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A new palette reaches the row, and a notice raised before the switch
// is drawn in the new palette too: it is the row's for four seconds, which
// is long enough to watch it stay the wrong colour.
func TestSetRolesRedrawsTheRowInTheNewPalette(t *testing.T) {
	trueColour(t)
	for _, tt := range []struct {
		name  string
		state func(m *Model)
	}{
		{"hints", func(m *Model) {}},
		{"error notice", func(m *Model) { m.SetNotice("⚠ upload failed", "error") }},
		{"progress notice", func(m *Model) { m.SetNotice("uploading…", "info") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := theme.MarkerRoles()
			after, known := theme.SecondMarkerRoles()

			m := New(before)
			m.SetWidth(80)
			m.SetHints([]Hint{{Key: "q", Label: "quit"}})
			m.SetRight("2 buffers")
			tt.state(&m)
			_ = m.View()
			m.SetRoles(after)

			view := m.View()
			found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
			if len(found) == 0 {
				t.Fatalf("the hint bar drew no colour at all:\n%s", ansi.Strip(view))
			}
			for _, c := range found {
				if _, ok := known[c[1]]; !ok {
					t.Errorf("after SetRoles the hint bar drew rgb(%s), "+
						"which is not in the palette it was given", c[1])
				}
			}
		})
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
