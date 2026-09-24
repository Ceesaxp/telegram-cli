package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// forumChatID is the forum every test here asks about — the same channel
// the send tests use, named as this client names chats.
func forumChatID() int64 { return channelChatID(forumChannelID) }

// topicListInvoker stands in for the server's topic list. It knows the channel
// behind forumChatID, serves pages in the order given, and records every
// messages.getForumTopics it was asked — which is the half of this that
// matters, because paging is a statement about the NEXT request.
//
// Once pages runs out it serves endless, for the tests that have to prove
// the loop stops rather than that it ran out of canned answers; with no
// endless page it answers an empty one.
type topicListInvoker struct {
	pages   []*tg.MessagesForumTopics
	endless *tg.MessagesForumTopics
	asked   []*tg.MessagesGetForumTopicsRequest

	// calls counts EVERY request, of any kind, for the tests whose subject
	// is that a call went nowhere near the network.
	calls int
}

func (f *topicListInvoker) Invoke(_ context.Context, input bin.Encoder, output bin.Decoder) error {
	f.calls++
	switch req := input.(type) {
	case *tg.ChannelsGetChannelsRequest:
		output.(*tg.MessagesChatsBox).Chats = &tg.MessagesChats{
			Chats: []tg.ChatClass{&tg.Channel{
				ID: forumChannelID, AccessHash: 1, Title: "Go Serbia", Forum: true,
			}},
		}
		return nil
	case *tg.MessagesGetForumTopicsRequest:
		// A COPY: the real invoker serializes the request before returning,
		// so the caller is free to advance the same struct to the next
		// page, and recording the pointer would leave every page of this
		// history showing the last page's offsets.
		asked := *req
		f.asked = append(f.asked, &asked)
		page := f.endless
		if len(f.pages) > 0 {
			page, f.pages = f.pages[0], f.pages[1:]
		}
		if page == nil {
			page = &tg.MessagesForumTopics{}
		}
		*output.(*tg.MessagesForumTopics) = *page
		return nil
	default:
		return errors.New("unexpected request")
	}
}

// topicListClient is a client that asks inv for topic lists.
func topicListClient(inv tg.Invoker) *Client {
	api := tg.NewClient(inv)
	return &Client{api: api, peers: peers.Options{}.Build(api), files: newFileRegistry()}
}

// jobsTopic is an ordinary topic of the forum, with every counter set to a
// different number so a field copied into its neighbour shows up.
func jobsTopic() *tg.ForumTopic {
	return &tg.ForumTopic{
		ID:                   7,
		Date:                 1700000000,
		Title:                "Jobs",
		IconColor:            0x6FB9F0,
		TopMessage:           412,
		ReadInboxMaxID:       400,
		UnreadCount:          12,
		UnreadMentionsCount:  2,
		UnreadReactionsCount: 3,
		Closed:               true,
		My:                   true,
	}
}

// The listing is the only place a topic's title, its counters and its read
// pointer come from — no update carries any of them — so a field lost in
// the conversion is a field the topic list never shows and never recovers.
func TestForumTopicsConvertsTheTopicsItWasSent(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{{
		Count:  1,
		Topics: []tg.ForumTopicClass{jobsTopic()},
	}}}
	c := topicListClient(inv)

	got, err := c.ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("listed %d topics, want 1", len(got))
	}
	want := *topicFromTG(forumChatID(), jobsTopic())
	want.TopicChatID = got[0].TopicChatID
	if *got[0] != want {
		t.Errorf("ForumTopics =\n%+v\nwant\n%+v", *got[0], want)
	}
}

// The server already returns pinned topics first, which is the order the
// topic list wants, so the client keeps it. Re-sorting here would only give
// the reader a second opinion about an order the phone has already decided.
func TestForumTopicsKeepsTheServersOrder(t *testing.T) {
	pinned := jobsTopic()
	pinned.ID, pinned.Title, pinned.Pinned = 3, "Announcements", true
	general := &tg.ForumTopic{ID: 1, Title: "General", TopMessage: 2}

	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{{
		Count:  3,
		Topics: []tg.ForumTopicClass{pinned, general, jobsTopic()},
	}}}

	got, err := topicListClient(inv).ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	want := []int64{3, 1, 7}
	if len(got) != len(want) {
		t.Fatalf("listed %d topics, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("topic %d is %d, want %d — the server's order was not kept",
				i, got[i].ID, id)
		}
	}
}

