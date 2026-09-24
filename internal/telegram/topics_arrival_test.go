package telegram

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/tg"
)

// forumListening is a client whose topic registry the test can prime, the
// dispatcher the listener's handlers are registered on, and the messages
// they publish.
//
// No invoker: everything this file is about happens between an update and
// the sink, with nothing asked of the server in between.
func forumListening(t *testing.T) (*Client, tg.UpdateDispatcher, *[]tea.Msg) {
	t.Helper()
	c := &Client{}
	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })
	d := tg.NewUpdateDispatcher()
	(&Listener{client: c}).registerHandlers(d)
	return c, d, &got
}

// arrivals are the messages one publish announced, in order.
//
// Each arrival is TWO published messages — the thread's and the chat
// list's row — and this insists on both, because a message announced to
// only one of them lands in the open thread while the row goes on showing
// the previous message, or puts a row up for a message the thread never
// showed. They are checked as a pair so the order of the two cannot drift
// either.
func arrivals(t *testing.T, published []tea.Msg) []*Message {
	t.Helper()

	var out []*Message
	for i := 0; i < len(published); i += 2 {
		arrived, ok := published[i].(NewMessageMsg)
		if !ok {
			t.Fatalf("published[%d] = %#v, want a new message", i, published[i])
		}
		if arrived.Message == nil {
			t.Fatalf("published[%d] announced a new message with nothing in it", i)
		}
		if i+1 >= len(published) {
			t.Fatalf("message %d arrived in chat %d and the chat list was never told",
				arrived.Message.ID, arrived.Message.ChatID)
		}
		row, ok := published[i+1].(ChatLastMessageMsg)
		if !ok {
			t.Fatalf("published[%d] = %#v, want the chat-list row for chat %d",
				i+1, published[i+1], arrived.Message.ChatID)
		}
		if row.ChatId != arrived.Message.ChatID || row.LastMessage != arrived.Message {
			t.Fatalf("the row went to chat %d carrying %#v, want chat %d carrying the "+
				"message just announced", row.ChatId, row.LastMessage, arrived.Message.ChatID)
		}
		out = append(out, arrived.Message)
	}
	return out
}

// arrivedIn is the chat IDs the arrivals belong to, for a failure message
// that says where a message went rather than printing the whole message.
func arrivedIn(messages []*Message) []int64 {
	out := make([]int64, len(messages))
	for i, m := range messages {
		out[i] = m.ChatID
	}
	return out
}

// A message arriving in a forum belongs to one of its topics, and the
// reader is looking at that topic rather than at the forum. Published only
// under the forum, it never appears: the topic's thread is keyed by the
// topic's own chat ID, and so is its row in the topic list.
//
// The forum's own pair stays, because the forum is still a chat — its row
// in the chat list and its flat stream both want the message too.
func TestAMessageInAListedForumIsAlsoPublishedUnderItsTopic(t *testing.T) {
	c, _, published := forumListening(t)
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), 5)

	c.publishNewMessage(&Message{ID: 412, ChatID: forumChatID(), TopicID: 5})

	got := arrivals(t, *published)
	if want := []int64{forumChatID(), topic}; !int64sEqual(arrivedIn(got), want) {
		t.Fatalf("the message arrived in chats %v, want the forum then its topic %v",
			arrivedIn(got), want)
	}
	if got[1].TopicID != 5 {
		t.Errorf("the topic's copy names topic %d, want 5", got[1].TopicID)
	}
}

// In a forum, a message that names no topic is in General. Topic 0 is not
// a topic at all — it is what a message outside a forum reports — so the
// copy has to be filed under topic 1 or General's thread never fills.
func TestAMessageWithNoTopicInAListedForumGoesToGeneral(t *testing.T) {
	c, _, published := forumListening(t)
	c.topics.markListed(forumChatID())
	general := c.topicChatID(forumChatID(), generalTopicID)

	c.publishNewMessage(&Message{ID: 412, ChatID: forumChatID()})

	got := arrivals(t, *published)
	if want := []int64{forumChatID(), general}; !int64sEqual(arrivedIn(got), want) {
		t.Fatalf("the message arrived in chats %v, want the forum then its General %v",
			arrivedIn(got), want)
	}
	if got[1].TopicID != generalTopicID {
		t.Errorf("General's copy names topic %d, want %d", got[1].TopicID, generalTopicID)
	}
}

