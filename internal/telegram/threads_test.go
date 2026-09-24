package telegram

import (
	"testing"

	"github.com/gotd/td/tg"
)

// replyHeader builds the header Telegram would send, setting a field only
// when it would set the field's flag. Absence is what the rule turns on —
// a forum message that is not a reply is told apart from one that is by
// which of the two ids the header carries — so a builder that filled both
// in every case would test nothing.
func replyHeader(forum bool, replyToMsgID, replyToTopID int) *tg.MessageReplyHeader {
	h := &tg.MessageReplyHeader{ForumTopic: forum}
	if replyToMsgID != 0 {
		h.SetReplyToMsgID(replyToMsgID)
	}
	if replyToTopID != 0 {
		h.SetReplyToTopID(replyToTopID)
	}
	return h
}

// Telegram carries two different things in one reply header, and in a
// forum it puts the second in the field meant for the first: every
// ordinary message in a forum has a reply header that names its topic and
// answers nothing. Reading the header by Telegram's own rule is what keeps
// the reply quote under the messages that really are replies.
func TestTheReplyHeaderTellsAReplyApartFromItsTopic(t *testing.T) {
	tests := []struct {
		name      string
		header    tg.MessageReplyHeaderClass
		wantReply int64
		wantTopic int64
	}{
		{
			name:      "a reply outside a forum names what it answers and no topic",
			header:    replyHeader(false, 41, 0),
			wantReply: 41,
		},
		{
			name:      "a channel discussion comment has a top id but is still only a reply",
			header:    replyHeader(false, 41, 7),
			wantReply: 41,
		},
		{
			name:      "a reply inside a topic names both the message and the topic",
			header:    replyHeader(true, 41, 7),
			wantReply: 41,
			wantTopic: 7,
		},
		{
			name:      "a forum message that is not a reply names its topic and nothing else",
			header:    replyHeader(true, 7, 0),
			wantTopic: 7,
		},
		{
			name:   "a message with no header names neither",
			header: nil,
		},
		{
			name:   "a reply to a story is not a reply to a message",
			header: &tg.MessageReplyStoryHeader{Peer: &tg.PeerUser{UserID: 4}, StoryID: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReply, gotTopic := replyTargetsFromTG(tt.header)
			if gotReply != tt.wantReply || gotTopic != tt.wantTopic {
				t.Errorf("replyTargetsFromTG = (reply %d, topic %d), want (reply %d, topic %d)",
					gotReply, gotTopic, tt.wantReply, tt.wantTopic)
			}
		})
	}
}

// The bug this fixes, end to end: the chat view draws a reply quote for
// any message with a ReplyToMessageID, so copying the header's id straight
// across put a quote under every message in a forum — and, since a topic's
// root message is rarely in the store, almost every one of them read
// "earlier message".
func TestAForumMessageThatIsNotAReplyHasNothingToQuote(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	m := &tg.Message{ID: 9, PeerID: &tg.PeerChannel{ChannelID: 4}, Message: "hi"}
	m.SetReplyTo(replyHeader(true, 7, 0))

	msg := c.messageFromTG(m)
	if msg.ReplyToMessageID != 0 {
		t.Errorf("ReplyToMessageID = %d, want 0: the message replies to nothing", msg.ReplyToMessageID)
	}
	if msg.TopicID != 7 {
		t.Errorf("TopicID = %d, want the header's topic 7", msg.TopicID)
	}
}

// A real reply still reads the way it always has; the fix must not cost
// the quote on the messages that have earned one.
func TestAReplyOutsideAForumStillNamesWhatItAnswers(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	m := &tg.Message{ID: 9, PeerID: &tg.PeerUser{UserID: 4}, Message: "hi"}
	m.SetReplyTo(replyHeader(false, 41, 0))

	msg := c.messageFromTG(m)
	if msg.ReplyToMessageID != 41 {
		t.Errorf("ReplyToMessageID = %d, want 41", msg.ReplyToMessageID)
	}
	if msg.TopicID != 0 {
		t.Errorf("TopicID = %d, want 0 outside a forum", msg.TopicID)
	}
}

// A join or pin notice belongs to a topic like any other message, but it
// has never drawn a reply quote and must not start now: nothing sets
// ReplyToMessageID on a service message today.
func TestAServiceMessageInATopicCarriesTheTopicAndQuotesNothing(t *testing.T) {
	c := &Client{files: newFileRegistry()}

	m := &tg.MessageService{
		ID:     9,
		PeerID: &tg.PeerChannel{ChannelID: 4},
		Action: &tg.MessageActionPinMessage{},
	}
	m.SetReplyTo(replyHeader(true, 41, 7))

	msg := c.messageFromTGService(m)
	if msg.TopicID != 7 {
		t.Errorf("TopicID = %d, want the header's topic 7", msg.TopicID)
	}
	if msg.ReplyToMessageID != 0 {
		t.Errorf("ReplyToMessageID = %d, want 0: a service message draws no reply quote", msg.ReplyToMessageID)
	}
}
