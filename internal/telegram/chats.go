package telegram

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// LoadChats fetches the dialog list and pushes every chat to the UI
// as a ChatUpdateMsg (this replaces tdlib's updateNewChat flow).
func (c *Client) LoadChats(limit int) error {
	chats, err := c.ListChats(limit)
	if err != nil {
		return err
	}

	var totalUnread, unmutedUnread int32
	for _, chat := range chats {
		totalUnread += chat.UnreadCount
		if !chat.Muted {
			unmutedUnread += chat.UnreadCount
		}
		c.send(ChatUpdateMsg{Chat: chat})
	}

	c.send(UnreadCountMsg{
		UnreadCount:        totalUnread,
		UnreadUnmutedCount: unmutedUnread,
	})
	return nil
}

// dialogsPageSize is the largest dialog page Telegram will return.
const dialogsPageSize = 100

// MaxDialogsLimit caps ListChats so a bad limit cannot walk the whole
// dialog list. The TUI first paint only loads one recency page; folder
// membership comes from include/pin peers, not from this cap.
const MaxDialogsLimit = 500

// dialogCursor is the pagination state of MessagesGetDialogs: the date
// and ID of the last dialog's top message plus that dialog's peer.
type dialogCursor struct {
	date int
	id   int
	peer tg.InputPeerClass
}

// ListChats fetches the dialog list without emitting any UI events.
// Telegram returns at most 100 dialogs per request, so larger limits are
// served by paginating until limit is reached or a short page arrives.
func (c *Client) ListChats(limit int) ([]*Chat, error) {
	if limit <= 0 {
		limit = dialogsPageSize
	}
	if limit > MaxDialogsLimit {
		limit = MaxDialogsLimit
	}

	var (
		out    []*Chat
		seen   = make(map[int64]bool, limit)
		cursor = dialogCursor{peer: &tg.InputPeerEmpty{}}
	)

	for len(out) < limit {
		pageLimit := limit - len(out)
		if pageLimit > dialogsPageSize {
			pageLimit = dialogsPageSize
		}

		page, next, raw, err := c.listChatsPage(cursor, pageLimit)
		if err != nil {
			return nil, err
		}

		for _, chat := range page {
			if seen[chat.ID] {
				continue
			}
			seen[chat.ID] = true
			out = append(out, chat)
			if len(out) == limit {
				break
			}
		}

		// A short page is the end of the list. next.peer is nil when the
		// page held no usable dialog to continue from, and an unchanged
		// cursor would loop forever.
		if raw < pageLimit || next.peer == nil ||
			(next.date == cursor.date && next.id == cursor.id) {
			break
		}
		cursor = next
	}
	return out, nil
}

// listChatsPage fetches one page of dialogs. It returns the converted
// chats, the cursor for the next page, and the number of raw dialogs the
// server sent (which is what tells a short — i.e. final — page apart).
func (c *Client) listChatsPage(cursor dialogCursor, limit int) ([]*Chat, dialogCursor, int, error) {
	ctx, cancel := opCtx()
	defer cancel()

	var next dialogCursor

	res, err := c.api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
		Limit:      limit,
		OffsetDate: cursor.date,
		OffsetID:   cursor.id,
		OffsetPeer: cursor.peer,
	})
	if err != nil {
		return nil, next, 0, fmt.Errorf("get dialogs: %w", err)
	}

	var (
		dialogs  []tg.DialogClass
		messages []tg.MessageClass
		chats    []tg.ChatClass
		users    []tg.UserClass
		complete bool
	)
	switch d := res.(type) {
	case *tg.MessagesDialogs:
		dialogs, messages, chats, users = d.Dialogs, d.Messages, d.Chats, d.Users
		complete = true // the server returned the entire list
	case *tg.MessagesDialogsSlice:
		dialogs, messages, chats, users = d.Dialogs, d.Messages, d.Chats, d.Users
	default:
		return nil, next, 0, fmt.Errorf("unexpected dialogs type %T", res)
	}

	// Seed the peers manager so access hashes are known. This must happen
	// for every page, not just the first.
	if err := c.peers.Apply(ctx, users, chats); err != nil {
		return nil, next, 0, fmt.Errorf("apply peers: %w", err)
	}

	out, lastMessages, entities := c.chatsFromDialogParts(dialogs, messages, users, chats)
	for _, dc := range dialogs {
		d, ok := dc.(*tg.Dialog)
		if !ok {
			continue
		}

		// Advance the cursor even for dialogs we cannot convert into a
		// Chat, otherwise one unknown peer would stall pagination. All
		// three fields must come from the SAME dialog — a cursor mixing
		// this page's ID with the previous page's date re-requests
		// dialogs we already have. Dialogs whose top message is missing
		// from the response are skipped for cursor purposes; the last
		// one that does have a date wins, and if none does, next.peer
		// stays nil and the caller stops.
		if !complete {
			if lm, ok := lastMessages[chatIDFromPeer(d.Peer)]; ok {
				if peer, ok := inputPeerFromEntities(d.Peer, entities); ok {
					next = dialogCursor{
						date: int(lm.Date),
						id:   d.TopMessage,
						peer: peer,
					}
				}
			}
		}
	}

	if complete {
		// Nothing left to page through: report a short page.
		return out, dialogCursor{}, 0, nil
	}
	return out, next, len(dialogs), nil
}

