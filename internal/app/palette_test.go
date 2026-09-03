package app

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/hintbar"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/mediaview"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/topbar"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// truecolourSeq finds every 24-bit foreground or background in a rendered
// string, capturing it as "r;g;b".
var truecolourSeq = regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`)

// A component is given the palette once, in its constructor, and everything
// it draws has to come from that palette.
//
// This has gone wrong twice, in opposite directions, which is why the table
// covers EVERY component that takes one rather than the ones that looked
// interesting at the time.
//
// First all three panels took a theme.Roles argument, dropped it on the
// floor, and installed theme.DarkRoles(false) — masked by a redundant
// SetRoles call at startup. Then, removing that setter, the chat list stopped
// being given a palette at all: its constructor set no roles, every colour
// was the zero value, and the list rendered unstyled.
func TestEveryComponentUsesThePaletteItWasGiven(t *testing.T) {
	marker, known := theme.MarkerRoles()
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
			m.SetFocused(true)
			return m.View()
		}},
		{"the thread", func() string {
			m := chatview.New(s, tg, marker)
			m.SetSize(60, 10)
			return m.View()
		}},
		{"the top bar", func() string {
			m := topbar.New(marker)
			m.SetWidth(80)
			m.SetClock("12:40")
			m.SetConnection(topbar.Connected, "connected")
			m.SetDevices(2)
			m.SetFolders([]topbar.Folder{{Name: "all", Active: true}, {Name: "work"}})
			return m.View()
		}},
		{"the hint bar", func() string {
			m := hintbar.New(marker)
			m.SetWidth(80)
			m.SetHints([]hintbar.Hint{{Key: "q", Label: "quit"}})
			m.SetRight("2 buffers")
			return m.View()
		}},
		{"the media overlay", func() string {
			m := mediaview.New(marker)
			m.SetSize(60, 10)
			m.Open("photo · nadia", "downloading…")
			return m.View()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := tt.view()

			found := truecolourSeq.FindAllStringSubmatch(view, -1)
			if len(found) == 0 {
				t.Fatalf("%s drew no colour at all — it is not using the "+
					"palette it was given:\n%s", tt.name,
					strings.ReplaceAll(view, "\x1b", "ESC"))
			}
			for _, m := range found {
				if _, ok := known[m[1]]; !ok {
					t.Errorf("%s drew rgb(%s), which is not in the palette "+
						"it was given", tt.name, m[1])
				}
			}
		})
	}
}

// And the palette the app resolves is the one the assembled screen uses.
func TestTheAppHandsOutOnePalette(t *testing.T) {
	m := framedModel(t, 120, 30)

	view := m.View().Content
	bg := backgroundSeq(m.roles.Bg)
	if bg == "" {
		t.Fatal("precondition: the resolved palette emits no background")
	}
	if !strings.Contains(view, bg) {
		t.Errorf("the frame's bg does not appear in the rendered screen")
	}
}

// The grid draws the gutter and the renderer draws the body, and they have to
// agree about what amber is. The renderer holds its own copy of the palette,
// so a constructor that set the grid's and forgot the renderer's would give a
// message whose timestamp and whose text came from two different themes —
// visible only on a message with inline formatting.
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
