package forward

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A picker open when the palette changes is redrawn in the new one, at
// either of its two steps.
func TestSetRolesRedrawsThePickerInTheNewPalette(t *testing.T) {
	trueColour(t)
	for _, tt := range []struct {
		name  string
		state func(m *Model)
	}{
		{"pick", func(m *Model) {}},
		{"confirm", func(m *Model) {
			if *m, _ = press(*m, "enter"); m.Step() != StepConfirm {
				t.Fatal("precondition: enter did not reach the confirmation")
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := theme.MarkerRoles()
			after, known := theme.SecondMarkerRoles()

			m := New(before)
			m.Open(Source{ChatID: 1, MessageID: 2, Preview: "rebased, CI green"},
				[]Chat{{ID: 3, Title: "Nadia Feld", Sigil: "@", Handle: "nadia"}})
			tt.state(&m)
			_ = m.View()
			m.SetRoles(after)

			view := m.View()
			found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
			if len(found) == 0 {
				t.Fatalf("the picker drew no colour at all:\n%s", ansi.Strip(view))
			}
			for _, c := range found {
				if _, ok := known[c[1]]; !ok {
					t.Errorf("after SetRoles the picker drew rgb(%s), "+
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