// chatsFromDialogParts converts a dialogs payload into domain chats.
func (c *Client) chatsFromDialogParts(dialogs []tg.DialogClass, messages []tg.MessageClass, users []tg.UserClass, chats []tg.ChatClass) ([]*Chat, map[int64]*Message, tg.Entities) {
	entities := tg.Entities{
		Users:    make(map[int64]*tg.User, len(users)),
		Chats:    make(map[int64]*tg.Chat, len(chats)),
		Channels: make(map[int64]*tg.Channel, len(chats)),
	}
	for _, uc := range users {
		if u, ok := uc.(*tg.User); ok {
			entities.Users[u.ID] = u
		}
	}
	for _, cc := range chats {
		switch v := cc.(type) {
		case *tg.Chat:
			entities.Chats[v.ID] = v
		case *tg.Channel:
			entities.Channels[v.ID] = v
		}
	}

	lastMessages := make(map[int64]*Message, len(messages))
	for _, mc := range messages {
		if m := c.messageClassFromTG(mc); m != nil {
			lastMessages[m.ChatID] = m
		}
	}

	now := time.Now().Unix()
	out := make([]*Chat, 0, len(dialogs))
	for _, dc := range dialogs {
		d, ok := dc.(*tg.Dialog)
		if !ok {
			continue
		}
		chat, err := c.chatFromPeer(d.Peer, entities)
		if err != nil {
			continue
		}
		chat.Pinned = d.Pinned
		chat.UnreadCount = int32(d.UnreadCount)
		chat.LastReadInboxMessageID = int64(d.ReadInboxMaxID)
		chat.LastReadOutboxMessageID = int64(d.ReadOutboxMaxID)
		chat.Muted = mutedFromNotifySettings(d.NotifySettings, now)
		if lm, ok := lastMessages[chat.ID]; ok {
			chat.LastMessage = lm
			chat.Order = int64(lm.Date)
		}
		out = append(out, chat)
	}
	return out, lastMessages, entities
}

// inputPeerFromEntities builds an InputPeer (access hash included) from a
// peer already present in the response entities.
func inputPeerFromEntities(p tg.PeerClass, e tg.Entities) (tg.InputPeerClass, bool) {
	switch v := p.(type) {
	case *tg.PeerUser:
		if u, ok := e.Users[v.UserID]; ok {
			return &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}, true
		}
	case *tg.PeerChat:
		if ch, ok := e.Chats[v.ChatID]; ok {
			return &tg.InputPeerChat{ChatID: ch.ID}, true
		}
	case *tg.PeerChannel:
		if ch, ok := e.Channels[v.ChannelID]; ok {
			return &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}, true
		}
	}
	return nil, false
}

// GetChat returns a single chat by canonical chat ID.
//
// The mute flag comes from a second call, because resolving a peer does not
// report it: notify settings belong to the account's view of the peer, not
// to the peer. Without it a chat outside the loaded dialog page would look
// unmuted to everything downstream, and the first message from it would ring
// — which is the whole reason anybody looks at this function.
func (c *Client) GetChat(chatID int64) (*Chat, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.peers.ResolveTDLibID(ctx, constant.TDLibPeerID(chatID))
	if err != nil {
		return nil, fmt.Errorf("get chat %d: %w", chatID, err)
	}

	var chat *Chat
	switch p := peer.(type) {
	case peers.User:
		chat = c.chatFromUser(p.Raw())
	case peers.Chat:
		chat = c.chatFromBasicGroup(p.Raw())
	case peers.Channel:
		chat = c.chatFromChannel(p.Raw())
	default:
		return nil, fmt.Errorf("get chat %d: unexpected peer type %T", chatID, peer)
	}

	chat.Muted = peerMuted(ctx, c.api, peer.InputPeer())
	return chat, nil
}

