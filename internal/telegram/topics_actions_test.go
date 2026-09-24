package telegram

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

// This file is the actions a reader takes INSIDE an open topic that are not
// reading it and not sending to it: reacting, pinning, editing, forwarding
// in and out, and asking who is in the group.
//
// Every one of them used to reach inputPeer with the topic's own chat ID and
// stop at its refusal, which the guard counted as handled. The refusal is a
// safety net, not an answer: with a topic open, the chat ID the app passes IS
// the synthetic one, so each of these simply failed.

// topicActionInvoker stands in for the server for those actions, recording
// the request rather than answering with anything interesting — which peer
// was named, and which topic, is the whole subject. Everything else falls
// through to readHistoryInvoker, which is what knows the forum's channel.
type topicActionInvoker struct {
	reactions []*tg.MessagesSendReactionRequest
	pins      []*tg.MessagesUpdatePinnedMessageRequest
	edits     []*tg.MessagesEditMessageRequest
	forwards  []*tg.MessagesForwardMessagesRequest

	// edited is the message the server answers an edit with, filed under the
	// forum as every message in one is.
	edited *tg.Message
}

func (f *topicActionInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	switch req := input.(type) {
	case *tg.MessagesSendReactionRequest:
		f.reactions = append(f.reactions, req)
		output.(*tg.UpdatesBox).Updates = &tg.Updates{}
		return nil
	case *tg.MessagesUpdatePinnedMessageRequest:
		f.pins = append(f.pins, req)
		output.(*tg.UpdatesBox).Updates = &tg.Updates{}
		return nil
	case *tg.MessagesEditMessageRequest:
		f.edits = append(f.edits, req)
		edited := f.edited
		if edited == nil {
			edited = inTheForum(412, "edited")
		}
		output.(*tg.UpdatesBox).Updates = &tg.Updates{Updates: []tg.UpdateClass{
			&tg.UpdateEditChannelMessage{Message: edited},
		}}
		return nil
	case *tg.MessagesForwardMessagesRequest:
		f.forwards = append(f.forwards, req)
		output.(*tg.UpdatesBox).Updates = updatesWith(inTheForum(300, "forwarded"))
		return nil
	default:
		return readHistoryInvoker{}.Invoke(ctx, input, output)
	}
}

// inTheForum is a message as the server hands it back: belonging to the
// forum's channel, because that is the peer, whichever topic it sits in.
func inTheForum(id int, text string) *tg.Message {
	return &tg.Message{
		ID:      id,
		PeerID:  &tg.PeerChannel{ChannelID: forumChannelID},
		Message: text,
	}
}

// A reaction goes on a message, and a topic's messages are the forum's: the
// peer is the forum's, and messages.sendReaction names no topic because a
// message ID already belongs to exactly one.
func TestReactingInsideATopicNamesTheForumsPeer(t *testing.T) {
	inv := &topicActionInvoker{}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if err := c.SetReaction(topic, 412, "❤"); err != nil {
		t.Fatalf("SetReaction: %v", err)
	}

	if len(inv.reactions) != 1 {
		t.Fatalf("made %d reaction requests, want one", len(inv.reactions))
	}
	req := inv.reactions[0]
	assertForumPeer(t, req.Peer)
	if req.MsgID != 412 {
		t.Errorf("reacted to message %d, want 412", req.MsgID)
	}
}

// Pinning is the same shape: the pin is on one message of the forum, and the
// forum's peer is the only one there is to name.
func TestPinningInsideATopicNamesTheForumsPeer(t *testing.T) {
	inv := &topicActionInvoker{}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if err := c.SetPinned(topic, 412, true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}

	if len(inv.pins) != 1 {
		t.Fatalf("made %d pin requests, want one", len(inv.pins))
	}
	assertForumPeer(t, inv.pins[0].Peer)
	if inv.pins[0].ID != 412 {
		t.Errorf("pinned message %d, want 412", inv.pins[0].ID)
	}
}

