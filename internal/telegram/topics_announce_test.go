package telegram

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/tg"
)

// This file is the ANNOUNCEMENT side of a forum topic: not which request
// this package sends, which topics_fetch_test.go and topics_read_test.go
// hold, but which chat it names in what it publishes afterwards.
//
// The rule is one line long — everything published carries the chat ID the
// CALLER used, and for a topic that is the synthetic one — and it is the
// half that was missed. A request aimed at the wrong chat fails loudly; an
// announcement aimed at the wrong chat is a screen that quietly never
// changes, or, for a deletion, other chats losing messages they still have.

// publishing attaches a sink to c and hands back everything it collects, in
// order. The tests below are about what came out of it and in what order.
func publishing(c *Client) *[]tea.Msg {
	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })
	return &got
}

// deletions are the deletion events published, for a failure that names the
// chats they were aimed at rather than printing every message.
func deletions(t *testing.T, published []tea.Msg) []MessageDeletedMsg {
	t.Helper()

	out := make([]MessageDeletedMsg, 0, len(published))
	for i, msg := range published {
		deleted, ok := msg.(MessageDeletedMsg)
		if !ok {
			t.Fatalf("published[%d] = %#v, want a deletion", i, msg)
		}
		out = append(out, deleted)
	}
	return out
}

// deletedIn is the chat IDs a run of deletions named.
func deletedIn(deleted []MessageDeletedMsg) []int64 {
	out := make([]int64, len(deleted))
	for i, d := range deleted {
		out[i] = d.ChatId
	}
	return out
}

// Deleting your own message inside a topic has to be announced FOR that
// topic. A synthetic chat ID is not a channel — by design — so the kind
// test that decides whether a deletion names its chat answered no for one,
// and the event went out with a chat of zero.
//
// Zero is not "no chat" to the thread, it is EVERY chat: the store deletes
// those IDs from all of them. Message IDs collide freely between chats, so
// deleting message 4521 in a topic took message 4521 out of every other
// loaded conversation that happened to have one.
func TestADeletionInATopicIsAnnouncedForThatTopic(t *testing.T) {
	c, _ := topicFetchClient(t)
	published := publishing(c)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if err := c.DeleteMessages(topic, []int64{4521}, true); err != nil {
		t.Fatalf("DeleteMessages: %v", err)
	}

	got := deletions(t, *published)
	if len(got) != 1 {
		t.Fatalf("published %d deletions, want 1", len(got))
	}
	if got[0].ChatId != topic {
		t.Errorf("the deletion named chat %d, want the topic %d — a chat of 0 is "+
			"read as every chat, and takes those message IDs out of all of them",
			got[0].ChatId, topic)
	}
	if len(got[0].MessageIds) != 1 || got[0].MessageIds[0] != 4521 {
		t.Errorf("the deletion carried %v, want message 4521", got[0].MessageIds)
	}
}

// And what a zero chat MEANS is pinned from the other side, because the fix
// above turns on it: a channel names its chat, and everything else says
// nothing and is resolved from every chat, which is what the server's own
// peerless deletion update carries.
func TestADeletionInAnOrdinaryChatIsAnnouncedAsBefore(t *testing.T) {
	t.Run("a channel names its chat", func(t *testing.T) {
		c, _ := topicFetchClient(t)
		published := publishing(c)

		if err := c.DeleteMessages(forumChatID(), []int64{4521}, true); err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		got := deletions(t, *published)
		if len(got) != 1 || got[0].ChatId != forumChatID() {
			t.Fatalf("published %v, want one deletion for the channel %d",
				deletedIn(got), forumChatID())
		}
	})

	t.Run("a basic group names none", func(t *testing.T) {
		c, _ := topicFetchClient(t)
		published := publishing(c)

		if err := c.DeleteMessages(basicGroupID, []int64{4521}, true); err != nil {
			t.Fatalf("DeleteMessages: %v", err)
		}

		got := deletions(t, *published)
		if len(got) != 1 || got[0].ChatId != 0 {
			t.Fatalf("published %v, want one deletion carrying no chat at all",
				deletedIn(got))
		}
	})
}

// edits are the refetch requests published, in order.
//
// An edit, a reaction and a poll vote all come out as one of these: the
// tallies arrive attached to the message they belong to, so the thread
// answers all three by fetching the message again.
func edits(t *testing.T, published []tea.Msg) []MessageEditedMsg {
	t.Helper()

	out := make([]MessageEditedMsg, 0, len(published))
	for i, msg := range published {
		edited, ok := msg.(MessageEditedMsg)
		if !ok {
			t.Fatalf("published[%d] = %#v, want a refetch request", i, msg)
		}
		out = append(out, edited)
	}
	return out
}