// notifySettingsGetter is the one call [peerMuted] makes, so it can be
// tested without a connection.
type notifySettingsGetter interface {
	AccountGetNotifySettings(context.Context, tg.InputNotifyPeerClass) (*tg.PeerNotifySettings, error)
}

// peerMuted reads the account's notify settings for one peer.
//
// A failure means "not muted". The settings are an extra round trip on top
// of an answer the caller already has, so failing the whole chat because the
// mute flag could not be read would be the worse trade — and of the two ways
// to be wrong, ringing for a chat that turns out to be silenced is the one a
// messaging client is allowed to pick. Assuming muted would hide messages.
func peerMuted(ctx context.Context, api notifySettingsGetter, peer tg.InputPeerClass) bool {
	settings, err := api.AccountGetNotifySettings(ctx, &tg.InputNotifyPeer{Peer: peer})
	if err != nil {
		log.Printf("notify settings: %s", err)
		return false
	}
	return mutedFromNotifySettings(*settings, time.Now().Unix())
}

// peerChatUpdate is the message for a chat built by resolving a PEER.
//
// Both peer-derived senders go through this rather than writing the literal
// out, so "a peer view is partial" is one fact in one place. It was two call
// sites before, and a flag that two call sites have to remember is a flag
// one of them eventually forgets — which is the shape of the bug this whole
// change exists to fix.
func peerChatUpdate(chat *Chat) ChatUpdateMsg {
	return ChatUpdateMsg{Chat: chat, FromPeer: true}
}

// GetChatHistory returns messages of a chat, newest first.
// fromMessageID paginates backwards (offsetID); offset skips messages.
func (c *Client) GetChatHistory(chatID, fromMessageID int64, offset, limit int32) ([]*Message, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	res, err := c.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:       peer,
		OffsetID:   int(fromMessageID),
		OffsetDate: 0,
		AddOffset:  int(offset),
		Limit:      int(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}

	var messages []tg.MessageClass
	switch r := res.(type) {
	case *tg.MessagesMessages:
		messages = r.Messages
	case *tg.MessagesMessagesSlice:
		messages = r.Messages
	case *tg.MessagesChannelMessages:
		messages = r.Messages
	default:
		return nil, fmt.Errorf("unexpected history type %T", res)
	}

	out := make([]*Message, 0, len(messages))
	for _, mc := range messages {
		if m := c.messageClassFromTG(mc); m != nil {
			out = append(out, m)
		}
	}
	return out, nil
}

