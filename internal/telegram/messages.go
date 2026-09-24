package telegram

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/tg"
)

// SendTextMessage sends a plain text message, optionally as a reply.
//
// placeholderID is the ID of the local echo the caller already put in the
// thread, or 0 for callers that draw nothing (the REST and MCP servers). It
// is handed straight back on the success message so the thread knows which
// row the confirmed message replaces.
//
// chatID may name a forum topic, and then the message is filed under that
// topic and comes back belonging to it rather than to the forum — see
// [Client.targetFor] and [replyHeaderFor] for what that takes.
func (c *Client) SendTextMessage(chatID int64, text string, replyToMessageID, placeholderID int64) (*Message, error) {
	msg, _, err := c.SendTextMessageWithMentions(chatID, text, nil, replyToMessageID, placeholderID)
	return msg, err
}

// SendTextMessageWithMentions is SendTextMessage for a draft that mentions
// users without a username (see MentionSpan). It also reports how many of
// the mentions were dropped and went out as plain text: the message itself
// is sent either way, so a drop is not an error, only something the caller
// may want to tell the user. The count is 0 whenever err is set.
func (c *Client) SendTextMessageWithMentions(chatID int64, text string, mentions []MentionSpan, replyToMessageID, placeholderID int64) (*Message, int, error) {
	ctx, cancel := opCtx()
	defer cancel()
	target, err := c.targetFor(ctx, chatID)
	if err != nil {
		return nil, 0, fmt.Errorf("send message: %w", err)
	}

	// Link previews are left to the server: NoWebpage stays unset so
	// markdown changes formatting only, never preview behaviour.
	body, entities, dropped := c.formatOutgoingWithMentions(ctx, text, mentions)
	req := &tg.MessagesSendMessageRequest{
		Peer:     target.peer,
		Message:  body,
		Entities: entities,
		RandomID: rand.Int63(),
	}
	req.ReplyTo = replyHeaderFor(target.topicID, replyToMessageID)

	updates, err := c.api.MessagesSendMessage(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("send message: %w", topicSendError(err))
	}

	msg := messageFromUpdates(c, updates)
	if msg == nil {
		// UpdateShortSentMessage carries no full message — build one from
		// what we know. It DOES carry the real message ID, so the copy
		// arriving later via the update dispatcher dedups correctly.
		// Echo what went on the wire, not what was typed: markers are
		// stripped and the entities run through the incoming converter,
		// so the local copy renders exactly like the server's.
		msg = &Message{
			ChatID:           chatID,
			IsOutgoing:       true,
			ReplyToMessageID: replyToMessageID,
			Content:          &MessageText{Text: formattedTextFromTG(body, asReceived(entities))},
		}
		if s, ok := updates.(*tg.UpdateShortSentMessage); ok {
			msg.ID = int64(s.ID)
			msg.Date = int32(s.Date)
		}
	}
	c.publishSent(target, msg, placeholderID)
	return msg, dropped, nil
}

// generalTopicID is the General topic, which every forum has and none can
// delete. It is the one topic that is named by NOT naming it: a send with
// no reply header lands there, so it is stripped from the rule below
// rather than sent.
const generalTopicID int64 = 1

