package search

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// A new palette reaches the whole overlay. Most of what it draws is styled by
// the widgets it holds — the input, the tabs, the result rows — with styles
// New built, so a setter that only swapped the palette would recolour the
// frame and the hint and leave everything inside them as it was.
func TestSetRolesRedrawsTheSearchInTheNewPalette(t *testing.T) {
	trueColour(t)
	for _, tt := range []struct {
		name  string
		state func(m Model) Model
	}{
		{"typing", func(m Model) Model {
			m.SetQuery("deploy")
			return m
		}},
		{"results", func(m Model) Model {
			m.query = "deploy"
			m, _ = m.Update(searchResultsMsg{tab: TabChats, items: []widgets.ListItem{
				{ID: "1", Title: "infra-oncall", Subtitle: "supergroup"},
				{ID: "2", Title: "deploys", Subtitle: "channel"},
			}})
			return m
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := theme.MarkerRoles()
			after, known := theme.SecondMarkerRoles()

			m := New(store.NewStore(), nil, before)
			m.SetSize(120, 40)
			m.SetVisible(true)
			m = tt.state(m)
			_ = m.View()
			m.SetRoles(after)

			view := m.View()
			found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
			if len(found) == 0 {
				t.Fatalf("the search drew no colour at all:\n%s", ansi.Strip(view))
			}
			for _, c := range found {
				if _, ok := known[c[1]]; !ok {
					t.Errorf("after SetRoles the search drew rgb(%s), "+
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
