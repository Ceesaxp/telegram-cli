package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// notifyModel is an app that will actually try to notify. The default test
// config has notifications off, which makes every assertion about them pass
// for the wrong reason.
//
// The method is pinned to "terminal" so the sequence is produced regardless
// of what terminal the suite happens to be running under — the allowlist is
// internal/notification's to test.
func notifyModel(t *testing.T) Model {
	t.Helper()

	// The held-notification backstop, shortened. rawSequences runs the
	// commands it is given, and a tea.Tick command blocks for its whole
	// duration — so the real four seconds would be paid by every test that
	// holds anything.
	prev := noticeGrace
	noticeGrace = time.Millisecond
	t.Cleanup(func() { noticeGrace = prev })

	cfg := &config.Config{}
	cfg.Notifications.Enabled = true
	cfg.Notifications.ShowPreview = true
	cfg.Notifications.Method = config.NotifyMethodTerminal

	m := New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	m.screen = ScreenMain
	return m
}

// incoming is a message from somebody else, in a chat that is not open.
func incoming(chatID int64) telegram.NewMessageMsg {
	return telegram.NewMessageMsg{Message: &telegram.Message{
		ID: 1, ChatID: chatID, Date: 1700000000,
		SenderID: &telegram.MessageSenderUser{UserID: 999},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hello"}},
	}}
}

// rawSequences returns every escape sequence the returned command would
// write, walking a batch if that is what came back.
func rawSequences(t *testing.T, cmd tea.Cmd) []string {
	t.Helper()
	if cmd == nil {
		return nil
	}

	var out []string
	var walk func(tea.Msg)
	walk = func(msg tea.Msg) {
		switch v := msg.(type) {
		case tea.RawMsg:
			s, _ := v.Msg.(string)
			out = append(out, s)
		case tea.BatchMsg:
			for _, c := range v {
				if c != nil {
					walk(c())
				}
			}
		}
	}
	walk(cmd())
	return out
}

// A muted chat stays silent.
func TestAMutedChatDoesNotNotify(t *testing.T) {
	m := notifyModel(t)
	m.store.Chats.Set(&telegram.Chat{
		ID: 7, Type: telegram.ChatTypePrivate, Title: "Ana", Muted: true,
	})

	_, cmd := m.Update(incoming(7))
	if got := rawSequences(t, cmd); len(got) != 0 {
		t.Errorf("a muted chat produced %d notification(s): %q", len(got), got)
	}
}

// And an unmuted one does — otherwise the test above passes by notifying
// nobody about anything.
func TestAnUnmutedChatNotifies(t *testing.T) {
	m := notifyModel(t)
	m.store.Chats.Set(&telegram.Chat{
		ID: 7, Type: telegram.ChatTypePrivate, Title: "Ana", Muted: false,
	})

	got := rawSequences(t, must(m.Update(incoming(7))))
	if len(got) != 1 {
		t.Fatalf("an unmuted chat produced %d notifications, want 1: %q", len(got), got)
	}
	if !strings.Contains(got[0], "Ana") || !strings.Contains(got[0], "hello") {
		t.Errorf("the notification says %q", got[0])
	}
}

// Opening a chat unmuted it.
//
// OpenChat fetches the chat by resolving its peer, which reports who a chat
// is and nothing about the reader's relationship with it. That partial view
// went into the store as though it were complete, so opening a muted chat
// cleared its mute flag — and every message after that rang.
func TestOpeningAChatDoesNotUnmuteIt(t *testing.T) {
	m := notifyModel(t)
	m.store.Chats.Set(&telegram.Chat{
		ID: 7, Type: telegram.ChatTypePrivate, Title: "Ana",
		Muted: true, UnreadCount: 3,
	})

	// What OpenChat sends back once the fetch returns.
	next, _ := m.Update(telegram.ChatUpdateMsg{
		Chat: &telegram.Chat{
			ID: 7, Type: telegram.ChatTypePrivate, Title: "Ana", Muted: true,
		},
		FromPeer: true,
	})
	m = next.(Model)

	entry, ok := m.store.Chats.Get(7)
	if !ok {
		t.Fatal("the chat is gone")
	}
	if !entry.Chat.Muted {
		t.Error("opening the chat unmuted it")
	}
	if entry.UnreadCount != 3 {
		t.Errorf("opening the chat set the unread count to %d", entry.UnreadCount)
	}

	if got := rawSequences(t, must(m.Update(incoming(7)))); len(got) != 0 {
		t.Errorf("after opening, the muted chat notified: %q", got)
	}
}

// Our own message, arriving as an update because it was sent from another
// device, is not news.
func TestOurOwnMessageDoesNotNotify(t *testing.T) {
	m := notifyModel(t)
	m.store.Chats.Set(&telegram.Chat{ID: 7, Type: telegram.ChatTypePrivate, Title: "Ana"})

	msg := incoming(7)
	msg.Message.IsOutgoing = true

	if got := rawSequences(t, must(m.Update(msg))); len(got) != 0 {
		t.Errorf("our own message notified: %q", got)
	}
}

func must(_ tea.Model, cmd tea.Cmd) tea.Cmd { return cmd }