// replyHeaderFor is the reply header a send carries, which is the one place
// this package decides both what a message replies to and which topic it is
// filed under.
//
// The rule, from core.telegram.org/api/forum and gotd's own note on the
// field — top_msg_id "must contain the topic ID only when replying to
// messages in forum topics different from the General topic":
//
//   - Outside a forum, the header is the reply and nothing else.
//   - A plain post into a topic is a reply to the topic's root message,
//     with no top_msg_id: that is what files it under the topic.
//   - A reply inside a topic names the message replied to, and the topic
//     on top, so a deleted target still lands in the right topic.
//   - General takes neither. A plain send lands there by having no header
//     at all, and a reply inside it is an ordinary reply.
//
// Text sends and media sends both need this and had a copy each, which was
// one copy too many for a rule with two dimensions.
func replyHeaderFor(topicID, replyToMessageID int64) tg.InputReplyToClass {
	if topicID == generalTopicID {
		topicID = 0
	}

	// What the message replies to: the target if there is one, and
	// otherwise the topic's root, which is the plain post into a topic.
	target := replyToMessageID
	if target == 0 {
		target = topicID
	}
	if target == 0 {
		// Returned as a nil interface rather than a nil pointer: the
		// request encodes the field whenever the interface is non-nil, and
		// a typed nil would put an empty header on the wire.
		return nil
	}

	header := &tg.InputReplyToMessage{ReplyToMsgID: int(target)}
	// Only when the two differ, per the rule above: replying to the topic's
	// own root message is the plain post, and naming the topic twice is
	// what gotd's note explicitly rules out.
	if topicID != 0 && target != topicID {
		header.SetTopMsgID(int(topicID))
	}
	return header
}

// chatTarget is the chat a call is about: the peer Telegram is asked about,
// the topic inside it, and the chat ID the CALLER named, which for a topic
// is its synthetic ID and for everything else is the peer again.
//
// The caller's ID is carried because the answer has to be handed back under
// it. The server talks about the forum — that is the peer it was asked
// about — while the reader, the local echo and the thread all sit in the
// topic's own chat, so an answer passed on as it came back would land in
// the forum's flat stream instead of the topic being read. See
// [chatTarget.filed] for the rewrite, and [Client.publishSent].
type chatTarget struct {
	peer    tg.InputPeerClass
	chatID  int64
	topicID int64
}

// targetFor resolves the chat a call names into what the wire needs: a
// topic's synthetic chat ID becomes its forum's peer and the topic inside
// it, and every other chat ID becomes itself with no topic.
//
// This is the split chokepoint for every path that both names a peer and
// hands a message back — sending, editing, reacting, pinning, forwarding,
// fetching a page of history — which is why they resolve their peer through
// it rather than calling inputPeer themselves.
func (c *Client) targetFor(ctx context.Context, chatID int64) (chatTarget, error) {
	real, topicID := c.splitTopic(chatID)
	peer, err := c.inputPeer(ctx, real)
	if err != nil {
		return chatTarget{}, err
	}
	return chatTarget{peer: peer, chatID: chatID, topicID: topicID}, nil
}

// filed is msg as it belongs to the chat the caller named.
//
// Every message the server says anything about in a forum belongs to the
// forum: that is the peer, and it is what [Client.messageClassFromTG] reads
// the chat ID off. The caller asked about a topic, everything it keys is
// keyed on the topic's own ID, and so this is where the two are reconciled
// — once, rather than at each of the call sites that has an answer to hand
// back.
//
// A chat that is not a topic is left exactly as the server wrote it, and so
// is a nil message: an absent answer is not something to rewrite.
func (t chatTarget) filed(msg *Message) *Message {
	if msg == nil || t.topicID == 0 {
		return msg
	}
	msg.ChatID = t.chatID
	msg.TopicID = t.topicID
	return msg
}

// publishSent announces a message this client just sent, under the chat ID
// the caller sent it to.
//
// The rewrite is the whole point. A message sent to a topic comes back from
// the server belonging to the forum — that is the peer it was sent to — but
// the placeholder it replaces sits in the topic's chat, and so does the
// reader. Published as it came back, the echo would never resolve and the
// message would appear in the forum's flat stream instead of the topic
// being read.
func (c *Client) publishSent(target chatTarget, msg *Message, placeholderID int64) {
	c.send(MessageSendSucceededMsg{Message: target.filed(msg), OldMessageId: placeholderID})
}

