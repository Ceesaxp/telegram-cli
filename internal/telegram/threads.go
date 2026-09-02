package telegram

import (
	"fmt"

	"github.com/gotd/td/tg"
)

// commentsFromTG converts a message's reply header into the discussion
// under it, or nil when there is not one.
//
// The `comments` flag is the whole distinction. Telegram uses one structure
// for two different things: the comment count on a CHANNEL POST, which
// lives in a group linked to the channel, and the reply count on a GROUP
// message, which is a thread inside the group itself. Only the first has
// somewhere else to go, and only the first is what a channel reader is
// looking for.
func commentsFromTG(r tg.MessageReplies) *Comments {
	if !r.Comments {
		return nil
	}

	comments := &Comments{Count: int32(r.Replies)}
	if channelID, ok := r.GetChannelID(); ok {
		comments.ChatID = channelChatID(channelID)
	}

	// Unread against the account's own read position. Telegram sends both
	// halves or neither; without the read mark there is nothing to compare
	// and "unread" would be a guess about somebody's attention.
	maxID, haveMax := r.GetMaxID()
	readID, haveRead := r.GetReadMaxID()
	comments.Unread = haveMax && haveRead && maxID > readID

	return comments
}

// DiscussionMessage finds where a channel post's comments live: the chat
// and the message inside it that the comments hang off.
//
// A post's comments are not in the channel. Telegram copies the post into
// the linked group and the comments are replies to that copy, so opening
// them means opening a different chat at a message whose id this client has
// never seen. This is the only call that knows the translation.
//
// The linked group is announced as a side effect, the same way
// CreatePrivateChat announces a chat it resolved: the caller is about to
// open it, and a chat opening with no name is a chat that looks broken for
// as long as the resolve takes.
//
// Announced through GetChat rather than off the Chats the response already
// carries. Those are peers, and a chat built from a peer carries Muted=false
// — which the store would merge over a group the reader had muted on
// purpose. That is divergence 39 exactly, and the AST guard beside it
// refused the shortcut when this was first written that way.
func (c *Client) DiscussionMessage(chatID, messageID int64) (int64, int64, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return 0, 0, fmt.Errorf("open comments: %w", err)
	}

	res, err := c.api.MessagesGetDiscussionMessage(ctx, &tg.MessagesGetDiscussionMessageRequest{
		Peer:  peer,
		MsgID: int(messageID),
	})
	if err != nil {
		return 0, 0, fmt.Errorf("open comments: %w", err)
	}

	// The first message is the post's copy in the linked group; anything
	// after it is the chain above it, which is not where the reader is
	// going.
	for _, m := range res.Messages {
		converted := c.messageClassFromTG(m)
		if converted == nil || converted.ChatID == 0 {
			continue
		}
		c.announceChat(converted.ChatID)
		return converted.ChatID, converted.ID, nil
	}
	return 0, 0, fmt.Errorf("open comments: the discussion has no message to open at")
}

// announceChat tells the app about a chat it is about to be sent to.
//
// Best effort: a failure here costs a title, and the caller has somewhere
// to go either way. Reporting it would put an error on screen about the
// name of a chat that is opening in front of the reader.
func (c *Client) announceChat(chatID int64) {
	chat, err := c.GetChat(chatID)
	if err != nil {
		return
	}
	c.send(peerChatUpdate(chat))
}
