package app

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
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

// opsGroup is a basic group, the plainest chat completion works in.
func opsGroup() *telegram.Chat {
	return &telegram.Chat{ID: basicGroupID, Type: telegram.ChatTypeBasicGroup, Title: "ops"}
}

// nadia is a member with a username, so choosing her types it.
func nadia() *telegram.User {
	return &telegram.User{ID: 7, FirstName: "Nadia", LastName: "Feld", Username: "nadia"}
}

// Tab cycles panels from the composer, but while the picker is open it is
// one of the two keys that insert — the picker's hint says so, and the
// composer has to be the one to hear it.
func TestTabInsertsFromAnOpenPicker(t *testing.T) {
	m := openedChat(t, opsGroup())
	m.composer.SetMentionCandidates(basicGroupID, []*telegram.User{nadia()})
	m = typeText(t, m, "@na")
	if !m.composer.MentionActive() {
		t.Fatal("setup: the picker did not open")
	}

	m = update(t, m, "\t")
	if m.focus != PanelComposer {
		t.Fatalf("tab moved focus to %v; it belongs to the picker while it is open", m.focus)
	}
	if got := m.composer.Draft(); got != "@nadia " {
		t.Fatalf("draft = %q, want the chosen member inserted", got)
	}
}

// And with the picker closed Tab is panel cycling again — even with the
// half-typed @ still in the draft.
func TestTabCyclesPanelsOnceThePickerCloses(t *testing.T) {
	m := openedChat(t, opsGroup())
	m.composer.SetMentionCandidates(basicGroupID, []*telegram.User{nadia()})
	m = typeText(t, m, "@na")
	m = update(t, m, "\x1b") // esc closes the picker, and only that
	if m.composer.MentionActive() || m.focus != PanelComposer {
		t.Fatalf("setup: esc left picker open = %v, focus %v", m.composer.MentionActive(), m.focus)
	}

	m = update(t, m, "\t")
	if m.focus == PanelComposer {
		t.Fatal("tab with the picker closed did not cycle panels")
	}
	if got := m.composer.Draft(); got != "@na" {
		t.Fatalf("draft = %q, want it untouched", got)
	}
}

// Enter is the picker's too: it inserts, and nothing is sent. A half-typed
// "@na" going to a group is not something to do on the reader's behalf.
func TestEnterInsertsFromAnOpenPickerRatherThanSending(t *testing.T) {
	m := openedChat(t, opsGroup())
	m.composer.SetMentionCandidates(basicGroupID, []*telegram.User{nadia()})
	m = typeText(t, m, "@na")

	m, cmd := updateCmd(t, m, "\r")
	for _, msg := range flattenCmd(cmd) {
		if sent, ok := msg.(composer.MessageSubmittedMsg); ok {
			t.Fatalf("enter sent %q with the picker open", sent.Text)
		}
	}
	if got := m.composer.Draft(); got != "@nadia " {
		t.Fatalf("draft = %q, want the chosen member inserted", got)
	}
}