// topicSendError turns Telegram's refusal of a send into a topic into
// something a person can read, and leaves every other error alone.
//
// The text goes straight to the reader as a send failure, and TOPIC_CLOSED
// is not a sentence. The cause is dropped rather than wrapped for the same
// reason: what would be appended is "rpc error code 400: TOPIC_CLOSED",
// which is the noise this exists to replace.
func topicSendError(err error) error {
	switch {
	case tg.IsTopicClosed(err):
		return errors.New("that topic is closed — only its creator and the group's admins can post in it")
	case tg.IsTopicDeleted(err):
		return errors.New("that topic has been deleted")
	case tg.IsTopicIDInvalid(err):
		return errors.New("Telegram does not know that topic — the topic list is out of date")
	}
	return err
}

// EditTextMessage edits a text message.
func (c *Client) EditTextMessage(chatID int64, messageID int64, text string) (*Message, error) {
	msg, _, err := c.EditTextMessageWithMentions(chatID, messageID, text, nil)
	return msg, err
}

// EditTextMessageWithMentions is EditTextMessage for text that mentions
// users without a username (see MentionSpan). An edit replaces all of a
// message's entities, so mentions kept through the edit must be passed
// again. Dropped mentions are counted as in SendTextMessageWithMentions.
//
// Editing a message inside a forum topic edits it where it lives, which is
// the forum: a topic's messages are the forum's channel messages, and there
// is no topic to name because the message names itself. What the topic DOES
// decide is where the answer goes — the edited message comes back belonging
// to the forum, and the thread waiting to redraw the row is the topic's, so
// it is handed back filed under the chat the caller named. Both ways out
// take that filing: the message the updates carry, and the refetch for the
// answers that carry none.
func (c *Client) EditTextMessageWithMentions(chatID int64, messageID int64, text string, mentions []MentionSpan) (*Message, int, error) {
	ctx, cancel := opCtx()
	defer cancel()
	target, err := c.targetFor(ctx, chatID)
	if err != nil {
		return nil, 0, fmt.Errorf("edit message: %w", err)
	}

	body, entities, dropped := c.formatOutgoingWithMentions(ctx, text, mentions)
	updates, err := c.api.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
		Peer:     target.peer,
		ID:       int(messageID),
		Message:  body,
		Entities: entities,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("edit message: %w", err)
	}

	if msg := messageFromUpdates(c, updates); msg != nil {
		return target.filed(msg), dropped, nil
	}
	msg, err := c.GetMessage(chatID, messageID)
	if err != nil {
		return nil, 0, err
	}
	return target.filed(msg), dropped, nil
}

// DeleteMessages deletes messages from a chat.
//
// revoke asks Telegram to delete the messages for everyone rather than
// only for the current user. It is IGNORED for channels and supergroups:
// channels.deleteMessages has no such flag because channel deletions are
// always for everyone.
//
// On success the deletion is published immediately so the UI does not
// wait for the server echo. The server sends its own update shortly
// after; the message store deletes by filtering on an ID set, so
// applying the same deletion twice is a no-op.
//
// The split is [Client.GetMessages]'s, for the same reason and against the
// same hole: a topic's messages belong to the forum's channel, and the
// peerless call the unsplit branch would take deletes by the account's own
// numbering. An ID the split could not translate is refused outright rather
// than carried on with — see unallocatedTopic for why the refusal cannot be
// left to inputPeer here.
func (c *Client) DeleteMessages(chatID int64, messageIDs []int64, revoke bool) error {
	if len(messageIDs) == 0 {
		return nil
	}

	real, _ := c.splitTopic(chatID)
	if err := refuseTopic(real, unallocatedTopic); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}

	ctx, cancel := opCtx()
	defer cancel()

	ids := int64sToInts(messageIDs)

	if constant.TDLibPeerID(real).IsChannel() {
		peer, err := c.inputPeer(ctx, real)
		if err != nil {
			return fmt.Errorf("delete messages: %w", err)
		}
		inputChannel, ok := peerAsInputChannel(peer)
		if !ok {
			return fmt.Errorf("delete messages: peer %d is not a channel", real)
		}
		if _, err := c.api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{
			Channel: inputChannel,
			ID:      ids,
		}); err != nil {
			return fmt.Errorf("delete messages: %w", err)
		}
	} else {
		// messages.deleteMessages takes no peer: for users and basic
		// groups message IDs are unique account-wide.
		if _, err := c.api.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{
			Revoke: revoke,
			ID:     ids,
		}); err != nil {
			return fmt.Errorf("delete messages: %w", err)
		}
	}

	c.send(deletedMsgFor(chatID, messageIDs))
	return nil
}

