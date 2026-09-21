package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/hintbar"
	"github.com/charmbracelet/lipgloss"
)

// A theme file named by ui.theme is the palette the components are built
// with: config.Load reads it, app.New draws it over its base at the one
// dispatch point, and what the components render is the theme's — its
// colours, and its sender ramp.
//
// Through config.Load rather than a hand-built Config, because the spec
// rides on unexported fields only Load can set, and because the whole path
// is what a user gets.
func TestTheComponentsDrawWithTheConfiguredThemeFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "themes", "test.toml"), `
[colors]
cyan = "#123456"
red  = "#654321"

[senders]
ramp = ["red"]
`)
	writeFile(t, filepath.Join(dir, "config.toml"), "[ui]\ntheme = \"test\"\n")
	t.Setenv("TELETUI_CONFIG", filepath.Join(dir, "config.toml"))
	t.Setenv("XDG_CONFIG_HOME", dir)
	// Depth comes from the environment, as it does for the real app.
	t.Setenv("COLORTERM", "truecolor")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if w := config.StartupWarnings(cfg); len(w) != 0 {
		t.Fatalf("precondition: the theme loaded with warnings %q", w)
	}
	s := store.NewStore()
	m := New(cfg, nil, s, telegram.NewTUIAuthorizer(cfg))

	if string(m.roles.Cyan) != "#123456" {
		t.Errorf("the app's cyan is %q, want the theme's #123456", m.roles.Cyan)
	}

	// The hint bar's keys are cyan.
	m.hintBar.SetWidth(80)
	m.hintBar.SetHints([]hintbar.Hint{{Key: "q", Label: "quit"}})
	if bar := m.hintBar.View(); !strings.Contains(bar, foregroundSeq(m.roles.Cyan)) {
		t.Errorf("the hint bar does not draw the theme's cyan:\n%s",
			strings.ReplaceAll(bar, "\x1b", "ESC"))
	}

	// The theme's ramp is red alone, so every other sender is red.
	m.chatView.SetSize(60, 10)
	m.chatView.OpenChat(testChatID, "test")
	s.Messages.Append(testChatID, &telegram.Message{
		ID: 1, ChatID: testChatID, Date: 1700000000,
		SenderID: &telegram.MessageSenderUser{UserID: 200},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hello"}},
	})
	if string(m.roles.Red) != "#654321" {
		t.Fatalf("precondition: the app's red is %q, want the theme's #654321", m.roles.Red)
	}
	if view := m.chatView.View(); !strings.Contains(view, foregroundSeq(m.roles.Red)) {
		t.Errorf("the thread does not colour senders from the theme's ramp:\n%s",
			strings.ReplaceAll(view, "\x1b", "ESC"))
	}

	// And the rail names the same people, so it is handed the same ramp: a
	// group's member is red there too. Nothing else in the rail is red, so
	// a rail left on the default ramp draws none.
	const group = int64(42)
	s.Chats.Set(&telegram.Chat{ID: group, Type: telegram.ChatTypeSupergroup, Title: "group"})
	m.rail.SetSize(30, 12)
	m.rail.SetDataForTest(group, nil, nil, []*telegram.ChatMember{
		{MemberID: &telegram.MessageSenderUser{UserID: 300}},
	}, 1)
	if view := m.rail.View(); !strings.Contains(view, foregroundSeq(m.roles.Red)) {
		t.Errorf("the rail does not colour members from the theme's ramp:\n%s",
			strings.ReplaceAll(view, "\x1b", "ESC"))
	}
}

// foregroundSeq is the escape a colour renders to as a foreground, built by
// lipgloss rather than written out: termenv truncates each channel as it
// converts, so #654321 is 101;67;32 on the wire, not 101;67;33.
func foregroundSeq(c lipgloss.Color) string {
	out := lipgloss.NewStyle().Foreground(c).Render("x")
	return out[:strings.Index(out, "x")]
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