// A deleted topic is not a row. The server sends forumTopicDeleted so a
// client holding a stale list can drop the topic, and this client holds no
// list between calls — drawing one would put a title-less, message-less
// row in the list that opens onto nothing.
func TestADeletedTopicIsNotARow(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{{
		Count: 2,
		Topics: []tg.ForumTopicClass{
			&tg.ForumTopicDeleted{ID: 5},
			jobsTopic(),
		},
	}}}

	got, err := topicListClient(inv).ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("listed %d topics, want only the one that still exists", len(got))
	}
	if got[0].ID != 7 {
		t.Errorf("listed topic %d, want 7", got[0].ID)
	}
}

// A topic row draws a preview line, the same as a chat row, and the message
// behind it arrives beside the topics rather than inside them: the topic
// carries only the ID. Without this the whole topic list draws blank
// second rows.
func TestATopicCarriesTheMessageTheListingSentWithIt(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{{
		Count:  1,
		Topics: []tg.ForumTopicClass{jobsTopic()},
		Messages: []tg.MessageClass{
			&tg.Message{ID: 99, Message: "another topic's"},
			&tg.Message{ID: 412, Message: "remote Go role"},
		},
	}}}

	got, err := topicListClient(inv).ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if got[0].LastMessage == nil {
		t.Fatal("the topic's last message was not attached")
	}
	if got[0].LastMessage.ID != 412 {
		t.Errorf("attached message %d, want the top message 412", got[0].LastMessage.ID)
	}
}

// A topic that names no message it was sent keeps a nil last message rather
// than borrowing one: a preview line from the wrong topic reads as though
// somebody wrote there.
func TestATopicWithNoMessageInThePageHasNone(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{{
		Count:    1,
		Topics:   []tg.ForumTopicClass{jobsTopic()},
		Messages: []tg.MessageClass{&tg.Message{ID: 99, Message: "another topic's"}},
	}}}

	got, err := topicListClient(inv).ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if got[0].LastMessage != nil {
		t.Errorf("the topic borrowed message %d", got[0].LastMessage.ID)
	}
}

// The chat ID is what the caller DOES with a topic: the chat list draws the
// row and then opens it, and the only ID that opens a topic is the
// synthetic one. Allocating it and not handing it back would leave the
// caller with a topic it cannot open.
//
// The round trip is the assertion rather than the number, because the
// number is an allocation order nothing should depend on.
func TestEveryListedTopicCarriesAChatIDThatSplitsBack(t *testing.T) {
	general := &tg.ForumTopic{ID: 1, Title: "General", TopMessage: 2}
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{{
		Count:  2,
		Topics: []tg.ForumTopicClass{general, jobsTopic()},
	}}}
	c := topicListClient(inv)

	got, err := c.ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	seen := map[int64]bool{}
	for _, topic := range got {
		if !isSyntheticChatID(topic.TopicChatID) {
			t.Errorf("topic %d has chat ID %d, which is not a topic's",
				topic.ID, topic.TopicChatID)
			continue
		}
		if seen[topic.TopicChatID] {
			t.Errorf("topic %d shares chat ID %d with another topic",
				topic.ID, topic.TopicChatID)
		}
		seen[topic.TopicChatID] = true

		forum, topicID := c.splitTopic(topic.TopicChatID)
		if forum != forumChatID() || topicID != topic.ID {
			t.Errorf("chat ID %d splits to (%d, %d), want (%d, %d)",
				topic.TopicChatID, forum, topicID, forumChatID(), topic.ID)
		}
	}
}

// Asking a TOPIC for the topic list is the ordinary case, not a mistake:
// the reader is inside a topic and goes back up. The chat ID is split
// first, so the forum is what reaches the wire — and the topic's own ID
// never could, because inputPeer refuses it.
func TestListingTopicsFromInsideATopicAsksAboutTheForum(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{{
		Count: 1, Topics: []tg.ForumTopicClass{jobsTopic()},
	}}}
	c := topicListClient(inv)
	insideATopic := c.topicChatID(forumChatID(), 7)

	if _, err := c.ForumTopics(insideATopic); err != nil {
		t.Fatalf("ForumTopics from inside a topic: %v", err)
	}

	if len(inv.asked) != 1 {
		t.Fatalf("asked the server %d times, want once", len(inv.asked))
	}
	channel, ok := inv.asked[0].Peer.(*tg.InputPeerChannel)
	if !ok {
		t.Fatalf("asked about peer %T, want the forum's channel", inv.asked[0].Peer)
	}
	if channel.ChannelID != forumChannelID {
		t.Errorf("asked about channel %d, want the forum %d",
			channel.ChannelID, forumChannelID)
	}
}