// Editing your own message inside a topic edits it where it lives, which is
// the forum — and the message that comes back has to belong to the topic the
// reader is looking at, because that is the thread that will redraw it.
func TestEditingAMessageInsideATopicNamesTheForumAndAnswersInTheTopic(t *testing.T) {
	inv := &topicActionInvoker{}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	got, err := c.EditTextMessage(topic, 412, "edited")
	if err != nil {
		t.Fatalf("EditTextMessage: %v", err)
	}

	if len(inv.edits) != 1 {
		t.Fatalf("made %d edit requests, want one", len(inv.edits))
	}
	assertForumPeer(t, inv.edits[0].Peer)
	if inv.edits[0].ID != 412 {
		t.Errorf("edited message %d, want 412", inv.edits[0].ID)
	}
	if got == nil {
		t.Fatal("the edit answered with no message")
	}
	if got.ChatID != topic || got.TopicID != jobsTopicID {
		t.Errorf("the edited message belongs to chat %d topic %d, want chat %d topic %d "+
			"(the forum is %d)", got.ChatID, got.TopicID, topic, jobsTopicID, forumChatID())
	}
}

// And an edit in a chat that is not a topic comes back exactly as the server
// wrote it: the rewrite is for topics alone.
func TestEditingAMessageInAnOrdinaryChatIsUnchanged(t *testing.T) {
	inv := &topicActionInvoker{}
	c, _ := topicReadClient(t, inv)

	got, err := c.EditTextMessage(forumChatID(), 412, "edited")
	if err != nil {
		t.Fatalf("EditTextMessage: %v", err)
	}

	if got.ChatID != forumChatID() {
		t.Errorf("the edited message belongs to chat %d, want %d", got.ChatID, forumChatID())
	}
	if got.TopicID != 0 {
		t.Errorf("the edited message names topic %d, want none", got.TopicID)
	}
}

// Forwarding OUT of a topic reads the messages where they live. A topic's
// messages are the forum's, so the source peer is the forum's — unsplit, the
// forward failed before it started.
func TestForwardingOutOfATopicNamesTheForumAsTheSource(t *testing.T) {
	inv := &topicActionInvoker{}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if _, err := c.ForwardMessages(topic, basicGroupID, []int64{412}); err != nil {
		t.Fatalf("ForwardMessages: %v", err)
	}

	if len(inv.forwards) != 1 {
		t.Fatalf("made %d forwards, want one", len(inv.forwards))
	}
	assertForumPeer(t, inv.forwards[0].FromPeer)
}

// Forwarding INTO a topic has to say which topic, or the copies land in
// General in front of the whole forum. top_msg_id is how the destination
// topic is named, and it is the only way: a forward carries no reply header.
func TestForwardingIntoATopicNamesTheDestinationTopic(t *testing.T) {
	inv := &topicActionInvoker{}
	c, _ := topicReadClient(t, inv)
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if _, err := c.ForwardMessages(basicGroupID, topic, []int64{412}); err != nil {
		t.Fatalf("ForwardMessages: %v", err)
	}

	if len(inv.forwards) != 1 {
		t.Fatalf("made %d forwards, want one", len(inv.forwards))
	}
	req := inv.forwards[0]
	assertForumPeer(t, req.ToPeer)
	assertTopMsgID(t, req.GetTopMsgID)
}

// A chat that is not a topic has no destination topic, and a top_msg_id of
// zero is not "no topic" — it is a topic nothing is in.
func TestForwardingIntoAnOrdinaryChatNamesNoTopic(t *testing.T) {
	inv := &topicActionInvoker{}
	c, _ := topicReadClient(t, inv)

	if _, err := c.ForwardMessages(basicGroupID, forumChatID(), []int64{412}); err != nil {
		t.Fatalf("ForwardMessages: %v", err)
	}

	if len(inv.forwards) != 1 {
		t.Fatalf("made %d forwards, want one", len(inv.forwards))
	}
	if top, ok := inv.forwards[0].GetTopMsgID(); ok {
		t.Errorf("the forward named topic %d, want no top_msg_id at all", top)
	}
}

