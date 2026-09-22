package contacts

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// A new palette reaches the rows and the empty-list placeholder, not only
// the filter row. The placeholder's style is built by New, and the rows are
// drawn by a method value bound to a copy of the model as New left it —
// palette included — so a setter that only swapped the palette would
// reach the header and stop there.
func TestSetRolesRedrawsTheContactsInTheNewPalette(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state func(m *Model)
	}{
		{"rows", func(m *Model) { m.SetContactsForTest(people()) }},
		{"empty", func(m *Model) {}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := theme.MarkerRoles()
			after, known := theme.SecondMarkerRoles()

			m := New(store.NewStore(), nil, before)
			m.SetSize(38, 12)
			m.SetVisible(true)
			m.SetFocused(true)
			tt.state(&m)
			_ = m.View()
			m.SetRoles(after)

			view := m.View()
			found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
			if len(found) == 0 {
				t.Fatalf("the contacts drew no colour at all:\n%s", ansi.Strip(view))
			}
			for _, c := range found {
				if _, ok := known[c[1]]; !ok {
					t.Errorf("after SetRoles the contacts drew rgb(%s), "+
						"which is not in the palette they were given", c[1])
				}
			}
		})
	}
}