// topicAt is a topic with an ID, a top message and a creation date of its
// own, so a page's last topic can be told from any other.
func topicAt(id, topMessage, date int) *tg.ForumTopic {
	return &tg.ForumTopic{
		ID: id, TopMessage: topMessage, Date: date,
		Title: "topic", IconColor: 0x6FB9F0,
	}
}

// The next page starts from the LAST topic of the one before it — that is
// what core.telegram.org/api/offsets means by paging a topic list, and all
// three offsets have to come from the same topic or the server resumes
// somewhere between two of them.
//
// The date is the date of the message the topic's top_message names, not
// the topic's own: the default order is by last message, so that is the
// date the cursor is expressed in.
func TestTheSecondPageStartsFromTheLastTopicOfTheFirst(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{
		{
			Count:  3,
			Topics: []tg.ForumTopicClass{topicAt(7, 412, 1700000000), topicAt(9, 300, 1690000000)},
			Messages: []tg.MessageClass{
				&tg.Message{ID: 412, Date: 1711111111},
				&tg.Message{ID: 300, Date: 1688888888},
			},
		},
		{Count: 3, Topics: []tg.ForumTopicClass{topicAt(11, 200, 1680000000)}},
	}}

	got, err := topicListClient(inv).ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("listed %d topics, want all 3 across the two pages", len(got))
	}
	if len(inv.asked) != 2 {
		t.Fatalf("asked the server %d times, want 2", len(inv.asked))
	}
	if first := inv.asked[0]; first.OffsetTopic != 0 || first.OffsetID != 0 || first.OffsetDate != 0 {
		t.Errorf("the first request carried offsets %+v, want none — it is the "+
			"top of the list", first)
	}
	second := inv.asked[1]
	if second.OffsetTopic != 9 {
		t.Errorf("OffsetTopic = %d, want the last topic of page one, 9", second.OffsetTopic)
	}
	if second.OffsetID != 300 {
		t.Errorf("OffsetID = %d, want that topic's top message, 300", second.OffsetID)
	}
	if second.OffsetDate != 1688888888 {
		t.Errorf("OffsetDate = %d, want the date of message 300, 1688888888",
			second.OffsetDate)
	}
}

// When the server says it ordered by creation date, the cursor is in that
// date instead: the topic's own, not its last message's. Paging by the
// wrong one against a forum ordered the other way walks the list in an
// order the offsets do not describe, and topics are silently skipped.
func TestPagingByCreateDateUsesTheTopicsOwnDate(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{
		{
			OrderByCreateDate: true,
			Count:             2,
			Topics:            []tg.ForumTopicClass{topicAt(9, 300, 1690000000)},
			Messages:          []tg.MessageClass{&tg.Message{ID: 300, Date: 1688888888}},
		},
		{Count: 2, Topics: []tg.ForumTopicClass{topicAt(11, 200, 1680000000)}},
	}}

	if _, err := topicListClient(inv).ForumTopics(forumChatID()); err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if len(inv.asked) != 2 {
		t.Fatalf("asked the server %d times, want 2", len(inv.asked))
	}
	if got := inv.asked[1].OffsetDate; got != 1690000000 {
		t.Errorf("OffsetDate = %d, want the topic's own date 1690000000 — the "+
			"page said it was ordered by creation date", got)
	}
}

// The messages beside a page are "related", not guaranteed: a top message
// that was deleted, or simply not sent, leaves the cursor with no date to
// take. The topic's own date is the only other one there is, and a cursor
// with a zero date would restart the walk from the newest topic — which is
// the endless loop this whole function is shaped to avoid.
func TestPagingFallsBackToTheTopicsDateWhenItsMessageIsMissing(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{
		{Count: 2, Topics: []tg.ForumTopicClass{topicAt(9, 300, 1690000000)}},
		{Count: 2, Topics: []tg.ForumTopicClass{topicAt(11, 200, 1680000000)}},
	}}

	if _, err := topicListClient(inv).ForumTopics(forumChatID()); err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if len(inv.asked) != 2 {
		t.Fatalf("asked the server %d times, want 2", len(inv.asked))
	}
	if got := inv.asked[1].OffsetDate; got != 1690000000 {
		t.Errorf("OffsetDate = %d, want the topic's own date 1690000000", got)
	}
}

