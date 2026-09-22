package chatview

import (
	"regexp"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// A new palette reaches every line of the thread, including the ones already
// drawn. Three things stood in its way: the grid caches each message's
// rendered lines, the renderer holds its own copy of the palette for the
// message bodies, and the grid's own copy is the third. Missing any one of
// them leaves old colours on screen — the cache the most visibly, since it
// serves the old lines back until the message changes.
func TestSetRolesRedrawsTheThreadInTheNewPalette(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state func(m *Model)
	}{
		{"messages", func(m *Model) {}},
		{"find bar", func(m *Model) { m.OpenFind() }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := theme.MarkerRoles()
			after, known := theme.SecondMarkerRoles()

			m := New(store.NewStore(), nil, before)
			m.SetSize(60, 12)
			m.OpenChat(testChatID, "test")
			m.MarkLoadedForTest()
			// Inline code is drawn by the renderer, the rest by the grid.
			m.store.Messages.Append(testChatID, &telegram.Message{
				ID: 1, ChatID: testChatID, Date: fixedDate,
				SenderID: &telegram.MessageSenderUser{UserID: 200},
				Content: &telegram.MessageText{Text: &telegram.FormattedText{
					Text: "run deploy.sh now",
					Entities: []*telegram.TextEntity{
						{Offset: 4, Length: 9, Type: &telegram.TextEntityTypeCode{}},
					},
				}},
			})
			m.store.Messages.Append(testChatID, textMessage(2, 300, "on it"))
			tt.state(&m)

			if _ = m.View(); m.cache.len() == 0 {
				t.Fatal("precondition: drawing the thread cached nothing")
			}
			m.SetRoles(after)

			view := m.View()
			found := regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`).FindAllStringSubmatch(view, -1)
			if len(found) == 0 {
				t.Fatalf("the thread drew no colour at all:\n%s", ansi.Strip(view))
			}
			for _, c := range found {
				if _, ok := known[c[1]]; !ok {
					t.Errorf("after SetRoles the thread drew rgb(%s), "+
						"which is not in the palette it was given", c[1])
				}
			}
		})
	}
}
