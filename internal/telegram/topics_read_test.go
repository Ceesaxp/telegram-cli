package telegram

import (
	"context"
	"reflect"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// jobsTopicID is the topic every test here reads: an ordinary topic of the
// forum, not General, so that a call which quietly dropped the topic would
// be asking about the whole forum and show up as a different request.
const jobsTopicID int64 = 7

// topicReadInvoker stands in for the server for the reads that are NOT
// already served by a fake of their own: the two history calls, the read
// mark in both its forms, and the search. Everything else falls through to
// readHistoryInvoker, which is what knows the forum's channel.
//
// It records the request rather than the answer, because the request is the
// whole subject: a topic and its forum come back from the server looking the
// same, and what tells the two apart is which call was made about which peer.
type topicReadInvoker struct {
	history     []*tg.MessagesGetHistoryRequest
	replies     []*tg.MessagesGetRepliesRequest
	discussion  []*tg.MessagesReadDiscussionRequest
	channelRead []*tg.ChannelsReadHistoryRequest
	search      []*tg.MessagesSearchRequest

	// answer is what every fetch comes back with, empty unless a test
	// cares what it converted.
	answer tg.MessagesMessagesClass
}

func (f *topicReadInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	answer := f.answer
	if answer == nil {
		answer = &tg.MessagesMessages{}
	}
	switch req := input.(type) {
	case *tg.MessagesGetHistoryRequest:
		f.history = append(f.history, req)
		output.(*tg.MessagesMessagesBox).Messages = answer
		return nil
	case *tg.MessagesGetRepliesRequest:
		f.replies = append(f.replies, req)
		output.(*tg.MessagesMessagesBox).Messages = answer
		return nil
	case *tg.MessagesReadDiscussionRequest:
		f.discussion = append(f.discussion, req)
		output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
		return nil
	case *tg.ChannelsReadHistoryRequest:
		f.channelRead = append(f.channelRead, req)
		output.(*tg.BoolBox).Bool = &tg.BoolTrue{}
		return nil
	case *tg.MessagesSearchRequest:
		f.search = append(f.search, req)
		output.(*tg.MessagesMessagesBox).Messages = answer
		return nil
	default:
		return readHistoryInvoker{}.Invoke(ctx, input, output)
	}
}

// topicReadClient is a client that reads the forum through inv, with the
// messages it publishes captured: what a read announces is the other half of
// getting a topic right, because it is what clears the topic's row.
func topicReadClient(t *testing.T, inv tg.Invoker) (*Client, *[]tea.Msg) {
	t.Helper()
	api := tg.NewClient(inv)
	c := &Client{api: api, peers: peers.Options{}.Build(api), files: newFileRegistry()}
	var published []tea.Msg
	c.setMsgSink(func(m tea.Msg) { published = append(published, m) })
	return c, &published
}

// assertForumPeer holds a request to naming the forum's channel. A topic is
// not a peer, so this is the only peer any of these calls can name.
func assertForumPeer(t *testing.T, peer tg.InputPeerClass) {
	t.Helper()
	channel, ok := peer.(*tg.InputPeerChannel)
	if !ok {
		t.Fatalf("named %#v, want the forum's channel peer", peer)
	}
	if channel.ChannelID != forumChannelID {
		t.Errorf("named channel %d, want the forum %d", channel.ChannelID, forumChannelID)
	}
}

// A topic's messages are the forum's, and messages.getHistory answers with
// every topic's at once — the flat stream the reader opened a topic to get
// out of. The thread under the topic's root message is the topic.
func TestATopicsHistoryIsTheRepliesToItsRootMessage(t *testing.T) {
	inv := &topicReadInvoker{}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if _, err := c.GetChatHistory(topic, 0, 0, 50); err != nil {
		t.Fatalf("GetChatHistory: %v", err)
	}

	if len(inv.replies) != 1 || len(inv.history) != 0 {
		t.Fatalf("made %d replies and %d history calls, want one replies call",
			len(inv.replies), len(inv.history))
	}
	req := inv.replies[0]
	if int64(req.MsgID) != jobsTopicID {
		t.Errorf("asked for the replies to message %d, want the topic's root %d",
			req.MsgID, jobsTopicID)
	}
	assertForumPeer(t, req.Peer)
}

// General is a topic like any other here: its root is the forum's own first
// message and it exists, which is all messages.getReplies needs.
func TestGeneralsHistoryIsTheRepliesToTheForumsRootMessage(t *testing.T) {
	inv := &topicReadInvoker{}
	c, _ := topicReadClient(t, inv)
	general := c.topicChatID(forumChatID(), generalTopicID)

	if _, err := c.GetChatHistory(general, 0, 0, 50); err != nil {
		t.Fatalf("GetChatHistory: %v", err)
	}

	if len(inv.replies) != 1 {
		t.Fatalf("made %d replies calls, want one", len(inv.replies))
	}
	if int64(inv.replies[0].MsgID) != generalTopicID {
		t.Errorf("asked for the replies to message %d, want General's %d",
			inv.replies[0].MsgID, generalTopicID)
	}
}

// Almost every chat is not a topic, and one that is not must be fetched
// exactly as it was before there were topics at all.
func TestAnOrdinaryChatsHistoryIsStillItsHistory(t *testing.T) {
	inv := &topicReadInvoker{}
	c, _ := topicReadClient(t, inv)

	if _, err := c.GetChatHistory(forumChatID(), 0, 0, 50); err != nil {
		t.Fatalf("GetChatHistory: %v", err)
	}

	if len(inv.history) != 1 || len(inv.replies) != 0 {
		t.Fatalf("made %d history and %d replies calls, want one history call",
			len(inv.history), len(inv.replies))
	}
	assertForumPeer(t, inv.history[0].Peer)
}

// The thread pages a topic the way it pages any chat — the same offset, the
// same skip, the same clamp — so the two calls have to take them the same
// way or paging back in a topic would fetch the same page for ever.
func TestATopicsHistoryPagesTheWayAChatsDoes(t *testing.T) {
	inv := &topicReadInvoker{}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if _, err := c.GetChatHistory(topic, 412, 3, 0); err != nil {
		t.Fatalf("GetChatHistory: %v", err)
	}

	if len(inv.replies) != 1 {
		t.Fatalf("made %d replies calls, want one", len(inv.replies))
	}
	req := inv.replies[0]
	if req.OffsetID != 412 || req.AddOffset != 3 || req.Limit != 100 {
		t.Errorf("asked with offset_id %d, add_offset %d, limit %d; want 412, 3 and "+
			"the clamped 100", req.OffsetID, req.AddOffset, req.Limit)
	}
	if req.MaxID != 0 || req.MinID != 0 || req.Hash != 0 {
		t.Errorf("asked with max_id %d, min_id %d, hash %d; want none of the three",
			req.MaxID, req.MinID, req.Hash)
	}
}

// A topic's history has to come back as the same messages from the same
// call: everything above this package reads a page of history and knows
// nothing about which RPC fetched it.
func TestATopicsHistoryComesBackAsMessages(t *testing.T) {
	inv := &topicReadInvoker{answer: &tg.MessagesChannelMessages{Messages: []tg.MessageClass{
		&tg.Message{ID: 412, PeerID: &tg.PeerChannel{ChannelID: forumChannelID}, Message: "remote Go role"},
	}}}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	got, err := c.GetChatHistory(topic, 0, 0, 50)
	if err != nil {
		t.Fatalf("GetChatHistory: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("converted %d messages, want 1", len(got))
	}
	if got[0].ID != 412 {
		t.Errorf("history holds message %d, want 412", got[0].ID)
	}
}

// A topic's read pointer is its own — the forum's says nothing about any one
// topic — and Telegram takes it through the thread call. channels.readHistory
// would mark every topic in the forum read at once.
func TestReadingATopicMarksItsDiscussionRead(t *testing.T) {
	inv := &topicReadInvoker{}
	c, published := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if err := c.ViewMessages(topic, []int64{400, 412}); err != nil {
		t.Fatalf("ViewMessages: %v", err)
	}

	if len(inv.discussion) != 1 || len(inv.channelRead) != 0 {
		t.Fatalf("made %d discussion and %d channel reads, want one discussion read",
			len(inv.discussion), len(inv.channelRead))
	}
	req := inv.discussion[0]
	if int64(req.MsgID) != jobsTopicID {
		t.Errorf("read the thread of message %d, want the topic's root %d",
			req.MsgID, jobsTopicID)
	}
	if req.ReadMaxID != 412 {
		t.Errorf("read up to message %d, want the newest of the two, 412", req.ReadMaxID)
	}
	assertForumPeer(t, req.Peer)

	// Under the ID the caller asked with: the chat list keys the row on it,
	// and announcing the forum would clear the wrong badge.
	want := ChatMarkedReadMsg{ChatId: topic, MaxMessageId: 412}
	if len(*published) != 1 || (*published)[0] != want {
		t.Errorf("published %#v, want %#v", *published, want)
	}
}

// And a chat that is not a topic keeps the read it always had.
func TestAnOrdinaryChatsReadMarkIsNotADiscussion(t *testing.T) {
	inv := &topicReadInvoker{}
	c, published := topicReadClient(t, inv)

	if err := c.ViewMessages(forumChatID(), []int64{412}); err != nil {
		t.Fatalf("ViewMessages: %v", err)
	}

	if len(inv.channelRead) != 1 || len(inv.discussion) != 0 {
		t.Fatalf("made %d channel and %d discussion reads, want one channel read",
			len(inv.channelRead), len(inv.discussion))
	}
	want := ChatMarkedReadMsg{ChatId: forumChatID(), MaxMessageId: 412}
	if len(*published) != 1 || (*published)[0] != want {
		t.Errorf("published %#v, want %#v", *published, want)
	}
}

// The reactions counter the reader is clearing is the topic's row, so the
// clear is scoped to the topic. Unscoped it would take every other topic's
// unread reactions in the forum with it.
func TestClearingATopicsReactionsNamesTheTopic(t *testing.T) {
	inv := &readReactionsInvoker{}
	c, published := viewClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if err := c.ReadReactions(topic); err != nil {
		t.Fatalf("ReadReactions: %v", err)
	}

	if len(inv.asked) != 1 {
		t.Fatalf("asked the server %d times, want once", len(inv.asked))
	}
	assertTopMsgID(t, inv.asked[0].GetTopMsgID)
	assertForumPeer(t, inv.asked[0].Peer)

	want := ChatReactionsReadMsg{ChatId: topic}
	if len(*published) != 1 || (*published)[0] != want {
		t.Errorf("published %#v, want %#v", *published, want)
	}
}

// assertTopMsgID holds a flagged top_msg_id to the topic every test here
// reads. It takes the getter rather than its results because a two-result
// call cannot be passed alongside t, and a pair of locals per call site
// would bury the assertion.
func assertTopMsgID(t *testing.T, get func() (int, bool)) {
	t.Helper()
	top, ok := get()
	if !ok {
		t.Errorf("the call named no topic at all, want topic %d", jobsTopicID)
		return
	}
	if int64(top) != jobsTopicID {
		t.Errorf("the call named topic %d, want %d", top, jobsTopicID)
	}
}

// The @ on a topic's row counts that topic's mentions, so clearing them all
// is scoped to it, for the reason the reactions clear is.
func TestClearingAllOfATopicsMentionsNamesTheTopic(t *testing.T) {
	inv := &readAllMentionsInvoker{}
	c, published := viewClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	done, err := c.ReadAllMentions(topic)
	if err != nil {
		t.Fatalf("ReadAllMentions: %v", err)
	}
	if !done {
		t.Error("ReadAllMentions did not report a clear the server finished as done")
	}

	if len(inv.asked) != 1 {
		t.Fatalf("asked the server %d times, want once", len(inv.asked))
	}
	assertTopMsgID(t, inv.asked[0].GetTopMsgID)
	assertForumPeer(t, inv.asked[0].Peer)

	want := ChatMentionsReadMsg{ChatId: topic, All: true}
	if len(*published) != 1 || !reflect.DeepEqual((*published)[0], want) {
		t.Errorf("published %#v, want %#v", *published, want)
	}
}

// A jump to the next mention inside a topic walks that topic's mentions.
// The whole forum's would jump the reader out of the topic they are in.
func TestListingATopicsUnreadMentionsNamesTheTopic(t *testing.T) {
	inv := &unreadMentionsInvoker{answer: &tg.MessagesMessages{Messages: mentionsAt(400, 412)}}
	c, _ := viewClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	got, err := c.UnreadMentions(topic, 20)
	if err != nil {
		t.Fatalf("UnreadMentions: %v", err)
	}
	if want := []int64{400, 412}; !slices.Equal(got, want) {
		t.Errorf("UnreadMentions = %v, want %v", got, want)
	}

	if len(inv.asked) != 1 {
		t.Fatalf("asked the server %d times, want once", len(inv.asked))
	}
	assertTopMsgID(t, inv.asked[0].GetTopMsgID)
	assertForumPeer(t, inv.asked[0].Peer)
}

// A topic's messages ARE the forum's channel messages, so clearing named
// mentions in one takes the call that names the channel. A synthetic chat ID
// answers no to IsChannel, so the branch it used to take was the peerless
// one — which clears by the account's own numbering, and so cleared whatever
// messages happened to hold those IDs elsewhere.
func TestClearingATopicsMentionsNamesTheForumsChannel(t *testing.T) {
	inv := &readContentsInvoker{}
	c, published := viewClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if err := c.ReadMentions(topic, []int64{400, 412}); err != nil {
		t.Fatalf("ReadMentions: %v", err)
	}

	if len(inv.channels) != 1 || len(inv.messages) != 0 {
		t.Fatalf("made %d channel and %d messages calls, want one channel call",
			len(inv.channels), len(inv.messages))
	}
	req := inv.channels[0]
	channel, ok := req.Channel.(*tg.InputChannel)
	if !ok || channel.ChannelID != forumChannelID {
		t.Errorf("cleared in %#v, want the forum's channel %d", req.Channel, forumChannelID)
	}
	if !slices.Equal(req.ID, []int{400, 412}) {
		t.Errorf("cleared %v, want [400 412]", req.ID)
	}

	want := ChatMentionsReadMsg{ChatId: topic, MessageIds: []int64{400, 412}}
	if len(*published) != 1 || !reflect.DeepEqual((*published)[0], want) {
		t.Errorf("published %#v, want %#v", *published, want)
	}
}

// Searching from inside a topic searches the topic: the reader is looking at
// one conversation, and a hit in a different topic of the same forum opens
// onto a thread they are not in.
func TestSearchingInsideATopicSearchesThatTopic(t *testing.T) {
	inv := &topicReadInvoker{}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if _, err := c.SearchChatMessages(topic, "go", 0, 50); err != nil {
		t.Fatalf("SearchChatMessages: %v", err)
	}

	if len(inv.search) != 1 {
		t.Fatalf("made %d search calls, want one", len(inv.search))
	}
	assertTopMsgID(t, inv.search[0].GetTopMsgID)
	assertForumPeer(t, inv.search[0].Peer)
}

// And a search of an ordinary chat is the whole chat, as it has always been.
func TestSearchingAnOrdinaryChatNamesNoTopic(t *testing.T) {
	inv := &topicReadInvoker{}
	c, _ := topicReadClient(t, inv)

	if _, err := c.SearchChatMessages(forumChatID(), "go", 0, 50); err != nil {
		t.Fatalf("SearchChatMessages: %v", err)
	}

	if len(inv.search) != 1 {
		t.Fatalf("made %d search calls, want one", len(inv.search))
	}
	if top, ok := inv.search[0].GetTopMsgID(); ok {
		t.Errorf("the search was scoped to topic %d; it is for the whole chat", top)
	}
}

// topicReads are the reads a topic takes, each making exactly one request
// and handing back what went out along with the topic's own chat ID.
var topicReads = map[string]func(t *testing.T) (bin.Encoder, int64){
	"fetching its history": func(t *testing.T) (bin.Encoder, int64) {
		inv := &topicReadInvoker{}
		c, _ := topicReadClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if _, err := c.GetChatHistory(topic, 412, 3, 50); err != nil {
			t.Fatalf("GetChatHistory: %v", err)
		}
		return onlyRequest(t, inv.replies), topic
	},
	"marking it read": func(t *testing.T) (bin.Encoder, int64) {
		inv := &topicReadInvoker{}
		c, _ := topicReadClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if err := c.ViewMessages(topic, []int64{412}); err != nil {
			t.Fatalf("ViewMessages: %v", err)
		}
		return onlyRequest(t, inv.discussion), topic
	},
	"searching it": func(t *testing.T) (bin.Encoder, int64) {
		inv := &topicReadInvoker{}
		c, _ := topicReadClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if _, err := c.SearchChatMessages(topic, "go", 0, 50); err != nil {
			t.Fatalf("SearchChatMessages: %v", err)
		}
		return onlyRequest(t, inv.search), topic
	},
	"clearing its reactions": func(t *testing.T) (bin.Encoder, int64) {
		inv := &readReactionsInvoker{}
		c, _ := viewClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if err := c.ReadReactions(topic); err != nil {
			t.Fatalf("ReadReactions: %v", err)
		}
		return onlyRequest(t, inv.asked), topic
	},
	"clearing all its mentions": func(t *testing.T) (bin.Encoder, int64) {
		inv := &readAllMentionsInvoker{}
		c, _ := viewClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if _, err := c.ReadAllMentions(topic); err != nil {
			t.Fatalf("ReadAllMentions: %v", err)
		}
		return onlyRequest(t, inv.asked), topic
	},
	"listing its unread mentions": func(t *testing.T) (bin.Encoder, int64) {
		inv := &unreadMentionsInvoker{answer: &tg.MessagesMessages{}}
		c, _ := viewClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if _, err := c.UnreadMentions(topic, 20); err != nil {
			t.Fatalf("UnreadMentions: %v", err)
		}
		return onlyRequest(t, inv.asked), topic
	},
	"clearing some of its mentions": func(t *testing.T) (bin.Encoder, int64) {
		inv := &readContentsInvoker{}
		c, _ := viewClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if err := c.ReadMentions(topic, []int64{412}); err != nil {
			t.Fatalf("ReadMentions: %v", err)
		}
		return onlyRequest(t, inv.channels), topic
	},
}

// onlyRequest is the one request the fake was asked, and a failure naming
// how many there were instead.
func onlyRequest[T bin.Encoder](t *testing.T, sent []T) bin.Encoder {
	t.Helper()
	if len(sent) != 1 {
		t.Fatalf("made %d requests of the kind under test, want exactly one", len(sent))
	}
	return sent[0]
}

// A topic's chat ID names nothing Telegram has ever heard of, and every one
// of these calls resolves its peer from the forum instead. The check is on
// the encoded request rather than on one field, so a topic ID smuggled into
// any part of any of them fails here.
func TestNoReadOfATopicPutsItsSyntheticIDOnTheWire(t *testing.T) {
	for name, read := range topicReads {
		t.Run(name, func(t *testing.T) {
			sent, topic := read(t)

			var b bin.Buffer
			if err := sent.Encode(&b); err != nil {
				t.Fatalf("encoding the request: %v", err)
			}
			if containsInt64(b.Buf, topic) {
				t.Errorf("the synthetic chat ID %d is somewhere in the request the "+
					"client encoded — a topic's ID must never reach the wire", topic)
			}
		})
	}
}
