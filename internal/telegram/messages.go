package telegram

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/tg"
)

// SendTextMessage sends a plain text message, optionally as a reply.
func (c *Client) SendTextMessage(chatID int64, text string, replyToMessageID int64) (*Message, error) {
	ctx := context.Background()
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
		// The update will also arrive via the update dispatcher.
		return &Message{
			ChatID:     chatID,
			IsOutgoing: true,
			Content:    &MessageText{Text: &FormattedText{Text: text}},
		}, nil
	}
	c.send(MessageSendSucceededMsg{Message: msg, OldMessageId: 0})
	return msg, nil
}

// EditTextMessage edits a text message.
func (c *Client) EditTextMessage(chatID int64, messageID int64, text string) (*Message, error) {
	ctx := context.Background()
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

// GetMessage fetches a single message.
func (c *Client) GetMessage(chatID, messageID int64) (*Message, error) {
	ctx := context.Background()

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