// deletedMsgFor builds the deletion event for a chat, matching the shape
// the update listener emits so consumers need only one code path.
// Channel deletions name their chat; non-channel ones carry ChatId 0,
// because the corresponding server update has no peer and the store
// resolves them via DeleteFromAll.
//
// A forum topic names its chat too, and it has to be asked for separately:
// a synthetic chat ID answers NO to IsChannel — deliberately, so that a
// stray one takes the peer path and is refused there — so the kind test
// alone left a deletion in a topic carrying no chat at all. Zero is not
// "no chat" to the thread, it is EVERY chat, and message IDs collide
// freely between chats: deleting message 4521 in a topic took message 4521
// out of every other conversation loaded that happened to have one.
//
// The ID published is the caller's, which for a topic is the synthetic one
// — the same rule publishSent and messageFiledUnderItsTopic follow, and for
// the same reason: the thread, the row and the store are all keyed by it.
// The forum's own copy of the message is left to the server's echo, which
// announces the deletion for the forum and for its topics alike.
func deletedMsgFor(chatID int64, messageIDs []int64) MessageDeletedMsg {
	msg := MessageDeletedMsg{MessageIds: messageIDs}
	if isSyntheticChatID(chatID) || constant.TDLibPeerID(chatID).IsChannel() {
		msg.ChatId = chatID
	}
	return msg
}

// int64sToInts narrows message IDs for the tg request types.
func int64sToInts(ids []int64) []int {
	out := make([]int, len(ids))
	for i, id := range ids {
		out[i] = int(id)
	}
	return out
}

// GetMessage fetches a single message.
func (c *Client) GetMessage(chatID, messageID int64) (*Message, error) {
	msgs, err := c.GetMessages(chatID, []int64{messageID})
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		if m.ID == messageID {
			return m, nil
		}
	}
	return nil, fmt.Errorf("message %d not found in chat %d", messageID, chatID)
}

// GetMessages fetches several messages of one chat in a single request.
//
// Both messages.getMessages and channels.getMessages take a list, and the
// callers that refetch after an edit, a reaction or a poll vote have a list
// — one request per update is the shape Telegram answers with FLOOD_WAIT.
// [GetMessage] is this with a list of one.
//
// Messages the server did not return are simply absent from the result: a
// message deleted between the update and the fetch is a normal race, not an
// error the caller can do anything about.
//
// A topic's messages are the forum's channel messages, under the forum's
// own numbering, so the split has to come before the branch below: a
// synthetic chat ID is not a channel, and the peerless call it would
// otherwise take names no chat at all and would answer with whatever the
// account's own numbering has at those IDs. An ID the split could not
// translate is refused for the same reason — that branch resolves no peer,
// so nothing downstream would ever say no to it.
func (c *Client) GetMessages(chatID int64, messageIDs []int64) ([]*Message, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	real, topicID := c.splitTopic(chatID)
	if err := refuseTopic(real, unallocatedTopic); err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}

	ctx, cancel := opCtx()
	defer cancel()

	ids := make([]tg.InputMessageClass, 0, len(messageIDs))
	for _, id := range messageIDs {
		ids = append(ids, &tg.InputMessageID{ID: int(id)})
	}

	var messages []tg.MessageClass
	if constant.TDLibPeerID(real).IsChannel() {
		peer, err := c.inputPeer(ctx, real)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		inputChannel, ok := peerAsInputChannel(peer)
		if !ok {
			return nil, fmt.Errorf("get messages: peer %d is not a channel", real)
		}
		res, err := c.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: inputChannel,
			ID:      ids,
		})
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		messages = messagesFromMessagesClass(res)
	} else {
		res, err := c.api.MessagesGetMessages(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("get messages: %w", err)
		}
		messages = messagesFromMessagesClass(res)
	}

	// Filed under the chat the caller named, like a page of history and
	// like a message that arrived live. This is the fetch the thread
	// refetches through after an edit or a reaction, and it writes what it
	// gets back into the store under the topic's key — so a message
	// labelled with the forum would change chats the moment anyone touched
	// it.
	return c.messagesFiledIn(chatTarget{chatID: chatID, topicID: topicID}, messages), nil
}

