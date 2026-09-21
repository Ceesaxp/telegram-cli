package telegram

import (
	"testing"

	"github.com/gotd/td/tg"
)

// groupMention is a message in the basic group 5 that names the reader and
// that the reader has not opened yet: what the phone counts as an @.
func groupMention() *tg.Message {
	return &tg.Message{
		ID:          9,
		PeerID:      &tg.PeerChat{ChatID: 5},
		FromID:      &tg.PeerUser{UserID: 4},
		Message:     "@reader look",
		Mentioned:   true,
		MediaUnread: true,
	}
}

// The dialog is the only place the per-chat count of unread mentions
// comes from: no update carries it. It has to survive the conversion or
// the @ on a chat's row has nothing to start from.
func TestADialogCarriesItsUnreadMentions(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	chats, _, _ := c.chatsFromDialogParts(
		[]tg.DialogClass{&tg.Dialog{Peer: &tg.PeerChat{ChatID: 5}, UnreadMentionsCount: 2}},
		nil, nil,
		[]tg.ChatClass{&tg.Chat{ID: 5, Title: "g"}},
	)

	if len(chats) != 1 {
		t.Fatalf("converted %d chats, want 1", len(chats))
	}
	if got := chats[0].UnreadMentionsCount; got != 2 {
		t.Errorf("UnreadMentionsCount = %d, want the dialog's 2", got)
	}
}

// Telegram flags a message that names the reader, and keeps it an unread
// mention for as long as its contents are unread. Without the flag on the
// message nothing downstream can count one arriving.
func TestAnIncomingGroupMentionIsUnread(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	if got := c.messageFromTG(groupMention()); !got.UnreadMention {
		t.Error("an unread mention in a group did not say so")
	}
}

// What the phone counts is narrower than "the message names the reader".
// The reader's own message is not news to them, one they have opened is
// read, and only a group has mentions at all: a DM is addressed to the
// reader already, and nobody can mention anybody in a channel's posts.
func TestOnlyAnUnopenedIncomingGroupMentionIsUnread(t *testing.T) {
	for name, change := range map[string]func(*tg.Message){
		"the reader's own message": func(m *tg.Message) { m.Out = true },
		"a message that does not name the reader": func(m *tg.Message) {
			m.Mentioned = false
		},
		"a mention already opened": func(m *tg.Message) { m.MediaUnread = false },
		"a message in a DM":        func(m *tg.Message) { m.PeerID = &tg.PeerUser{UserID: 4} },
		"a channel post": func(m *tg.Message) {
			m.PeerID = &tg.PeerChannel{ChannelID: 9}
			m.Post = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := &Client{files: newFileRegistry()}
			m := groupMention()
			change(m)

			if got := c.messageFromTG(m); got.UnreadMention {
				t.Errorf("%s counts as an unread mention", name)
			}
		})
	}
}
