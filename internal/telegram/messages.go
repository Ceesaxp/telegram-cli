package telegram

import (
	"fmt"
	"math/rand"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/tg"
)

// SendTextMessage sends a plain text message, optionally as a reply.
func (c *Client) SendTextMessage(chatID int64, text string, replyToMessageID int64) (*Message, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	req := &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  text,
		RandomID: rand.Int63(),
	}
	if replyToMessageID != 0 {
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: int(replyToMessageID)}
	}

	updates, err := c.api.MessagesSendMessage(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	msg := messageFromUpdates(c, updates)
	if msg == nil {
		// UpdateShortSentMessage carries no full message — build one from
		// what we know. It DOES carry the real message ID, so the copy
		// arriving later via the update dispatcher dedups correctly.
		msg = &Message{
			ChatID:           chatID,
			IsOutgoing:       true,
			ReplyToMessageID: replyToMessageID,
			Content:          &MessageText{Text: &FormattedText{Text: text}},
		}
		if s, ok := updates.(*tg.UpdateShortSentMessage); ok {
			msg.ID = int64(s.ID)
			msg.Date = int32(s.Date)
		}
	}
	c.send(MessageSendSucceededMsg{Message: msg, OldMessageId: 0})
	return msg, nil
}

// EditTextMessage edits a text message.
func (c *Client) EditTextMessage(chatID int64, messageID int64, text string) (*Message, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("edit message: %w", err)
	}

	updates, err := c.api.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{
		Peer:    peer,
		ID:      int(messageID),
		Message: text,
	})
	if err != nil {
		return nil, fmt.Errorf("edit message: %w", err)
	}

	if msg := messageFromUpdates(c, updates); msg != nil {
		return msg, nil
	}
	return c.GetMessage(chatID, messageID)
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
func (c *Client) DeleteMessages(chatID int64, messageIDs []int64, revoke bool) error {
	if len(messageIDs) == 0 {
		return nil
	}

	ctx, cancel := opCtx()
	defer cancel()

	ids := int64sToInts(messageIDs)

	if constant.TDLibPeerID(chatID).IsChannel() {
		peer, err := c.inputPeer(ctx, chatID)
		if err != nil {
			return fmt.Errorf("delete messages: %w", err)
		}
		inputChannel, ok := peerAsInputChannel(peer)
		if !ok {
			return fmt.Errorf("delete messages: peer %d is not a channel", chatID)
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
func deletedMsgFor(chatID int64, messageIDs []int64) MessageDeletedMsg {
	msg := MessageDeletedMsg{MessageIds: messageIDs}
	if constant.TDLibPeerID(chatID).IsChannel() {
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
	ctx, cancel := opCtx()
	defer cancel()

	var messages []tg.MessageClass
	if constant.TDLibPeerID(chatID).IsChannel() {
		peer, err := c.inputPeer(ctx, chatID)
		if err != nil {
			return nil, fmt.Errorf("get message: %w", err)
		}
		inputChannel, ok := peerAsInputChannel(peer)
		if !ok {
			return nil, fmt.Errorf("get message: peer %d is not a channel", chatID)
		}
		res, err := c.api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: inputChannel,
			ID:      []tg.InputMessageClass{&tg.InputMessageID{ID: int(messageID)}},
		})
		if err != nil {
			return nil, fmt.Errorf("get message: %w", err)
		}
		messages = messagesFromMessagesClass(res)
	} else {
		res, err := c.api.MessagesGetMessages(ctx, []tg.InputMessageClass{
			&tg.InputMessageID{ID: int(messageID)},
		})
		if err != nil {
			return nil, fmt.Errorf("get message: %w", err)
		}
		messages = messagesFromMessagesClass(res)
	}

	for _, mc := range messages {
		if m := c.messageClassFromTG(mc); m != nil && m.ID == messageID {
			return m, nil
		}
	}
	return nil, fmt.Errorf("message %d not found in chat %d", messageID, chatID)
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