// Almost no chat is a forum, and a group that is not one has no topics to
// route to. A second copy there would be a message filed under an ID
// nothing has ever drawn, and the thread would show it twice.
func TestAMessageInAnOrdinaryGroupIsPublishedOnce(t *testing.T) {
	c, _, published := forumListening(t)

	c.publishNewMessage(&Message{ID: 412, ChatID: basicGroupID})

	got := arrivals(t, *published)
	if want := []int64{basicGroupID}; !int64sEqual(arrivedIn(got), want) {
		t.Fatalf("the message arrived in chats %v, want only the group %v",
			arrivedIn(got), want)
	}
}

// A forum nobody has opened has no topic list, so it has no topic rows and
// nothing keyed by its topics. Minting an ID for one on every arriving
// message would be allocation with no reader.
func TestAMessageInAForumNobodyHasListedIsPublishedOnce(t *testing.T) {
	c, _, published := forumListening(t)

	c.publishNewMessage(&Message{ID: 412, ChatID: forumChatID(), TopicID: 5})

	got := arrivals(t, *published)
	if want := []int64{forumChatID()}; !int64sEqual(arrivedIn(got), want) {
		t.Fatalf("the message arrived in chats %v, want only the forum %v",
			arrivedIn(got), want)
	}
}

// The two copies differ in exactly one field, and the original is the one
// the forum's own row and flat stream hold. Filing the topic's copy by
// rewriting the message in place would move the message out of the forum
// as a side effect of announcing it to the topic.
func TestTheTopicsCopyDoesNotMutateTheArrivingMessage(t *testing.T) {
	c, _, published := forumListening(t)
	c.topics.markListed(forumChatID())
	arrived := &Message{ID: 412, ChatID: forumChatID(), TopicID: 5}

	c.publishNewMessage(arrived)

	if arrived.ChatID != forumChatID() {
		t.Errorf("the arriving message now belongs to chat %d, want the forum %d",
			arrived.ChatID, forumChatID())
	}
	if arrived.TopicID != 5 {
		t.Errorf("the arriving message now names topic %d, want 5", arrived.TopicID)
	}
	if got := arrivals(t, *published); got[0] != arrived {
		t.Error("the forum's own arrival is not the message that arrived")
	}
}

// Reading a topic on the phone has to clear its row here, and the server
// echoes the read as updateReadChannelDiscussionInbox — for this client's
// own read as well as another device's. Keyed by the forum, it would clear
// the forum's badge and leave every topic's untouched.
func TestATopicReadElsewhereLandsOnTheTopic(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), 5)

	err := d.Handle(context.Background(), &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateReadChannelDiscussionInbox{
			ChannelID: forumChannelID, TopMsgID: 5, ReadMaxID: 412,
		},
	}})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	want := ChatMarkedReadMsg{ChatId: topic, MaxMessageId: 412}
	if len(*published) != 1 || (*published)[0] != want {
		t.Fatalf("published %#v, want %#v", *published, want)
	}
}

// And the outbox half, which is what moves the sent ticks in a topic the
// reader is looking at when the other side reads it.
func TestATopicsOutboxReadLandsOnTheTopic(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), 5)

	err := d.Handle(context.Background(), &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateReadChannelDiscussionOutbox{
			ChannelID: forumChannelID, TopMsgID: 5, ReadMaxID: 412,
		},
	}})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	want := ChatReadOutboxMsg{ChatId: topic, LastReadOutboxMessageId: 412}
	if len(*published) != 1 || (*published)[0] != want {
		t.Fatalf("published %#v, want %#v", *published, want)
	}
}

// The same update carries a channel post's comment thread, which is not a
// topic and has no row anywhere. The registry is what tells the two apart:
// a forum this session listed has topic rows, and nothing else does.
func TestADiscussionReadOutsideAListedForumPublishesNothing(t *testing.T) {
	_, d, published := forumListening(t)

	err := d.Handle(context.Background(), &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateReadChannelDiscussionInbox{
			ChannelID: forumChannelID, TopMsgID: 5, ReadMaxID: 412,
		},
		&tg.UpdateReadChannelDiscussionOutbox{
			ChannelID: forumChannelID, TopMsgID: 5, ReadMaxID: 412,
		},
	}})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(*published) != 0 {
		t.Fatalf("published %#v for a thread that is not a topic of a listed forum",
			*published)
	}
}

// int64sEqual compares two chat-ID lists; the failures above print both.
func int64sEqual(got, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
