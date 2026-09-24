package telegram

import (
	"context"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// topicFetchInvoker stands in for the server for the calls that fetch,
// delete or search a chat's messages, and records which of each pair of
// RPCs it was asked.
//
// Which one is the whole subject here. A topic's messages are the forum's
// channel messages, and the peerless messages.getMessages /
// messages.deleteMessages act on the ACCOUNT'S own numbering — so taking
// the wrong branch is not an error the caller sees, it is the wrong
// messages, quietly.
type topicFetchInvoker struct {
	channelFetches []*tg.ChannelsGetMessagesRequest
	plainFetches   []*tg.MessagesGetMessagesRequest
	channelDeletes []*tg.ChannelsDeleteMessagesRequest
	plainDeletes   []*tg.MessagesDeleteMessagesRequest
	searches       []*tg.MessagesSearchRequest

	// answer is what a fetch comes back with. Empty for the tests that are
	// only about which RPC was asked; set by the ones that care what the
	// answer is labelled with.
	answer []tg.MessageClass
}

func (f *topicFetchInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	switch req := input.(type) {
	case *tg.ChannelsGetMessagesRequest:
		f.channelFetches = append(f.channelFetches, req)
		output.(*tg.MessagesMessagesBox).Messages = &tg.MessagesMessages{Messages: f.answer}
		return nil
	case *tg.MessagesGetMessagesRequest:
		f.plainFetches = append(f.plainFetches, req)
		output.(*tg.MessagesMessagesBox).Messages = &tg.MessagesMessages{}
		return nil
	case *tg.ChannelsDeleteMessagesRequest:
		f.channelDeletes = append(f.channelDeletes, req)
		return nil
	case *tg.MessagesDeleteMessagesRequest:
		f.plainDeletes = append(f.plainDeletes, req)
		return nil
	case *tg.MessagesSearchRequest:
		f.searches = append(f.searches, req)
		output.(*tg.MessagesMessagesBox).Messages = &tg.MessagesMessages{}
		return nil
	default:
		return readHistoryInvoker{}.Invoke(ctx, input, output)
	}
}

// topicFetchClient is a client asking inv, with a forum whose topics have
// been listed — which is what makes the topic's chat ID split back into
// the forum rather than come back synthetic.
func topicFetchClient(t *testing.T) (*Client, *topicFetchInvoker) {
	t.Helper()
	inv := &topicFetchInvoker{}
	api := tg.NewClient(inv)
	c := &Client{api: api, peers: peers.Options{}.Build(api), files: newFileRegistry()}
	c.topics.markListed(forumChatID())
	return c, inv
}

// onlyChannelFetch is the one channels.getMessages the client made, and a
// failure naming the peerless call if it took that branch instead.
func onlyChannelFetch(t *testing.T, inv *topicFetchInvoker) *tg.ChannelsGetMessagesRequest {
	t.Helper()
	if len(inv.plainFetches) != 0 {
		t.Fatalf("asked messages.getMessages %d times — that call names no chat at "+
			"all, so it fetched by the account's own numbering",
			len(inv.plainFetches))
	}
	if len(inv.channelFetches) != 1 {
		t.Fatalf("made %d channels.getMessages requests, want 1", len(inv.channelFetches))
	}
	return inv.channelFetches[0]
}

// assertNamesTheForum holds a channel request to the forum's own channel.
func assertNamesTheForum(t *testing.T, channel tg.InputChannelClass) {
	t.Helper()
	input, ok := channel.(*tg.InputChannel)
	if !ok {
		t.Fatalf("named %#v, want the forum's channel", channel)
	}
	if input.ChannelID != forumChannelID {
		t.Errorf("named channel %d, want the forum %d", input.ChannelID, forumChannelID)
	}
}

// A topic's messages ARE the forum's channel messages, and a synthetic
// chat ID is not a channel — so unsplit, the fetch takes the peerless
// branch and asks for messages of those IDs in the account's own
// numbering, which is somebody else's conversation.
func TestGetMessagesInATopicAsksTheForumsChannel(t *testing.T) {
	c, inv := topicFetchClient(t)
	topic := c.topicChatID(forumChatID(), 5)

	if _, err := c.GetMessages(topic, []int64{412}); err != nil {
		t.Fatalf("GetMessages: %v", err)
	}

	got := onlyChannelFetch(t, inv)
	assertNamesTheForum(t, got.Channel)
	if len(got.ID) != 1 || got.ID[0].(*tg.InputMessageID).ID != 412 {
		t.Errorf("asked for %#v, want message 412", got.ID)
	}
}

// A message fetched for a topic belongs to the topic, like one fetched as
// a page of its history and one that arrived live. The refetch after an
// edit or a reaction goes through here and writes what it gets back into
// the topic's store, so a message that disagreed would flip chats the
// moment somebody reacted to it.
func TestAMessageFetchedForATopicBelongsToTheTopic(t *testing.T) {
	c, inv := topicFetchClient(t)
	topic := c.topicChatID(forumChatID(), 5)
	inv.answer = []tg.MessageClass{&tg.Message{
		ID:      412,
		PeerID:  &tg.PeerChannel{ChannelID: forumChannelID},
		FromID:  &tg.PeerUser{UserID: 3},
		Message: "remote Go role",
	}}

	msgs, err := c.GetMessages(topic, []int64{412})
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].ChatID != topic || msgs[0].TopicID != 5 {
		t.Errorf("message is in chat %d topic %d, want chat %d topic 5",
			msgs[0].ChatID, msgs[0].TopicID, topic)
	}
}

