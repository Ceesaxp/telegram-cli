package telegram

import (
	"fmt"
	"math/rand"

	"github.com/gotd/td/tg"
)

// ForwardMessages forwards messages from one chat to another using
// Telegram's own forwarding RPC, and returns the copies that landed in the
// destination.
//
// It does not copy content. Re-sending the text, or downloading the media
// and uploading it again, produces a message that merely looks similar: it
// loses the "forwarded from" attribution, the original entities, the
// reply and media semantics, and — the part that matters — the server-side
// restriction on chats whose content is protected. Forwarding is a thing
// the server does, and asking it is the only way to get it.
//
// Attribution and captions are preserved (DropAuthor and DropMediaCaptions
// stay unset). A caller that wants an unattributed copy is asking for a
// different feature, and Telegram's own clients make it a separate choice
// rather than a default.
func (c *Client) ForwardMessages(fromChatID, toChatID int64, messageIDs []int64) ([]*Message, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	ctx, cancel := opCtx()
	defer cancel()

	from, err := c.inputPeer(ctx, fromChatID)
	if err != nil {
		return nil, fmt.Errorf("forward messages: source chat: %w", err)
	}
	to, err := c.inputPeer(ctx, toChatID)
	if err != nil {
		return nil, fmt.Errorf("forward messages: destination chat: %w", err)
	}

	// One random ID per message, and they must line up with ID positionally
	// — the server pairs them by index to deduplicate a retried request.
	randomIDs := make([]int64, len(messageIDs))
	for i := range randomIDs {
		randomIDs[i] = rand.Int63()
	}

	updates, err := c.api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: from,
		ToPeer:   to,
		ID:       int64sToInts(messageIDs),
		RandomID: randomIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("forward messages: %w", err)
	}

	return messagesFromUpdates(c, updates), nil
}

// messagesFromUpdates collects every new message in an update set, in
// arrival order.
//
// [messageFromUpdates] returns the first one, which is right for a send:
// one request, one message. A forward of N messages produces N, and
// keeping only the first would under-report what happened — the caller
// uses the count to say so.
func messagesFromUpdates(c *Client, updates tg.UpdatesClass) []*Message {
	var raw []tg.UpdateClass
	switch u := updates.(type) {
	case *tg.Updates:
		raw = u.Updates
	case *tg.UpdatesCombined:
		raw = u.Updates
	}

	out := make([]*Message, 0, len(raw))
	for _, upd := range raw {
		switch v := upd.(type) {
		case *tg.UpdateNewMessage:
			if m := c.messageClassFromTG(v.Message); m != nil {
				out = append(out, m)
			}
		case *tg.UpdateNewChannelMessage:
			if m := c.messageClassFromTG(v.Message); m != nil {
				out = append(out, m)
			}
		}
	}
	return out
}