// A message forwarded into a topic is announced to the topic as well as to
// the forum, the way an arriving one is: the topic is the row the reader is
// looking at, and a forward that only told the forum left the open thread
// showing nothing until it was reloaded.
func TestAForwardIntoATopicIsAnnouncedToTheTopic(t *testing.T) {
	inv := &topicActionInvoker{}
	c, published := topicReadClient(t, inv)
	// Listed, because a forum nobody has opened has no topic rows to
	// announce to — the gate messageFiledUnderItsTopic applies.
	c.topics.markListed(forumChatID())
	topic := c.topicChatID(forumChatID(), jobsTopicID)

	if _, err := c.ForwardMessages(basicGroupID, topic, []int64{412}); err != nil {
		t.Fatalf("ForwardMessages: %v", err)
	}

	if !announcedIn(*published, topic, jobsTopicID) {
		t.Errorf("published %#v, none of it a new message in the topic's chat %d",
			*published, topic)
	}
	if !announcedIn(*published, forumChatID(), jobsTopicID) {
		t.Errorf("published %#v, none of it a new message in the forum's chat %d — a "+
			"forwarded message is still in the forum's flat stream", *published, forumChatID())
	}
}

// announcedIn reports whether a new message was announced in chatID, naming
// the topic it belongs to.
func announcedIn(published []tea.Msg, chatID, topicID int64) bool {
	for _, m := range published {
		arrived, ok := m.(NewMessageMsg)
		if !ok || arrived.Message == nil {
			continue
		}
		if arrived.Message.ChatID == chatID && arrived.Message.TopicID == topicID {
			return true
		}
	}
	return false
}

// The thread asks how many members a chat has the moment it opens, so a
// topic that could not answer errored where a group would not. Members
// belong to the forum: a topic has no members of its own, and the count the
// header wants is the forum's.
func TestTheMemberCountOfATopicIsTheForums(t *testing.T) {
	full := &tg.ChannelFull{ID: forumChannelID}
	full.SetParticipantsCount(42)
	inv := &memberInvoker{fullChannel: &tg.MessagesChatFull{FullChat: full}}
	c, _ := memberClient(inv)
	topic := c.topicChatID(channelChatID(forumChannelID), jobsTopicID)

	got, err := c.GetSupergroupFullInfo(topic)
	if err != nil {
		t.Fatalf("GetSupergroupFullInfo: %v", err)
	}
	if got.MemberCount != 42 {
		t.Errorf("MemberCount = %d, want the forum's 42", got.MemberCount)
	}
}

// And the member list itself, for the same reason.
func TestTheMembersOfATopicAreTheForums(t *testing.T) {
	inv := &memberInvoker{participants: &tg.ChannelsChannelParticipants{
		Count:        1,
		Participants: []tg.ChannelParticipantClass{&tg.ChannelParticipant{UserID: 7}},
		Users:        []tg.UserClass{member(7, "Ana", "ana")},
	}}
	c, _ := memberClient(inv)
	topic := c.topicChatID(channelChatID(forumChannelID), jobsTopicID)

	got, err := c.GetSupergroupMembers(topic, 0, 50)
	if err != nil {
		t.Fatalf("GetSupergroupMembers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("listed %d members, want the forum's 1", len(got))
	}
}

// The @-mention picker in a topic offers the forum's members. Unsplit, a
// synthetic chat ID is neither a basic group nor a channel, so the picker
// fell through to the "not that kind of chat" branch and silently offered
// nobody while the reader typed.
func TestTheMentionPickerInATopicSearchesTheForumsMembers(t *testing.T) {
	inv := &memberInvoker{participants: &tg.ChannelsChannelParticipants{
		Count:        1,
		Participants: []tg.ChannelParticipantClass{&tg.ChannelParticipant{UserID: 7}},
		Users:        []tg.UserClass{member(7, "Ana", "ana")},
	}}
	c, _ := memberClient(inv)
	topic := c.topicChatID(channelChatID(forumChannelID), jobsTopicID)

	got, err := c.SearchChatMembers(topic, "an", 20)
	if err != nil {
		t.Fatalf("SearchChatMembers: %v", err)
	}
	if len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("offered %#v, want the forum's one member", got)
	}
}

