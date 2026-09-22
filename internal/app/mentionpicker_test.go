package app

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
)

// The @ picker's app half (issue #41): which chats offer it, who it offers,
// the member search behind it, where it is painted, and what a send does
// with the mentions it inserts. The picker itself — the token, the keys,
// the ranking — is the composer's, and tested there.

// Chat IDs in TDLib's encoding, which is what SearchChatMembers reads the
// kind of chat from.
const (
	basicGroupID int64 = -4101
	supergroupID int64 = -1001234567890
	channelID    int64 = -1009876543210
	privateID    int64 = 4102
)

// openedChat opens chat the way the chat list does — through the real
// switch path — and focuses the composer, ready to type into.
func openedChat(t *testing.T, chat *telegram.Chat) Model {
	t.Helper()
	m := sizedMainModel(t)
	m.store.Chats.Set(chat)
	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: chat.ID})
	m.setFocus(PanelComposer)
	return m
}

// typeText types each rune of text into m as its own key press.
func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = update(t, m, string(r))
	}
	return m
}

// A typed @ opens completion where there are members to choose between —
// a basic group, a supergroup — and nowhere else. A private chat has one
// person in it, and a broadcast channel's members cannot be mentioned.
func TestMentionCompletionOpensInGroupsOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		chat *telegram.Chat
		want bool
	}{
		{"basic group", &telegram.Chat{ID: basicGroupID, Type: telegram.ChatTypeBasicGroup, Title: "ops"}, true},
		{"supergroup", &telegram.Chat{ID: supergroupID, Type: telegram.ChatTypeSupergroup, Title: "infra"}, true},
		{"private chat", &telegram.Chat{ID: privateID, Type: telegram.ChatTypePrivate, Title: "Nadia"}, false},
		{"channel", &telegram.Chat{ID: channelID, Type: telegram.ChatTypeChannel, Title: "news"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := typeText(t, openedChat(t, tc.chat), "@")
			if got := m.composer.MentionActive(); got != tc.want {
				t.Fatalf("after @ the picker is open = %v, want %v", got, tc.want)
			}
		})
	}
}

// Completion is a property of the chat, so it follows the reader from a
// group into a private chat and back.
func TestMentionCompletionFollowsTheOpenChat(t *testing.T) {
	group := &telegram.Chat{ID: basicGroupID, Type: telegram.ChatTypeBasicGroup, Title: "ops"}
	dm := &telegram.Chat{ID: privateID, Type: telegram.ChatTypePrivate, Title: "Nadia"}

	m := openedChat(t, group)
	m.store.Chats.Set(dm)

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: privateID})
	m.setFocus(PanelComposer)
	if m = typeText(t, m, "@"); m.composer.MentionActive() {
		t.Fatal("the private chat opened the picker the group had enabled")
	}

	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: basicGroupID})
	m.setFocus(PanelComposer)
	if m = typeText(t, m, "@"); !m.composer.MentionActive() {
		t.Fatal("back in the group, @ did not open the picker")
	}
}
