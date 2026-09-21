package telegram

import (
	"context"
	"errors"
	"reflect"
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

// readContentsInvoker stands in for the server: it knows the basic group 5
// and the channel 9, answers both kinds of readMessageContents with readErr
// or with success, and records what it was asked.
type readContentsInvoker struct {
	readErr  error
	messages []*tg.MessagesReadMessageContentsRequest
	channels []*tg.ChannelsReadMessageContentsRequest
}

func (f *readContentsInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	switch req := input.(type) {
	case *tg.MessagesReadMessageContentsRequest:
		f.messages = append(f.messages, req)
		if f.readErr != nil {
			return f.readErr
		}
		affected := output.(*tg.MessagesAffectedMessages)
		affected.Pts, affected.PtsCount = 1, 1
		return nil
	case *tg.ChannelsReadMessageContentsRequest:
		f.channels = append(f.channels, req)
		if f.readErr != nil {
			return f.readErr
		}
		output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
		return nil
	default:
		return readHistoryInvoker{}.Invoke(ctx, input, output)
	}
}

// A mention is cleared by reading the message's contents, and Telegram has
// two calls for it: one that takes bare message IDs, which are the
// account's own numbering and only mean something outside a channel, and
// one that names the channel whose numbering they are in.
func TestReadMentionsUsesTheChannelCallOnlyForAChannel(t *testing.T) {
	t.Run("a basic group", func(t *testing.T) {
		inv := &readContentsInvoker{}
		c, _ := viewClient(t, inv)

		if err := c.ReadMentions(basicGroupID, []int64{3, 7}); err != nil {
			t.Fatalf("ReadMentions: %v", err)
		}

		if len(inv.messages) != 1 || len(inv.channels) != 0 {
			t.Fatalf("made %d messages and %d channel calls, want one messages call",
				len(inv.messages), len(inv.channels))
		}
		if got := inv.messages[0].ID; !slices.Equal(got, []int{3, 7}) {
			t.Errorf("cleared %v, want [3 7]", got)
		}
	})

	t.Run("a channel", func(t *testing.T) {
		inv := &readContentsInvoker{}
		c, _ := viewClient(t, inv)

		if err := c.ReadMentions(channelChatID(9), []int64{3, 7}); err != nil {
			t.Fatalf("ReadMentions: %v", err)
		}

		if len(inv.channels) != 1 || len(inv.messages) != 0 {
			t.Fatalf("made %d channel and %d messages calls, want one channel call",
				len(inv.channels), len(inv.messages))
		}
		req := inv.channels[0]
		if ch, ok := req.Channel.(*tg.InputChannel); !ok || ch.ChannelID != 9 {
			t.Errorf("cleared in %#v, want the channel 9", req.Channel)
		}
		if !slices.Equal(req.ID, []int{3, 7}) {
			t.Errorf("cleared %v, want [3 7]", req.ID)
		}
	})
}

// Clearing mentions tells the chat list at once, the way a read does: no
// update carries the count, so without it the @ would stay until the next
// dialog reload.
func TestReadMentionsAnnouncesTheClear(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a basic group": basicGroupID,
		"a channel":     channelChatID(9),
	} {
		t.Run(name, func(t *testing.T) {
			c, got := viewClient(t, &readContentsInvoker{})

			if err := c.ReadMentions(chatID, []int64{3, 7}); err != nil {
				t.Fatalf("ReadMentions: %v", err)
			}

			want := ChatMentionsReadMsg{ChatId: chatID, MessageIds: []int64{3, 7}}
			if len(*got) != 1 || !reflect.DeepEqual((*got)[0], want) {
				t.Errorf("published %#v, want %#v", *got, want)
			}
		})
	}
}

// A clear the server refused did not happen, and the @ has to say so.
func TestReadMentionsAnnouncesNothingWhenTheClearFails(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a basic group": basicGroupID,
		"a channel":     channelChatID(9),
	} {
		t.Run(name, func(t *testing.T) {
			c, got := viewClient(t, &readContentsInvoker{readErr: errors.New("no connection")})

			if err := c.ReadMentions(chatID, []int64{7}); err == nil {
				t.Fatal("ReadMentions reported success for a failed clear")
			}
			if len(*got) != 0 {
				t.Errorf("a failed clear published %#v", *got)
			}
		})
	}
}

// readAllMentionsInvoker stands in for the server: it knows the basic group
// 5 and the channel 9, answers messages.readMentions with readErr or with
// the offsets queued in offsets, and records what it was asked.
type readAllMentionsInvoker struct {
	readErr error
	offsets []int
	asked   []*tg.MessagesReadMentionsRequest
}