// Having them all is the ordinary way to stop: the server says how many
// there are, and asking again once they have all arrived is a round trip
// that can only answer nothing.
func TestListingStopsWhenEveryTopicHasArrived(t *testing.T) {
	inv := &topicListInvoker{
		pages:   []*tg.MessagesForumTopics{{Count: 1, Topics: []tg.ForumTopicClass{jobsTopic()}}},
		endless: &tg.MessagesForumTopics{Count: 1, Topics: []tg.ForumTopicClass{jobsTopic()}},
	}

	if _, err := topicListClient(inv).ForumTopics(forumChatID()); err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if len(inv.asked) != 1 {
		t.Errorf("asked the server %d times, want once — the first page held "+
			"every topic it said there was", len(inv.asked))
	}
}

// A page with nothing on it is the end of the list whatever the count said,
// and the count can be wrong: it is "topics matching the query", counted
// server-side, and a listing that drains before it is reached would
// otherwise ask forever for a page that keeps coming back empty.
func TestAnEmptyPageEndsTheList(t *testing.T) {
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{
		{Count: 500, Topics: []tg.ForumTopicClass{jobsTopic()}},
		{Count: 500},
	}}

	got, err := topicListClient(inv).ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if len(got) != 1 {
		t.Errorf("listed %d topics, want the 1 that arrived", len(got))
	}
	if len(inv.asked) != 2 {
		t.Errorf("asked the server %d times, want 2 — the empty second page "+
			"is the end", len(inv.asked))
	}
}

// And a server that never stops answering does not hang the client.
//
// This is the failure mode the loop is designed against: a count that is
// never reached, a page that is never empty, and a cursor the server does
// not honour. None of that is hypothetical — an offset the server ignores
// re-serves the same page forever — so the walk is bounded by a count of
// pages as well as by what the answers say.
func TestAForumThatKeepsAnsweringStopsAtTheCap(t *testing.T) {
	inv := &topicListInvoker{endless: &tg.MessagesForumTopics{
		Topics: []tg.ForumTopicClass{topicAt(9, 300, 1690000000)},
	}}

	got, err := topicListClient(inv).ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}

	if len(inv.asked) != maxForumTopicPages {
		t.Errorf("asked the server %d times, want to stop at the cap of %d",
			len(inv.asked), maxForumTopicPages)
	}
	// What it managed to read still comes back: a capped walk is a partial
	// answer, not a failed one, and the topics it did reach are drawable.
	if len(got) == 0 {
		t.Error("a capped walk threw away the topics it had already read")
	}
}

// listedForum lists one forum's topics and hands back the client and the
// topic, which is the state everything below starts from: the topic exists
// as a chat because a listing said so.
func listedForum(t *testing.T) (*Client, *topicListInvoker, *Topic) {
	t.Helper()
	inv := &topicListInvoker{pages: []*tg.MessagesForumTopics{{
		Count:    1,
		Topics:   []tg.ForumTopicClass{jobsTopic()},
		Messages: []tg.MessageClass{&tg.Message{ID: 412, Message: "remote Go role"}},
	}}}
	c := topicListClient(inv)

	topics, err := c.ForumTopics(forumChatID())
	if err != nil {
		t.Fatalf("ForumTopics: %v", err)
	}
	if len(topics) != 1 {
		t.Fatalf("listed %d topics, want 1", len(topics))
	}
	return c, inv, topics[0]
}

// A topic is a chat to everything above this package, and GetChat is how
// anything above asks what a chat is. It cannot resolve a peer for one —
// there is no peer — so it answers from what the listing left behind, and
// it must do that without a round trip: GetChat is on the path of opening a
// chat, and a topic that needed the network to name itself would stall the
// open on every single one.
func TestGetChatOnATopicAnswersWithoutAskingTheServer(t *testing.T) {
	c, inv, topic := listedForum(t)
	before := inv.calls

	chat, err := c.GetChat(topic.TopicChatID)
	if err != nil {
		t.Fatalf("GetChat on a topic: %v", err)
	}

	if inv.calls != before {
		t.Errorf("GetChat on a topic made %d requests, want none — a topic is "+
			"not a peer and there is nothing to ask about", inv.calls-before)
	}
	if chat.ID != topic.TopicChatID {
		t.Errorf("Chat.ID = %d, want the synthetic ID it was asked about, %d",
			chat.ID, topic.TopicChatID)
	}
	if chat.Type != ChatTypeSupergroup {
		t.Errorf("Chat.Type = %v, want a supergroup — a topic is part of one",
			chat.Type)
	}
	if chat.UnreadCount != 12 {
		t.Errorf("UnreadCount = %d, want the topic's 12", chat.UnreadCount)
	}
	if chat.UnreadMentionsCount != 2 {
		t.Errorf("UnreadMentionsCount = %d, want the topic's 2", chat.UnreadMentionsCount)
	}
	if chat.UnreadReactionsCount != 3 {
		t.Errorf("UnreadReactionsCount = %d, want the topic's 3", chat.UnreadReactionsCount)
	}
	if chat.LastReadInboxMessageID != 400 {
		t.Errorf("LastReadInboxMessageID = %d, want the topic's read pointer 400 — "+
			"the unread divider is placed from it", chat.LastReadInboxMessageID)
	}
	if chat.LastMessage == nil || chat.LastMessage.ID != 412 {
		t.Error("the topic's last message did not reach its chat")
	}
}