// SearchChats searches chat titles by query (server-side).
func (c *Client) SearchChats(query string, limit int32) ([]*Chat, error) {
	ctx, cancel := opCtx()
	defer cancel()
	if limit <= 0 {
		limit = 20
	}

	res, err := c.api.ContactsSearch(ctx, &tg.ContactsSearchRequest{
		Q:     query,
		Limit: int(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search chats: %w", err)
	}

	found := res

	entities := tg.Entities{
		Users:    make(map[int64]*tg.User),
		Chats:    make(map[int64]*tg.Chat),
		Channels: make(map[int64]*tg.Channel),
	}
	for _, uc := range found.Users {
		if u, ok := uc.(*tg.User); ok {
			entities.Users[u.ID] = u
		}
	}
	for _, cc := range found.Chats {
		switch v := cc.(type) {
		case *tg.Chat:
			entities.Chats[v.ID] = v
		case *tg.Channel:
			entities.Channels[v.ID] = v
		}
	}

	out := make([]*Chat, 0, len(found.MyResults)+len(found.Results))
	for _, peer := range append(found.MyResults, found.Results...) {
		if chat, err := c.chatFromPeer(peer, entities); err == nil {
			out = append(out, chat)
		}
	}
	return out, nil
}

// SearchMessages searches messages globally by query.
func (c *Client) SearchMessages(query string, limit int32) ([]*Message, error) {
	ctx, cancel := opCtx()
	defer cancel()
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	res, err := c.api.MessagesSearchGlobal(ctx, &tg.MessagesSearchGlobalRequest{
		Q:          query,
		Filter:     &tg.InputMessagesFilterEmpty{},
		OffsetPeer: &tg.InputPeerEmpty{},
		Limit:      int(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}

	var messages []tg.MessageClass
	switch r := res.(type) {
	case *tg.MessagesMessages:
		messages = r.Messages
	case *tg.MessagesMessagesSlice:
		messages = r.Messages
	case *tg.MessagesChannelMessages:
		messages = r.Messages
	default:
		return nil, fmt.Errorf("unexpected search type %T", res)
	}

	out := make([]*Message, 0, len(messages))
	for _, mc := range messages {
		if m := c.messageClassFromTG(mc); m != nil {
			out = append(out, m)
		}
	}
	return out, nil
}

// SearchChatMessages searches messages within a single chat, newest
// first, like GetChatHistory.
//
// fromMessageID is the pagination offset: 0 starts from the latest
// message, and paging back means passing the ID of the oldest message
// already seen. limit is clamped to 1..100 (0 or negative means 100,
// matching the sibling search and history methods).
//
// One RPC covers every chat kind — messages.search takes the peer, so
// users, basic groups, supergroups and channels all route through
// c.inputPeer with no channel-specific variant.
//
// An empty query returns an error rather than a guaranteed server-side
// SEARCH_QUERY_EMPTY round trip.
func (c *Client) SearchChatMessages(chatID int64, query string, fromMessageID int64, limit int32) ([]*Message, error) {
	if query == "" {
		return nil, fmt.Errorf("search chat messages: empty query")
	}

	ctx, cancel := opCtx()
	defer cancel()

	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("search chat messages: %w", err)
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	res, err := c.api.MessagesSearch(ctx, &tg.MessagesSearchRequest{
		Peer:     peer,
		Q:        query,
		Filter:   &tg.InputMessagesFilterEmpty{},
		OffsetID: int(fromMessageID),
		Limit:    int(limit),
		// The remaining bounds are deliberately zero: no date range, no
		// extra offset, no min/max ID clamp, and no result hash. These
		// are plain (non-flag) fields, so zero reaches the wire as
		// "unbounded" rather than "absent".
		MinDate:   0,
		MaxDate:   0,
		AddOffset: 0,
		MaxID:     0,
		MinID:     0,
		Hash:      0,
	})
	if err != nil {
		return nil, fmt.Errorf("search chat messages: %w", err)
	}

	messages := messagesFromMessagesClass(res)
	out := make([]*Message, 0, len(messages))
	for _, mc := range messages {
		if m := c.messageClassFromTG(mc); m != nil {
			out = append(out, m)
		}
	}
	return out, nil
}

// OpenChat is a light-weight placeholder kept for API compatibility:
// gotd needs no open/close chat lifecycle. It emits the chat so the
// store has it even for chats outside the loaded dialogs.
func (c *Client) OpenChat(chatID int64) error {
	chat, err := c.GetChat(chatID)
	if err != nil {
		return err
	}
	c.send(peerChatUpdate(chat))
	return nil
}

// ViewMessages marks messages as read.
func (c *Client) ViewMessages(chatID int64, messageIDs []int64) error {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return fmt.Errorf("view messages: %w", err)
	}

	maxID := int64(0)
	for _, id := range messageIDs {
		if id > maxID {
			maxID = id
		}
	}
	if maxID == 0 {
		return nil
	}

	if constant.TDLibPeerID(chatID).IsChannel() {
		inputChannel, ok := peerAsInputChannel(peer)
		if !ok {
			return fmt.Errorf("view messages: peer %d is not a channel", chatID)
		}
		if _, err := c.api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
			Channel: inputChannel,
			MaxID:   int(maxID),
		}); err != nil {
			return fmt.Errorf("read channel history: %w", err)
		}
		return nil
	}

	if _, err := c.api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{
		Peer:  peer,
		MaxID: int(maxID),
	}); err != nil {
		return fmt.Errorf("read history: %w", err)
	}
	return nil
}

// peerAsInputChannel extracts an InputChannel from an InputPeer.
func peerAsInputChannel(peer tg.InputPeerClass) (tg.InputChannelClass, bool) {
	switch p := peer.(type) {
	case *tg.InputPeerChannel:
		return &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash}, true
	case *tg.InputPeerChannelFromMessage:
		return &tg.InputChannelFromMessage{
			Peer:      p.Peer,
			MsgID:     p.MsgID,
			ChannelID: p.ChannelID,
		}, true
	default:
		return nil, false
	}
}