// editedIn is the chat IDs a run of refetch requests named.
func editedIn(edited []MessageEditedMsg) []int64 {
	out := make([]int64, len(edited))
	for i, e := range edited {
		out[i] = e.ChatId
	}
	return out
}

// inTopic is a message of the forum as the server sends one: the peer is
// the forum's channel, and the topic is in the reply header, where every
// ordinary message in a forum carries it.
func inTopic(id int, topicID int, text string) *tg.Message {
	m := &tg.Message{
		ID:      id,
		PeerID:  &tg.PeerChannel{ChannelID: forumChannelID},
		Message: text,
	}
	m.SetReplyTo(replyHeader(true, topicID, 0))
	return m
}

// handle dispatches one update the way the update stream would.
func handle(t *testing.T, d tg.UpdateDispatcher, u tg.UpdateClass) {
	t.Helper()
	if err := d.Handle(context.Background(), &tg.Updates{Updates: []tg.UpdateClass{u}}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

// Somebody edits a message in the topic you are reading, and the thread
// redraws it — which it does by refetching, and only for the chat it has
// open. With an open topic that chat is the topic's synthetic ID, so an
// edit announced for the forum alone reached nothing and the row kept the
// text it had until the topic was closed and opened again.
//
// Announced twice, as an arriving message is: the forum's flat stream holds
// the message too, and only one of the two can be the chat on screen, so
// the pair costs a dispatch and never a second round trip.
func TestAnEditInATopicIsAnnouncedForTheTopicToo(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	handle(t, d, &tg.UpdateEditChannelMessage{
		Message: inTopic(412, int(jobsTopicID), "edited"),
	})

	got := edits(t, *published)
	if want := []int64{forumChatID(), topic}; !int64sEqual(editedIn(got), want) {
		t.Fatalf("the edit was announced for chats %v, want the forum then its "+
			"topic %v", editedIn(got), want)
	}
	for i, e := range got {
		if e.MessageId != 412 {
			t.Errorf("announcement %d names message %d, want 412", i, e.MessageId)
		}
	}
}

// A message in a forum that names no topic is in General, which is the rule
// an arriving message is already filed by. An edit that skipped it would
// leave the one topic every forum has unable to redraw anything.
func TestAnEditWithNoTopicInAListedForumGoesToGeneral(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	general := c.topicChatID(forumChatID(), generalTopicID)

	handle(t, d, &tg.UpdateEditChannelMessage{
		Message: &tg.Message{
			ID:      412,
			PeerID:  &tg.PeerChannel{ChannelID: forumChannelID},
			Message: "edited",
		},
	})

	got := edits(t, *published)
	if want := []int64{forumChatID(), general}; !int64sEqual(editedIn(got), want) {
		t.Fatalf("the edit was announced for chats %v, want the forum then its "+
			"General %v", editedIn(got), want)
	}
}

// And an edit outside a forum is announced once, as it always was: an
// ordinary group has no topics to reach, and a forum nobody has listed has
// no topic chats for one to be keyed by.
func TestAnEditOutsideAListedForumIsAnnouncedOnce(t *testing.T) {
	_, d, published := forumListening(t)

	handle(t, d, &tg.UpdateEditChannelMessage{
		Message: inTopic(412, int(jobsTopicID), "edited"),
	})

	got := edits(t, *published)
	if want := []int64{forumChatID()}; !int64sEqual(editedIn(got), want) {
		t.Fatalf("the edit was announced for chats %v, want only the forum %v",
			editedIn(got), want)
	}
}

// unreadReaction is a reaction tally with something in it the reader has
// not seen, which is what raises the heart on the chat's row.
func unreadReaction() tg.MessageReactions {
	return tg.MessageReactions{
		RecentReactions: []tg.MessagePeerReaction{{Unread: true}},
	}
}

// reactionEcho is the echo Telegram sends after a reaction goes on a
// message of the forum, naming the topic it is in.
func reactionEcho(topMsgID int, reactions tg.MessageReactions) *tg.UpdateMessageReactions {
	u := &tg.UpdateMessageReactions{
		Peer:      &tg.PeerChannel{ChannelID: forumChannelID},
		MsgID:     412,
		Reactions: reactions,
	}
	if topMsgID != 0 {
		u.SetTopMsgID(topMsgID)
	}
	return u
}

// You react to a message in a topic, the request goes out correctly, the
// server echoes it — and the chip never appeared, because the echo was
// announced for the forum and the open thread is the topic. reactions.go
// said this worked; this is what makes that true.
func TestAReactionInATopicIsAnnouncedForTheTopicToo(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	handle(t, d, reactionEcho(int(jobsTopicID), tg.MessageReactions{}))

	got := edits(t, *published)
	if want := []int64{forumChatID(), topic}; !int64sEqual(editedIn(got), want) {
		t.Fatalf("the reaction was announced for chats %v, want the forum then its "+
			"topic %v", editedIn(got), want)
	}
}

// A reaction to the reader's own message also raises the unread-reaction
// chip, and that one is a COUNT rather than a refetch: it has to land on
// the topic the update named and on nothing else, because a badge put up
// on a row that has nothing to show cannot be cleared by reading.
func TestAnUnreadReactionInATopicRaisesTheTopicsChipToo(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	handle(t, d, reactionEcho(int(jobsTopicID), unreadReaction()))

	want := []tea.Msg{
		MessageEditedMsg{ChatId: forumChatID(), MessageId: 412},
		MessageEditedMsg{ChatId: topic, MessageId: 412},
		ChatUnreadReactionsMsg{ChatId: forumChatID()},
		ChatUnreadReactionsMsg{ChatId: topic},
	}
	if len(*published) != len(want) {
		t.Fatalf("published %#v, want %#v", *published, want)
	}
	for i := range want {
		if (*published)[i] != want[i] {
			t.Errorf("published[%d] = %#v, want %#v", i, (*published)[i], want[i])
		}
	}
}

// An update inside a forum that names no topic means General, which is the
// rule the reply header already states for a message: a message in a forum
// with no topic on it is in the one topic every forum has. Any other answer
// would leave General unable to show a reaction or a poll.
func TestAReactionWithNoTopicInAListedForumGoesToGeneral(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	general := c.topicChatID(forumChatID(), generalTopicID)

	handle(t, d, reactionEcho(0, tg.MessageReactions{}))

	got := edits(t, *published)
	if want := []int64{forumChatID(), general}; !int64sEqual(editedIn(got), want) {
		t.Fatalf("the reaction was announced for chats %v, want the forum then its "+
			"General %v", editedIn(got), want)
	}
}

// The same update in a chat whose topics nobody listed is announced once.
// top_msg_id is not a topic there: in a discussion group it names a channel
// post's comment thread, which is no chat of this client's at all.
func TestAReactionOutsideAListedForumIsAnnouncedOnce(t *testing.T) {
	_, d, published := forumListening(t)

	handle(t, d, reactionEcho(int(jobsTopicID), unreadReaction()))

	want := []tea.Msg{
		MessageEditedMsg{ChatId: forumChatID(), MessageId: 412},
		ChatUnreadReactionsMsg{ChatId: forumChatID()},
	}
	if len(*published) != len(want) || (*published)[0] != want[0] || (*published)[1] != want[1] {
		t.Fatalf("published %#v, want %#v", *published, want)
	}
}

// A poll's tally arrives as its own update and is answered by the same
// refetch, so it needs the same pair. A poll lives in one topic and is
// voted on by everybody reading it, which is the case where a thread that
// never redraws is most obvious.
func TestAPollVoteInATopicIsAnnouncedForTheTopicToo(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	u := &tg.UpdateMessagePoll{PollID: 77}
	u.SetPeer(&tg.PeerChannel{ChannelID: forumChannelID})
	u.SetMsgID(412)
	u.SetTopMsgID(int(jobsTopicID))
	handle(t, d, u)

	got := edits(t, *published)
	if want := []int64{forumChatID(), topic}; !int64sEqual(editedIn(got), want) {
		t.Fatalf("the poll was announced for chats %v, want the forum then its "+
			"topic %v", editedIn(got), want)
	}
}

// A poll update carries no peer at all when the server does not say which
// chat each reader has it open in, and that is still dropped: a refetch
// with no chat is a request aimed at nothing.
func TestAPollVoteWithNoPeerIsAnnouncedNowhere(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())

	handle(t, d, &tg.UpdateMessagePoll{PollID: 77})

	if len(*published) != 0 {
		t.Fatalf("published %#v for a poll update that named no chat", *published)
	}
}

// A message deleted from another device arrives as a channel deletion, and
// that update carries message IDs and a channel — and no topic, because the
// schema has no field for one. The messages are gone; which topic they were
// in is a thing the server has stopped saying.
//
// So the deletion is announced for the forum and for every topic of it this
// session has a chat for. It is honest because of what a message ID IS in a
// channel: the forum's own numbering, one message in exactly one topic, so
// the announcement is right for the topic that held the message and a no-op
// for the others — the store removes those IDs from the chat it is given,
// and the ones that never had them have nothing to remove.
//
// The alternative was the forum alone, which is what it did: a message
// deleted on the phone stayed on screen in the topic for as long as it was
// open.
func TestARemoteDeletionInAForumReachesItsTopicsToo(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	general := c.topicChatID(forumChatID(), generalTopicID)
	jobs := c.topicChatID(forumChatID(), jobsTopicID)

	handle(t, d, &tg.UpdateDeleteChannelMessages{
		ChannelID: forumChannelID,
		Messages:  []int{4521},
	})

	got := deletions(t, *published)
	if want := []int64{forumChatID(), general, jobs}; !int64sEqual(deletedIn(got), want) {
		t.Fatalf("the deletion was announced for chats %v, want the forum and each "+
			"of its topics %v", deletedIn(got), want)
	}
	for i, d := range got {
		if len(d.MessageIds) != 1 || d.MessageIds[0] != 4521 {
			t.Errorf("announcement %d carried %v, want message 4521", i, d.MessageIds)
		}
	}
}

// A channel with no topic chats is announced once, as it always was. That
// covers every ordinary channel and supergroup, and a forum nobody has
// opened: there is nothing keyed by its topics to tell.
func TestARemoteDeletionOutsideAListedForumIsAnnouncedOnce(t *testing.T) {
	_, d, published := forumListening(t)

	handle(t, d, &tg.UpdateDeleteChannelMessages{
		ChannelID: forumChannelID,
		Messages:  []int{4521},
	})

	got := deletions(t, *published)
	if want := []int64{forumChatID()}; !int64sEqual(deletedIn(got), want) {
		t.Fatalf("the deletion was announced for chats %v, want only the channel %v",
			deletedIn(got), want)
	}
}

// actionsIn is the chats a run of typing notices was announced for, holding
// each to naming the user who is typing.
func actionsIn(t *testing.T, published []tea.Msg) []int64 {
	t.Helper()

	out := make([]int64, 0, len(published))
	for i, msg := range published {
		action, ok := msg.(ChatActionMsg)
		if !ok {
			t.Fatalf("published[%d] = %#v, want a typing notice", i, msg)
		}
		if action.UserId != 3 {
			t.Errorf("published[%d] names user %d, want 3", i, action.UserId)
		}
		if _, ok := action.Action.(*ChatActionTyping); !ok {
			t.Errorf("published[%d] carries %#v, want typing", i, action.Action)
		}
		out = append(out, action.ChatId)
	}
	return out
}

// Somebody typing in a topic is drawn by the thread that has that topic
// open, and the thread keeps only the actions for the chat it is showing.
// Announced for the forum alone, the indicator never appeared inside a
// topic and appeared in the forum's flat stream for people typing in topics
// nobody was reading.
func TestTypingInATopicIsAnnouncedForTheTopicToo(t *testing.T) {
	c, d, published := forumListening(t)
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	u := &tg.UpdateChannelUserTyping{
		ChannelID: forumChannelID,
		FromID:    &tg.PeerUser{UserID: 3},
		Action:    &tg.SendMessageTypingAction{},
	}
	u.SetTopMsgID(int(jobsTopicID))
	handle(t, d, u)

	if want := []int64{forumChatID(), topic}; !int64sEqual(actionsIn(t, *published), want) {
		t.Fatalf("typing was announced for chats %v, want the forum then its topic %v",
			actionsIn(t, *published), want)
	}
}

// And typing in a channel whose topics nobody listed is announced once: the
// thread ID on this update is a comment thread there, which is no chat.
func TestTypingOutsideAListedForumIsAnnouncedOnce(t *testing.T) {
	_, d, published := forumListening(t)

	u := &tg.UpdateChannelUserTyping{
		ChannelID: forumChannelID,
		FromID:    &tg.PeerUser{UserID: 3},
		Action:    &tg.SendMessageTypingAction{},
	}
	u.SetTopMsgID(int(jobsTopicID))
	handle(t, d, u)

	if want := []int64{forumChatID()}; !int64sEqual(actionsIn(t, *published), want) {
		t.Fatalf("typing was announced for chats %v, want only the channel %v",
			actionsIn(t, *published), want)
	}
}
