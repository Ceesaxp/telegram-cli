package telegram

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

// Unread reactions are their own counter, beside the unread messages: the
// heart the phone shows on a chat where somebody reacted to the reader's
// message. The dialog is where the count comes from, and it has to survive
// the conversion or nothing downstream can know a chat has any.
func TestADialogCarriesItsUnreadReactions(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	chats, _, _ := c.chatsFromDialogParts(
		[]tg.DialogClass{&tg.Dialog{Peer: &tg.PeerChat{ChatID: 5}, UnreadReactionsCount: 2}},
		nil, nil,
		[]tg.ChatClass{&tg.Chat{ID: 5, Title: "g"}},
	)

	if len(chats) != 1 {
		t.Fatalf("converted %d chats, want 1", len(chats))
	}
	if got := chats[0].UnreadReactionsCount; got != 2 {
		t.Errorf("UnreadReactionsCount = %d, want the dialog's 2", got)
	}
}

// listening registers the listener's handlers on a fresh dispatcher, the
// way NewListener does on the client's, and collects what they publish.
func listening(t *testing.T) (tg.UpdateDispatcher, *[]tea.Msg) {
	t.Helper()
	c := &Client{}
	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })
	d := tg.NewUpdateDispatcher()
	(&Listener{client: c}).registerHandlers(d)
	return d, &got
}

// reactionUpdate is somebody reacting to message 7 in the private chat
// with user 42, with the reaction flagged unread or not.
func reactionUpdate(unread bool) *tg.Updates {
	u := &tg.UpdateMessageReactions{Peer: &tg.PeerUser{UserID: 42}, MsgID: 7}
	u.Reactions.SetRecentReactions([]tg.MessagePeerReaction{{
		Unread:   unread,
		PeerID:   &tg.PeerUser{UserID: 43},
		Reaction: &tg.ReactionEmoji{Emoticon: "❤"},
	}})
	return &tg.Updates{Updates: []tg.UpdateClass{u}}
}

func publishedUnreadReactions(got []tea.Msg) []ChatUnreadReactionsMsg {
	var out []ChatUnreadReactionsMsg
	for _, m := range got {
		if r, ok := m.(ChatUnreadReactionsMsg); ok {
			out = append(out, r)
		}
	}
	return out
}

// A reaction the reader has not seen yet is what raises the heart on the
// phone, and the update says so per reaction. The chat has to hear about
// it, or the count the dialog gave it is the last it ever knows.
func TestAnUnreadReactionIsAnnounced(t *testing.T) {
	d, got := listening(t)

	if err := d.Handle(context.Background(), reactionUpdate(true)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	want := []ChatUnreadReactionsMsg{{ChatId: 42}}
	if r := publishedUnreadReactions(*got); len(r) != 1 || r[0] != want[0] {
		t.Fatalf("published %#v, want %#v", *got, want)
	}
}

// Most reaction updates are tallies on somebody else's message, or on the
// reader's own that they have already seen. Those change what is drawn,
// and still refetch, but give the chat nothing new to clear.
func TestAReadReactionIsNotAnnounced(t *testing.T) {
	d, got := listening(t)

	if err := d.Handle(context.Background(), reactionUpdate(false)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if r := publishedUnreadReactions(*got); len(r) != 0 {
		t.Fatalf("a reaction without the unread flag published %#v", r)
	}
	if len(*got) != 1 || (*got)[0] != (MessageEditedMsg{ChatId: 42, MessageId: 7}) {
		t.Fatalf("published %#v, want only the refetch", *got)
	}
}

// readReactionsInvoker stands in for the server: it knows the basic group
// 5 and the channel 9, answers messages.readReactions with readErr or with
// the offsets queued in offsets, and records what it was asked.
type readReactionsInvoker struct {
	readErr error
	offsets []int
	asked   []*tg.MessagesReadReactionsRequest
}

func (f *readReactionsInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	req, ok := input.(*tg.MessagesReadReactionsRequest)
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

// Clearing a chat's reactions tells the chat list at once, the way a read
// does: the server's own word on it is not something to wait for.
func TestReadReactionsAnnouncesTheClear(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a basic group": basicGroupID,
		"a channel":     channelChatID(9),
	} {
		t.Run(name, func(t *testing.T) {
			inv := &readReactionsInvoker{}
			c, got := viewClient(t, inv)

			if err := c.ReadReactions(chatID); err != nil {
				t.Fatalf("ReadReactions: %v", err)
			}

			if len(inv.asked) != 1 {
				t.Fatalf("asked the server %d times, want once", len(inv.asked))
			}
			if _, ok := inv.asked[0].GetTopMsgID(); ok {
				t.Error("the clear was scoped to a thread; it is for the whole chat")
			}
			want := ChatReactionsReadMsg{ChatId: chatID}
			if len(*got) != 1 || (*got)[0] != want {
				t.Errorf("published %#v, want %#v", *got, want)
			}
		})
	}
}

// The server clears a long history in batches, and a positive offset in
// its answer means it stopped partway and wants the call made again. Taking
// the first answer as the end left the older reactions unread.
func TestReadReactionsRepeatsUntilTheServerIsDone(t *testing.T) {
	inv := &readReactionsInvoker{offsets: []int{40, 10, 0}}
	c, got := viewClient(t, inv)

	if err := c.ReadReactions(basicGroupID); err != nil {
		t.Fatalf("ReadReactions: %v", err)
	}

	if len(inv.asked) != 3 {
		t.Fatalf("asked the server %d times, want 3: once per batch", len(inv.asked))
	}
	if len(*got) != 1 {
		t.Errorf("published %#v, want one announcement for the whole clear", *got)
	}
}

// A server that never says it is done must not be called in a tight loop
// until the timeout: that is the pattern Telegram answers with FLOOD_WAIT.
// The walk stops at the cap, without an error, and without claiming a
// clear the server never confirmed: the count stays, and the next open
// asks again.
func TestReadReactionsStopsAtTheCap(t *testing.T) {
	never := make([]int, 3*maxReadReactionsCalls)
	for i := range never {
		never[i] = 1
	}
	inv := &readReactionsInvoker{offsets: never}
	c, got := viewClient(t, inv)

	if err := c.ReadReactions(basicGroupID); err != nil {
		t.Fatalf("ReadReactions: %v, want a quiet stop", err)
	}

	if len(inv.asked) != maxReadReactionsCalls {
		t.Fatalf("asked the server %d times, want the cap of %d", len(inv.asked), maxReadReactionsCalls)
	}
	if len(*got) != 0 {
		t.Errorf("a clear the server never finished published %#v", *got)
	}
}

// A clear the server refused did not happen.
func TestReadReactionsAnnouncesNothingWhenTheClearFails(t *testing.T) {
	c, got := viewClient(t, &readReactionsInvoker{readErr: errors.New("no connection")})

	if err := c.ReadReactions(basicGroupID); err == nil {
		t.Fatal("ReadReactions reported success for a failed clear")
	}
	if len(*got) != 0 {
		t.Errorf("a failed clear published %#v", *got)
	}
}
