package telegram

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// basicGroupID is a basic group's TDLib chat ID: the plain ID, negated.
const basicGroupID = -5

// readHistoryInvoker stands in for the server: it knows the basic group 5
// and the channel 9, and answers both kinds of read with readErr or with
// success.
type readHistoryInvoker struct {
	readErr error
}

func (f readHistoryInvoker) Invoke(_ context.Context, input bin.Encoder, output bin.Decoder) error {
	switch input.(type) {
	case *tg.MessagesGetChatsRequest:
		output.(*tg.MessagesChatsBox).Chats = &tg.MessagesChats{
			Chats: []tg.ChatClass{&tg.Chat{ID: 5, Title: "g"}},
		}
		return nil
	case *tg.ChannelsGetChannelsRequest:
		output.(*tg.MessagesChatsBox).Chats = &tg.MessagesChats{
			Chats: []tg.ChatClass{&tg.Channel{ID: 9, AccessHash: 1, Title: "c"}},
		}
		return nil
	case *tg.MessagesReadHistoryRequest:
		return f.readErr
	case *tg.ChannelsReadHistoryRequest:
		if f.readErr != nil {
			return f.readErr
		}
		output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
		return nil
	default:
		return errors.New("unexpected request")
	}
}

// viewClient is a client that can mark the invoker's chats read against
// inv, and the messages it publishes.
func viewClient(t *testing.T, inv tg.Invoker) (*Client, *[]tea.Msg) {
	t.Helper()
	api := tg.NewClient(inv)
	c := &Client{api: api, peers: peers.Options{}.Build(api)}
	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })
	return c, &got
}

// Marking a chat read tells the chat list at once, rather than waiting for
// the server's receipt, which for this session's own read does not
// reliably come back. It is published from the client so every in-process
// caller gets it without having to remember.
func TestViewMessagesAnnouncesTheRead(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a basic group": basicGroupID,
		"a channel":     channelChatID(9),
	} {
		t.Run(name, func(t *testing.T) {
			c, got := viewClient(t, readHistoryInvoker{})

			if err := c.ViewMessages(chatID, []int64{3, 7}); err != nil {
				t.Fatalf("ViewMessages: %v", err)
			}

			if len(*got) != 1 {
				t.Fatalf("published %d messages, want 1: %#v", len(*got), *got)
			}
			want := ChatMarkedReadMsg{ChatId: chatID, MaxMessageId: 7}
			if (*got)[0] != want {
				t.Errorf("published %#v, want %#v", (*got)[0], want)
			}
		})
	}
}

// A read the server refused did not happen, and the badge has to say so.
func TestViewMessagesAnnouncesNothingWhenTheReadFails(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a basic group": basicGroupID,
		"a channel":     channelChatID(9),
	} {
		t.Run(name, func(t *testing.T) {
			c, got := viewClient(t, readHistoryInvoker{readErr: errors.New("no connection")})

			if err := c.ViewMessages(chatID, []int64{7}); err == nil {
				t.Fatal("ViewMessages reported success for a failed read")
			}
			if len(*got) != 0 {
				t.Errorf("a failed read published %#v", *got)
			}
		})
	}
}

// Nothing to mark is not a read.
func TestViewMessagesAnnouncesNothingWithoutAMessage(t *testing.T) {
	c, got := viewClient(t, readHistoryInvoker{})

	if err := c.ViewMessages(basicGroupID, nil); err != nil {
		t.Fatalf("ViewMessages: %v", err)
	}
	if len(*got) != 0 {
		t.Errorf("marking nothing read published %#v", *got)
	}
}