func (f *readAllMentionsInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	req, ok := input.(*tg.MessagesReadMentionsRequest)
	if !ok {
		return readHistoryInvoker{}.Invoke(ctx, input, output)
	}
	f.asked = append(f.asked, req)
	if f.readErr != nil {
		return f.readErr
	}
	affected := output.(*tg.MessagesAffectedHistory)
	affected.Pts, affected.PtsCount = 1, 1
	if len(f.offsets) > 0 {
		affected.Offset, f.offsets = f.offsets[0], f.offsets[1:]
	}
	return nil
}

// Clearing every mention in a chat tells the chat list at once, and says
// it was all of them: the list does not know which messages they were.
func TestReadAllMentionsAnnouncesTheClear(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a basic group": basicGroupID,
		"a channel":     channelChatID(9),
	} {
		t.Run(name, func(t *testing.T) {
			inv := &readAllMentionsInvoker{}
			c, got := viewClient(t, inv)

			if err := c.ReadAllMentions(chatID); err != nil {
				t.Fatalf("ReadAllMentions: %v", err)
			}

			if len(inv.asked) != 1 {
				t.Fatalf("asked the server %d times, want once", len(inv.asked))
			}
			if _, ok := inv.asked[0].GetTopMsgID(); ok {
				t.Error("the clear was scoped to a thread; it is for the whole chat")
			}
			want := ChatMentionsReadMsg{ChatId: chatID, All: true}
			if len(*got) != 1 || !reflect.DeepEqual((*got)[0], want) {
				t.Errorf("published %#v, want %#v", *got, want)
			}
		})
	}
}

// messages.readMentions answers the way messages.readReactions does: a
// positive offset means it stopped partway and wants the call made again.
// Taking the first answer as the end left the older mentions unread.
func TestReadAllMentionsRepeatsUntilTheServerIsDone(t *testing.T) {
	inv := &readAllMentionsInvoker{offsets: []int{40, 10, 0}}
	c, got := viewClient(t, inv)

	if err := c.ReadAllMentions(basicGroupID); err != nil {
		t.Fatalf("ReadAllMentions: %v", err)
	}

	if len(inv.asked) != 3 {
		t.Fatalf("asked the server %d times, want 3: once per batch", len(inv.asked))
	}
	if len(*got) != 1 {
		t.Errorf("published %#v, want one announcement for the whole clear", *got)
	}
}

// A server that never says it is done must not be called in a tight loop
// until the timeout. The walk stops at the cap, without an error, and
// without claiming a clear the server never confirmed: the @ stays.
func TestReadAllMentionsStopsAtTheCap(t *testing.T) {
	never := make([]int, 3*maxReadMentionsCalls)
	for i := range never {
		never[i] = 1
	}
	inv := &readAllMentionsInvoker{offsets: never}
	c, got := viewClient(t, inv)

	if err := c.ReadAllMentions(basicGroupID); err != nil {
		t.Fatalf("ReadAllMentions: %v, want a quiet stop", err)
	}

	if len(inv.asked) != maxReadMentionsCalls {
		t.Fatalf("asked the server %d times, want the cap of %d", len(inv.asked), maxReadMentionsCalls)
	}
	if len(*got) != 0 {
		t.Errorf("a clear the server never finished published %#v", *got)
	}
}

// A clear the server refused did not happen.
func TestReadAllMentionsAnnouncesNothingWhenTheClearFails(t *testing.T) {
	c, got := viewClient(t, &readAllMentionsInvoker{readErr: errors.New("no connection")})

	if err := c.ReadAllMentions(basicGroupID); err == nil {
		t.Fatal("ReadAllMentions reported success for a failed clear")
	}
	if len(*got) != 0 {
		t.Errorf("a failed clear published %#v", *got)
	}
}

// Nothing to clear is not a clear, and not a call either.
func TestReadMentionsDoesNothingWithoutAMessage(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a basic group": basicGroupID,
		"a channel":     channelChatID(9),
	} {
		t.Run(name, func(t *testing.T) {
			inv := &readContentsInvoker{}
			c, got := viewClient(t, inv)

			if err := c.ReadMentions(chatID, nil); err != nil {
				t.Fatalf("ReadMentions: %v", err)
			}

			if len(inv.messages)+len(inv.channels) != 0 {
				t.Errorf("clearing nothing asked the server %d times", len(inv.messages)+len(inv.channels))
			}
			if len(*got) != 0 {
				t.Errorf("clearing nothing published %#v", *got)
			}
		})
	}
}