// convertedMessages converts a page of fetched messages, dropping the ones
// that convert to nothing — an empty message is a hole the server left
// where a deleted one used to be, not a row.
//
// One loop rather than the copy every fetching call used to carry: history,
// both searches, the media rail and the by-ID fetch all answer with a page
// of tg messages and all want the same domain messages out of it.
func (c *Client) convertedMessages(messages []tg.MessageClass) []*Message {
	out := make([]*Message, 0, len(messages))
	for _, mc := range messages {
		if m := c.messageClassFromTG(mc); m != nil {
			out = append(out, m)
		}
	}
	return out
}

// messagesFiledIn converts a page of fetched messages and files each one
// under the chat the caller asked about.
//
// The filing is what makes a page fetched for a topic agree with the rest of
// the client. A message in a forum names the forum as its peer whichever
// topic it sits in, so a page of a topic's history, a search of one or its
// media rail all came back labelled with the forum — while the same message
// arriving live, or echoed back from a send, came back labelled with the
// topic. One message cannot be in two chats depending on how it was
// fetched.
//
// After conversion rather than during it, deliberately: a message with no
// from_id takes its sender from its chat ID (see [Client.messageFromTG]), so
// a topic's ID pushed in earlier would rename every anonymous admin and
// channel post in the forum to a chat nothing has a title for.
func (c *Client) messagesFiledIn(target chatTarget, messages []tg.MessageClass) []*Message {
	out := c.convertedMessages(messages)
	for _, m := range out {
		target.filed(m)
	}
	return out
}

// messagesFromMessagesClass extracts the message list from a
// messages.Messages response.
func messagesFromMessagesClass(res tg.MessagesMessagesClass) []tg.MessageClass {
	switch r := res.(type) {
	case *tg.MessagesMessages:
		return r.Messages
	case *tg.MessagesMessagesSlice:
		return r.Messages
	case *tg.MessagesChannelMessages:
		return r.Messages
	default:
		return nil
	}
}

// messageFromUpdates extracts the first real message from an updates result
// (the response of send/edit message calls).
func messageFromUpdates(c *Client, updates tg.UpdatesClass) *Message {
	var msgs []tg.UpdateClass
	switch u := updates.(type) {
	case *tg.Updates:
		msgs = u.Updates
	case *tg.UpdatesCombined:
		msgs = u.Updates
	case *tg.UpdateShortSentMessage:
		return nil // no full message; the dispatcher will deliver it
	}
	for _, upd := range msgs {
		switch v := upd.(type) {
		case *tg.UpdateNewMessage:
			if m := c.messageClassFromTG(v.Message); m != nil {
				return m
			}
		case *tg.UpdateNewChannelMessage:
			if m := c.messageClassFromTG(v.Message); m != nil {
				return m
			}
		case *tg.UpdateEditMessage:
			if m := c.messageClassFromTG(v.Message); m != nil {
				return m
			}
		case *tg.UpdateEditChannelMessage:
			if m := c.messageClassFromTG(v.Message); m != nil {
				return m
			}
		}
	}
	return nil
}
