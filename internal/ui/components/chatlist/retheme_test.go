package chatlist

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// A new palette reaches every state the chat list draws. Each of the three
// had its own way of keeping the old one: the spinner and the empty-list
// placeholder hold styles New built, and the rows are drawn by a method value
// bound to a copy of the model as New left it — palette included.
func TestSetRolesRedrawsTheChatListInTheNewPalette(t *testing.T) {
	tests := []struct {
		name  string
		state func(m *Model)
	}{
		{"rows", func(m *Model) {
			m.loading = false
			m.store.Chats.Set(&telegram.Chat{
				ID: 1, Type: telegram.ChatTypeSupergroup, Title: "infra-oncall",
				UnreadCount: 3,
			})
			*m.dirty = true
		}},
		{"loading", func(m *Model) {}},
		{"empty", func(m *Model) { m.loading = false }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := theme.MarkerRoles()
			after, known := theme.SecondMarkerRoles()

			m := New(store.NewStore(), nil, before)
			m.SetSize(38, 12)
			tt.state(&m)
			_ = m.View()
			m.SetRoles(after)

			view := m.View()
			found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
			if len(found) == 0 {
				t.Fatalf("the chat list drew no colour at all:\n%s", ansi.Strip(view))
			}
			for _, c := range found {
				if _, ok := known[c[1]]; !ok {
					t.Errorf("after SetRoles the chat list drew rgb(%s), "+
						"which is not in the palette it was given", c[1])
				}
			}
		})
	}
}
