package chatlist

import (
	"testing"

	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

func previewModel() Model {
	s := store.NewStore()
	s.Users.Set(&telegram.User{ID: 11, FirstName: "nadia"})
	s.Users.Set(&telegram.User{ID: 99, FirstName: "me"})
	return Model{store: s, myUserID: 99}
}

func entryFor(kind telegram.ChatType, sender int64, text string) *store.ChatEntry {
	return &store.ChatEntry{
		Chat: &telegram.Chat{ID: 1, Type: kind, Title: "somewhere"},
		LastMessage: &telegram.Message{
			ID: 5, SenderID: &telegram.MessageSenderUser{UserID: sender},
			Content: &telegram.MessageText{Text: &telegram.FormattedText{Text: text}},
		},
	}
}

// TestThePreviewNamesTheSenderWhereItSaysSomething.
func TestThePreviewNamesTheSenderWhereItSaysSomething(t *testing.T) {
	m := previewModel()

	cases := map[string]struct {
		entry *store.ChatEntry
		want  string
	}{
		"a group has several people in it": {
			entryFor(telegram.ChatTypeSupergroup, 11, "rebased, CI green"),
			"nadia: rebased, CI green",
		},
		"your own message, anywhere": {
			entryFor(telegram.ChatTypePrivate, 99, "pushing the tag now"),
			"you: pushing the tag now",
		},
		"a one-to-one chat is already named by its title": {
			entryFor(telegram.ChatTypePrivate, 11, "sounds good — thurs then"),
			"sounds good — thurs then",
		},
		"a channel's posts are the channel's": {
			entryFor(telegram.ChatTypeChannel, 11, "v0.4.1 — keymap overhaul"),
			"v0.4.1 — keymap overhaul",
		},
		"a sender nobody has named yet": {
			entryFor(telegram.ChatTypeSupergroup, 4242, "who said this"),
			"who said this",
		},
	}
	for name, tc := range cases {
		if got := m.previewWithSender(tc.entry); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", name, got, tc.want)
		}
	}
}

// TestAnEmptyPreviewGainsNoName. "nadia: " over nothing is a row that says
// somebody spoke and refuses to say what.
func TestAnEmptyPreviewGainsNoName(t *testing.T) {
	entry := entryFor(telegram.ChatTypeSupergroup, 11, "")
	if got := previewModel().previewWithSender(entry); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestTheBadgeSaysHowLoudlyItIsWaiting. The number alone is the same claim
// for a chat that will interrupt you and one you have muted.
func TestTheBadgeSaysHowLoudlyItIsWaiting(t *testing.T) {
	if got := unreadBadge(4, false); got != "[4]" {
		t.Errorf("unmuted badge = %q, want [4]", got)
	}
	if got := unreadBadge(31, true); got != "(31)" {
		t.Errorf("muted badge = %q, want (31)", got)
	}
	// Three cells either way for a single digit, which is what keeps the
	// column aligned down a list of both kinds.
	if len(unreadBadge(4, false)) != len(unreadBadge(4, true)) {
		t.Error("the two badges are different widths for the same count")
	}
}

// TestBufferIndexIsTheRowTheReaderCanSee.
func TestBufferIndexIsTheRowTheReaderCanSee(t *testing.T) {
	m := previewModel()
	m.list = &widgets.List{}
	for _, id := range []string{"-1001", "901", "-1002"} {
		m.list.Items = append(m.list.Items, widgets.ListItem{ID: id})
	}

	for id, want := range map[int64]int{-1001: 1, 901: 2, -1002: 3, 4242: 0} {
		if got := m.BufferIndex(id); got != want {
			t.Errorf("BufferIndex(%d) = %d, want %d", id, got, want)
		}
	}
}
