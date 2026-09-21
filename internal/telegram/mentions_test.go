package telegram

import (
	"context"
	"slices"
	"testing"

	"github.com/gotd/td/bin"
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

// unreadMentionsInvoker stands in for the server: it knows the basic group
// 5 and the channel 9, answers messages.getUnreadMentions with answer, and
// records what it was asked.
type unreadMentionsInvoker struct {
	answer tg.MessagesMessagesClass
	asked  []*tg.MessagesGetUnreadMentionsRequest
}

func (f *unreadMentionsInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	req, ok := input.(*tg.MessagesGetUnreadMentionsRequest)
	if !ok {
		return readHistoryInvoker{}.Invoke(ctx, input, output)
	}
	f.asked = append(f.asked, req)
	output.(*tg.MessagesMessagesBox).Messages = f.answer
	return nil
}

// mentionsAt is a list of messages with the given IDs, in the order given.
func mentionsAt(ids ...int) []tg.MessageClass {
	out := make([]tg.MessageClass, len(ids))
	for i, id := range ids {
		out[i] = &tg.Message{ID: id, PeerID: &tg.PeerChat{ChatID: 5}}
	}
	return out
}

// A jump to the next mention goes to the oldest one first, the way the
// phone walks them. The server's order for this call is not documented, so
// the answer is put in order here rather than trusted.
func TestUnreadMentionsAreOldestFirst(t *testing.T) {
	inv := &unreadMentionsInvoker{answer: &tg.MessagesMessages{Messages: mentionsAt(30, 10, 20)}}
	c, _ := viewClient(t, inv)

	got, err := c.UnreadMentions(basicGroupID, 10)
	if err != nil {
		t.Fatalf("UnreadMentions: %v", err)
	}

	if want := []int64{10, 20, 30}; !slices.Equal(got, want) {
		t.Errorf("UnreadMentions = %v, want %v", got, want)
	}
}

// With no offset the call answers the NEWEST mentions, so a chat with more
// than limit of them would start its walk in the middle. Telegram Desktop
// asks from message 1 with a negative add_offset instead, which is the
// oldest page, and for the whole chat rather than one forum topic.
func TestUnreadMentionsAsksForTheOldestPage(t *testing.T) {
	inv := &unreadMentionsInvoker{answer: &tg.MessagesMessages{}}
	c, _ := viewClient(t, inv)

	if _, err := c.UnreadMentions(basicGroupID, 20); err != nil {
		t.Fatalf("UnreadMentions: %v", err)
	}

	if len(inv.asked) != 1 {
		t.Fatalf("asked the server %d times, want once", len(inv.asked))
	}
	req := inv.asked[0]
	if req.OffsetID != 1 || req.AddOffset != -20 || req.Limit != 20 {
		t.Errorf("asked with offset_id %d, add_offset %d, limit %d; want 1, -20, 20",
			req.OffsetID, req.AddOffset, req.Limit)
	}
	if req.MaxID != 0 || req.MinID != 0 {
		t.Errorf("asked with max_id %d, min_id %d; want neither", req.MaxID, req.MinID)
	}
	if _, ok := req.GetTopMsgID(); ok {
		t.Error("the list was scoped to a thread; it is for the whole chat")
	}
}

// The call answers in whichever messages shape the server picks: a slice
// for a group with more than a page, the channel form for a supergroup.
// Each carries the same list, and a "not modified" carries none.
func TestUnreadMentionsReadsEveryAnswerShape(t *testing.T) {
	for name, tc := range map[string]struct {
		answer tg.MessagesMessagesClass
		want   []int64
	}{
		"a slice":          {&tg.MessagesMessagesSlice{Messages: mentionsAt(8, 4)}, []int64{4, 8}},
		"channel messages": {&tg.MessagesChannelMessages{Messages: mentionsAt(8, 4)}, []int64{4, 8}},
		"not modified":     {&tg.MessagesMessagesNotModified{}, []int64{}},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := viewClient(t, &unreadMentionsInvoker{answer: tc.answer})

			got, err := c.UnreadMentions(basicGroupID, 10)
			if err != nil {
				t.Fatalf("UnreadMentions: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("UnreadMentions = %v, want %v", got, tc.want)
			}
		})
	}
}
