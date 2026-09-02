package telegram

import (
	"fmt"

	"github.com/gotd/td/tg"
)

// This file is the two things you can DO to somebody else's message that are
// not replying to it: react to it, and pin it. Both are one request and no
// local state — the server answers with an update, and the message comes
// back through the same refetch an edit takes.

// SetReaction puts your reaction on a message, or takes it off.
//
// An empty emoji REMOVES. Telegram models a reaction the same way: the
// request carries a list, and an empty list means "none of them are mine
// any more" — there is no separate removal call, and inventing one here
// would be two names for one request.
//
// One reaction at a time, which is what a non-premium account is allowed.
// The request would take several; sending several to an account that cannot
// have them fails the whole call, and failing at the far end of a round trip
// is a worse way to learn about a limit than not offering it.
func (c *Client) SetReaction(chatID, messageID int64, emoji string) error {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return fmt.Errorf("react: %w", err)
	}

	req := &tg.MessagesSendReactionRequest{
		Peer:  peer,
		MsgID: int(messageID),
		// Recent reactions are the row Telegram's own clients put at the
		// front of the picker. This one has a fixed set, so adding to a
		// list nothing reads would be a side effect on the account for no
		// effect on the screen.
		AddToRecent: false,
	}
	if emoji != "" {
		req.Reaction = []tg.ReactionClass{&tg.ReactionEmoji{Emoticon: emoji}}
	}

	if _, err := c.api.MessagesSendReaction(ctx, req); err != nil {
		return fmt.Errorf("react: %w", err)
	}
	return nil
}

// SetPinned pins a message, or unpins it.
//
// Silent, always. Pinning normally posts a service message into the chat
// announcing it — "X pinned a message" — and a client whose pin key also
// writes a line into everybody's history is a key people learn not to press.
// The pin itself is what was asked for; the announcement is not.
func (c *Client) SetPinned(chatID, messageID int64, pinned bool) error {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return fmt.Errorf("pin: %w", err)
	}

	req := &tg.MessagesUpdatePinnedMessageRequest{
		Peer:   peer,
		ID:     int(messageID),
		Silent: true,
		Unpin:  !pinned,
	}
	if _, err := c.api.MessagesUpdatePinnedMessage(ctx, req); err != nil {
		if pinned {
			return fmt.Errorf("pin: %w", err)
		}
		return fmt.Errorf("unpin: %w", err)
	}
	return nil
}
