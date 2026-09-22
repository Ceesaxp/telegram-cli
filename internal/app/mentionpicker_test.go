package app

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/charmbracelet/x/ansi"
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

// said is a text message in chat from sender.
func said(chatID, id int64, sender telegram.MessageSender) *telegram.Message {
	return &telegram.Message{
		ID: id, ChatID: chatID, SenderID: sender,
		Content: &telegram.MessageText{Text: &telegram.FormattedText{Text: "hi"}},
	}
}

func byUser(id int64) telegram.MessageSender { return &telegram.MessageSenderUser{UserID: id} }

// userIDs lists who users are, for a readable failure.
func userIDs(users []*telegram.User) []int64 {
	out := make([]int64, 0, len(users))
	for _, u := range users {
		out = append(out, u.ID)
	}
	return out
}

// The members an @ offers before any search answers are the people who
// have been talking: newest first, each once, and only people — not the
// reader, whom mentioning notifies nobody, and not a channel posting in the
// group, which is not a member at all. A sender the store cannot name has
// nothing to insert, so is left for the server to find.
func TestMentionCandidatesAreTheRecentSenders(t *testing.T) {
	m := sizedMainModel(t)
	m.myUserId = 1
	for _, u := range []*telegram.User{
		{ID: 1, FirstName: "Me"},
		{ID: 2, FirstName: "Ivo"},
		{ID: 3, FirstName: "Sam"},
		{ID: 4, FirstName: "Mira"},
	} {
		m.store.Users.Set(u)
	}
	for i, sender := range []telegram.MessageSender{
		byUser(3),
		byUser(2),
		&telegram.MessageSenderChat{ChatID: channelID},
		byUser(3),
		byUser(1),
		byUser(9), // not in the store
		byUser(4),
	} {
		m.store.Messages.Append(basicGroupID, said(basicGroupID, int64(i+1), sender))
	}

	got := userIDs(m.mentionCandidates(basicGroupID))
	want := []int64{4, 3, 2}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", got, want)
		}
	}
}

// pickerNames is what the open picker lists, top to bottom, as plain text.
func pickerNames(t *testing.T, m Model) string {
	t.Helper()
	rows, ok := m.composer.MentionPicker(60, 6)
	if !ok {
		t.Fatal("the picker drew nothing")
	}
	return ansi.Strip(strings.Join(rows, "\n"))
}

// The candidates are handed over when the chat opens, so the first @ has
// something to offer at once.
func TestOpeningAGroupOffersItsRecentSenders(t *testing.T) {
	m := sizedMainModel(t)
	m.store.Users.Set(nadia())
	m.store.Messages.Append(basicGroupID, said(basicGroupID, 1, byUser(nadia().ID)))
	m.store.Chats.Set(opsGroup())
	m = send(t, m, chatlist.ChatSelectedMsg{ChatId: basicGroupID})
	m.setFocus(PanelComposer)

	m = typeText(t, m, "@")
	if names := pickerNames(t, m); !strings.Contains(names, "Nadia Feld") {
		t.Fatalf("picker =\n%s\nwant the recent sender offered", names)
	}
}

// Messages keep arriving after the chat opens, and the page that was
// loading when it opened lands later still. Each query hands the composer
// the senders as they are now.
func TestAMentionQueryRefreshesTheCandidates(t *testing.T) {
	m := openedChat(t, opsGroup())
	m.store.Users.Set(nadia())
	m.store.Messages.Append(basicGroupID, said(basicGroupID, 1, byUser(nadia().ID)))

	m, cmd := updateCmd(t, m, "@")
	var asked bool
	for _, msg := range flattenCmd(cmd) {
		if q, ok := msg.(composer.MentionQueryMsg); ok {
			m = send(t, m, q)
			asked = true
		}
	}
	if !asked {
		t.Fatal("setup: @ asked the host nothing")
	}
	if names := pickerNames(t, m); !strings.Contains(names, "Nadia Feld") {
		t.Fatalf("picker =\n%s\nwant the sender who spoke after the chat opened", names)
	}
}