// And a chat that is not a topic keeps the branch it had: a channel by its
// channel, everything else by the peerless call.
func TestGetMessagesInAnOrdinaryChatIsUnchanged(t *testing.T) {
	t.Run("a channel", func(t *testing.T) {
		c, inv := topicFetchClient(t)

		if _, err := c.GetMessages(forumChatID(), []int64{412}); err != nil {
			t.Fatalf("GetMessages: %v", err)
		}

		assertNamesTheForum(t, onlyChannelFetch(t, inv).Channel)
	})

	t.Run("a basic group", func(t *testing.T) {
		c, inv := topicFetchClient(t)

		if _, err := c.GetMessages(basicGroupID, []int64{412}); err != nil {
			t.Fatalf("GetMessages: %v", err)
		}

		if len(inv.channelFetches) != 0 {
			t.Fatalf("asked channels.getMessages %d times for a basic group",
				len(inv.channelFetches))
		}
		if len(inv.plainFetches) != 1 {
			t.Fatalf("made %d messages.getMessages requests, want 1", len(inv.plainFetches))
		}
	})
}

// GetMessage is GetMessages with a list of one, so it inherits the split —
// and so does every reader that goes through it.
func TestGetMessageInATopicAsksTheForumsChannel(t *testing.T) {
	c, inv := topicFetchClient(t)
	topic := c.topicChatID(forumChatID(), 5)

	// The server answers with nothing, so the message is "not found"; the
	// request is what this is about.
	_, _ = c.GetMessage(topic, 412)

	assertNamesTheForum(t, onlyChannelFetch(t, inv).Channel)
}

// Re-registering a file's message when the bounded registry has dropped it
// is the same fetch, and a topic's file would otherwise be re-registered
// from the wrong messages entirely.
func TestDownloadingATopicsFileRefetchesFromTheForumsChannel(t *testing.T) {
	c, inv := topicFetchClient(t)
	topic := c.topicChatID(forumChatID(), 5)

	// The key is unknown, so the download re-registers it from the message
	// and then fails because the fetch brought nothing back.
	_, _ = c.DownloadMessageFile(topic, 412, "photo:412")

	assertNamesTheForum(t, onlyChannelFetch(t, inv).Channel)
}