// A basic group's full info is one of the two questions a topic is refused
// rather than answered, and the refusal has to say which question it was:
// splitting would ask about the forum, and a forum is a supergroup, which
// this call cannot answer about at all.
func TestABasicGroupsFullInfoRefusesATopicInASentence(t *testing.T) {
	inv := &memberInvoker{}
	c, _ := memberClient(inv)
	topic := c.topicChatID(channelChatID(forumChannelID), jobsTopicID)

	_, err := c.GetBasicGroupFullInfo(topic)

	if err == nil {
		t.Fatal("GetBasicGroupFullInfo answered about a forum topic as though it " +
			"were a basic group")
	}
	if !strings.Contains(err.Error(), "forum topic") {
		t.Errorf("the failure reads %q, which does not say the chat is a topic", err)
	}
	if len(inv.asked) != 0 {
		t.Errorf("a refused call still asked the server %#v", inv.asked)
	}
}

// And a chat that is not a topic is asked about exactly as before.
func TestABasicGroupsFullInfoStillAnswersForABasicGroup(t *testing.T) {
	inv := &memberInvoker{fullChat: &tg.MessagesChatFull{FullChat: &tg.ChatFull{ID: 5}}}
	c, _ := memberClient(inv)

	if _, err := c.GetBasicGroupFullInfo(basicGroupID); err != nil {
		t.Fatalf("GetBasicGroupFullInfo: %v", err)
	}
}

// topicActions are the actions a reader takes inside an open topic, each
// making exactly one request and handing back what went out along with the
// topic's own chat ID. It is [topicReads]'s twin, for the calls that change
// something rather than read it.
var topicActions = map[string]func(t *testing.T) (bin.Encoder, int64){
	"reacting to a message": func(t *testing.T) (bin.Encoder, int64) {
		inv := &topicActionInvoker{}
		c, _ := topicReadClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if err := c.SetReaction(topic, 412, "❤"); err != nil {
			t.Fatalf("SetReaction: %v", err)
		}
		return onlyRequest(t, inv.reactions), topic
	},
	"pinning a message": func(t *testing.T) (bin.Encoder, int64) {
		inv := &topicActionInvoker{}
		c, _ := topicReadClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if err := c.SetPinned(topic, 412, true); err != nil {
			t.Fatalf("SetPinned: %v", err)
		}
		return onlyRequest(t, inv.pins), topic
	},
	"editing a message": func(t *testing.T) (bin.Encoder, int64) {
		inv := &topicActionInvoker{}
		c, _ := topicReadClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if _, err := c.EditTextMessage(topic, 412, "edited"); err != nil {
			t.Fatalf("EditTextMessage: %v", err)
		}
		return onlyRequest(t, inv.edits), topic
	},
	"forwarding out of it": func(t *testing.T) (bin.Encoder, int64) {
		inv := &topicActionInvoker{}
		c, _ := topicReadClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if _, err := c.ForwardMessages(topic, basicGroupID, []int64{412}); err != nil {
			t.Fatalf("ForwardMessages: %v", err)
		}
		return onlyRequest(t, inv.forwards), topic
	},
	"forwarding into it": func(t *testing.T) (bin.Encoder, int64) {
		inv := &topicActionInvoker{}
		c, _ := topicReadClient(t, inv)
		topic := c.topicChatID(forumChatID(), jobsTopicID)
		if _, err := c.ForwardMessages(basicGroupID, topic, []int64{412}); err != nil {
			t.Fatalf("ForwardMessages: %v", err)
		}
		return onlyRequest(t, inv.forwards), topic
	},
}

// A topic's chat ID names nothing Telegram has ever heard of, and every one
// of these actions resolves its peer from the forum instead. The check is on
// the encoded request rather than on one field, so a topic ID smuggled into
// any part of any of them fails here — the same net [topicReads] is held in.
func TestNoActionInATopicPutsItsSyntheticIDOnTheWire(t *testing.T) {
	for name, act := range topicActions {
		t.Run(name, func(t *testing.T) {
			sent, topic := act(t)

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