// The title is the topic's own, not "Forum › Topic". The thread header
// composes the two because a reader inside a topic needs to know which
// forum they are in; a notification does not, and one that said the path
// would not be what the phone shows.
func TestATopicsChatIsTitledWithTheTopicAlone(t *testing.T) {
	c, _, topic := listedForum(t)

	chat, err := c.GetChat(topic.TopicChatID)
	if err != nil {
		t.Fatalf("GetChat on a topic: %v", err)
	}

	if chat.Title != "Jobs" {
		t.Errorf("Title = %q, want the topic's own %q", chat.Title, "Jobs")
	}
}

// An ID out of the synthetic band that names no topic this session listed
// must fail, and say so.
//
// Nothing persists the registry, so one can arrive from a previous run or
// from a translation bug. Letting it fall through to the peers manager
// would ask the server about an ID no peer has, and the reader would be
// told something about peers when what happened is that a topic went
// missing.
func TestGetChatOnATopicNobodyListedSaysSo(t *testing.T) {
	for name, chatID := range map[string]int64{
		"an ID from a previous session": syntheticChatIDBase + 777,
		"an ID allocated but never listed": func() int64 {
			c := &Client{}
			return c.topicChatID(forumChatID(), 5)
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			w := &wireWatcher{}
			api := tg.NewClient(w)
			c := &Client{api: api, peers: peers.Options{}.Build(api)}

			_, err := c.GetChat(chatID)

			if err == nil {
				t.Fatal("GetChat answered for a topic it knows nothing about")
			}
			if !strings.Contains(err.Error(), "forum topic") {
				t.Errorf("GetChat said %q, which does not say the ID is a forum topic", err)
			}
			if w.asked {
				t.Error("a topic nobody listed reached the wire")
			}
		})
	}
}

// Opening a topic announces its chat, the same as opening any other: that
// announcement is what puts a chat the dialog list never carried into the
// store, and a topic is never in the dialog list.
func TestOpeningATopicAnnouncesItsChat(t *testing.T) {
	c, _, topic := listedForum(t)
	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })

	if err := c.OpenChat(topic.TopicChatID); err != nil {
		t.Fatalf("OpenChat on a topic: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("published %d messages, want 1: %#v", len(got), got)
	}
	update, ok := got[0].(ChatUpdateMsg)
	if !ok {
		t.Fatalf("published %T, want a chat update", got[0])
	}
	if update.Chat.ID != topic.TopicChatID || update.Chat.Title != "Jobs" {
		t.Errorf("announced chat %d %q, want the topic %d %q",
			update.Chat.ID, update.Chat.Title, topic.TopicChatID, "Jobs")
	}
}

// The registry knows which forums have been listed, because an arriving
// message can only be filed under a topic in a forum whose topics are
// chats already. Outside one there is nothing to file it under.
func TestAListingIsRememberedAgainstItsForum(t *testing.T) {
	c, _, _ := listedForum(t)

	if !c.topics.hasListed(forumChatID()) {
		t.Error("the forum whose topics were just listed does not say so")
	}
	if c.topics.hasListed(channelChatID(11)) {
		t.Error("a forum nobody has listed claims its topics are known")
	}
}

// And a second listing replaces what the first said, rather than merging
// with it: every field of a topic record is what the server believes right
// now, so a counter that only ever grew would be one the reader can never
// clear.
func TestARelistingReplacesWhatTheTopicWas(t *testing.T) {
	c, _, topic := listedForum(t)

	read := jobsTopic()
	read.UnreadCount = 0
	c.topics.remember(topic.TopicChatID, topicFromTG(forumChatID(), read))

	chat, err := c.GetChat(topic.TopicChatID)
	if err != nil {
		t.Fatalf("GetChat on a topic: %v", err)
	}
	if chat.UnreadCount != 0 {
		t.Errorf("UnreadCount = %d, want the 0 the later listing reported",
			chat.UnreadCount)
	}
}