// The same hole, in the call that removes messages rather than reads them:
// messages.deleteMessages names no chat, so an unsplit topic would delete
// by the account's own numbering.
func TestDeleteMessagesInATopicAsksTheForumsChannel(t *testing.T) {
	c, inv := topicFetchClient(t)
	topic := c.topicChatID(forumChatID(), 5)

	if err := c.DeleteMessages(topic, []int64{412}, true); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	if len(inv.plainDeletes) != 0 {
		t.Fatalf("asked messages.deleteMessages %d times — that call names no chat, "+
			"so it deleted by the account's own numbering", len(inv.plainDeletes))
	}
	if len(inv.channelDeletes) != 1 {
		t.Fatalf("made %d channels.deleteMessages requests, want 1", len(inv.channelDeletes))
	}
	assertNamesTheForum(t, inv.channelDeletes[0].Channel)
	if len(inv.channelDeletes[0].ID) != 1 || inv.channelDeletes[0].ID[0] != 412 {
		t.Errorf("deleted %v, want message 412", inv.channelDeletes[0].ID)
	}
}

// And a chat that is not a topic keeps its branch here too.
func TestDeleteMessagesInAnOrdinaryChatIsUnchanged(t *testing.T) {
	t.Run("a channel", func(t *testing.T) {
		c, inv := topicFetchClient(t)

		if err := c.DeleteMessages(forumChatID(), []int64{412}, true); err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		if len(inv.channelDeletes) != 1 {
			t.Fatalf("made %d channels.deleteMessages requests, want 1",
				len(inv.channelDeletes))
		}
		assertNamesTheForum(t, inv.channelDeletes[0].Channel)
	})

	t.Run("a basic group", func(t *testing.T) {
		c, inv := topicFetchClient(t)

		if err := c.DeleteMessages(basicGroupID, []int64{412}, true); err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		if len(inv.channelDeletes) != 0 {
			t.Fatalf("asked channels.deleteMessages %d times for a basic group",
				len(inv.channelDeletes))
		}
		if len(inv.plainDeletes) != 1 || !inv.plainDeletes[0].Revoke {
			t.Fatalf("made %d messages.deleteMessages requests, want 1 with revoke set",
				len(inv.plainDeletes))
		}
	})
}

// The media rail asks for a chat's files, photos and pinned messages, and
// in a topic that means the topic's — the forum's peer, with the topic as
// the thread to search inside. Unsplit, the rail's call stopped at
// inputPeer's refusal and the rail stayed empty.
func TestSearchChatMediaInATopicNamesTheForumAndTheTopic(t *testing.T) {
	c, inv := topicFetchClient(t)
	topic := c.topicChatID(forumChatID(), 5)

	if _, err := c.SearchChatMedia(topic, MediaFilterFiles, 20); err != nil {
		t.Fatalf("SearchChatMedia: %v", err)
	}

	if len(inv.searches) != 1 {
		t.Fatalf("made %d searches, want 1", len(inv.searches))
	}
	got := inv.searches[0]
	peer, ok := got.Peer.(*tg.InputPeerChannel)
	if !ok || peer.ChannelID != forumChannelID {
		t.Fatalf("searched %#v, want the forum's channel peer", got.Peer)
	}
	if top, ok := got.GetTopMsgID(); !ok || top != 5 {
		t.Errorf("searched thread %d (set: %v), want topic 5", top, ok)
	}
}

// A chat that is not a topic has no thread to narrow the search to, and a
// top_msg_id of zero is not "no thread" — it is a thread nothing is in.
func TestSearchChatMediaInAnOrdinaryChatNamesNoTopic(t *testing.T) {
	c, inv := topicFetchClient(t)

	if _, err := c.SearchChatMedia(forumChatID(), MediaFilterFiles, 20); err != nil {
		t.Fatalf("SearchChatMedia: %v", err)
	}

	if len(inv.searches) != 1 {
		t.Fatalf("made %d searches, want 1", len(inv.searches))
	}
	if top, ok := inv.searches[0].GetTopMsgID(); ok {
		t.Errorf("searched thread %d, want no top_msg_id at all", top)
	}
}
