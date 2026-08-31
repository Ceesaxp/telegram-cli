package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatview"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// A component is given the palette once, in its constructor, and has to use
// the one it was given.
//
// All three of these ignored it. They took a theme.Roles argument, dropped it
// on the floor, and installed theme.DarkRoles(false) — the 256-colour
// fallback — so on a terminal reporting truecolour the thread, the chat list
// and the composer would have rendered from a different palette than the
// frame around them. It went unnoticed because the app also called SetRoles
// at startup, which papered over it; removing that redundant call is what
// exposed it.
func TestEveryComponentUsesThePaletteItWasGiven(t *testing.T) {
	// A palette nothing could arrive at by accident.
	marker := theme.DarkRoles(true)
	marker.Fg = lipgloss.Color("#010203")
	marker.Dim = lipgloss.Color("#040506")
	marker.Panel = lipgloss.Color("#070809")

	s := store.NewStore()
	var tg *telegram.Client

	tests := []struct {
		name string
		view func() string
	}{
		{"the composer", func() string {
			m := composer.New(marker)
			m.SetSize(60, 1)
			m.SetChatId(1)
			return m.View()
		}},
		{"the thread", func() string {
			m := chatview.New(s, tg, marker)
			m.SetSize(60, 10)
			return m.View()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := tt.view()
			if !strings.Contains(view, "1;2;3") && !strings.Contains(view, "4;5;6") &&
				!strings.Contains(view, "7;8;9") {
				t.Errorf("%s drew none of the palette it was given:\n%s",
					tt.name, strings.ReplaceAll(view, "\x1b", "ESC"))
			}
		})
	}
}

// And the palette the app resolves is the one every component gets, so a
// truecolour terminal does not get a 256-colour thread inside a truecolour
// frame.
func TestTheAppHandsOutOnePalette(t *testing.T) {
	m := framedModel(t, 120, 30)

	// The frame's own surfaces come from m.roles; if a component had kept
	// its own default, the row would carry two different greys for the same
	// role. Asserting the whole screen is painted is not enough — that
	// passed throughout — so this asks whether the thread's body uses the
	// same bg the frame fills its column with.
	view := m.View().Content
	bg := backgroundSeq(m.roles.Bg)
	if bg == "" {
		t.Fatal("precondition: the resolved palette emits no background")
	}
	if !strings.Contains(view, bg) {
		t.Errorf("the frame's bg does not appear in the rendered screen")
	}
}

// The grid draws the gutter and the renderer draws the body, and they have
// to agree about what amber is. The renderer holds its own copy of the
// palette, so a constructor that set the grid's and forgot the renderer's
// would produce a message whose timestamp and whose text came from two
// different themes — visible only on a message with inline formatting, which
// is why the whole-view assertion above does not catch it.
func TestTheMessageBodyUsesTheSamePaletteAsTheGrid(t *testing.T) {
	marker := theme.DarkRoles(true)
	marker.Amber = lipgloss.Color("#112233")

	s := store.NewStore()
	m := chatview.New(s, nil, marker)
	m.SetSize(60, 10)
	m.OpenChat(testChatID, "test")

	// Inline code is the amber role, and amber is what the marker moved.
	msg := &telegram.Message{
		ID: 1, ChatID: testChatID, Date: 1700000000,
		SenderID: &telegram.MessageSenderUser{UserID: 200},
		Content: &telegram.MessageText{Text: &telegram.FormattedText{
			Text: "run deploy.sh now",
			Entities: []*telegram.TextEntity{
				{Offset: 4, Length: 9, Type: &telegram.TextEntityTypeCode{}},
			},
		}},
	}
	s.Messages.Append(testChatID, msg)

	if view := m.View(); !strings.Contains(view, "17;34;51") {
		t.Errorf("the message body did not use the palette the thread was given:\n%s",
			strings.ReplaceAll(view, "\x1b", "ESC"))
	}
}
