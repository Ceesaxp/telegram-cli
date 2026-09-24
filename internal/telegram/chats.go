package telegram

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// LoadChats fetches the first page of the dialog list and pushes every chat
// to the UI as a ChatUpdateMsg (this replaces tdlib's updateNewChat flow).
//
// It also arms the pager: whatever it stopped at is where [LoadMoreChats]
// carries on from, so the reader can reach dialogs older than the first
// page without the client fetching an account's entire history at startup.
func (c *Client) LoadChats(limit int) error {
	return c.loadChatsWith(c.listChatsPage, limit)
}

func (c *Client) loadChatsWith(fetch dialogPageFetcher, limit int) error {
	c.dialogs.reset()
	_, err := c.pageWith(fetch, limit)
	return err
}

// LoadMoreChats fetches the next page after whatever has been loaded so
// far, pushing each chat to the UI. It reports how many arrived; zero means
// the list is exhausted and asking again will not change that.
func (c *Client) LoadMoreChats(limit int) (int, error) {
	return c.loadChatPage(limit)
}

// MoreChatsToLoad reports whether the dialog list has more to give. False
// once a short page has arrived, so the UI can stop asking rather than
// issuing a request per keystroke at the bottom of the list.
func (c *Client) MoreChatsToLoad() bool {
	c.dialogs.mu.Lock()
	defer c.dialogs.mu.Unlock()
	return !c.dialogs.done
}

// loadChatPage advances the pager by up to limit dialogs.
func (c *Client) loadChatPage(limit int) (int, error) {
	return c.pageWith(c.listChatsPage, limit)
}

// pageWith is loadChatPage against a supplied fetcher, so the pager's own
// rules — start over, carry on, stop when exhausted — can be exercised
// without a server.
func (c *Client) pageWith(fetch dialogPageFetcher, limit int) (int, error) {
	c.dialogs.mu.Lock()
	if c.dialogs.done {
		c.dialogs.mu.Unlock()
		return 0, nil
	}
	cursor := c.dialogs.cursor
	c.dialogs.mu.Unlock()

	chats, next, done, err := pageDialogs(fetch, cursor, limit)
	if err != nil {
		return 0, err
	}

	c.dialogs.mu.Lock()
	c.dialogs.cursor, c.dialogs.done = next, done
	c.dialogs.mu.Unlock()

	for _, chat := range chats {
		c.send(ChatUpdateMsg{Chat: chat})
	}
	return len(chats), nil
}

// dialogPager is where the dialog list has been read up to.
//
// On the client rather than handed to the UI: the cursor is a gotd
// InputPeer, and a chat list that had to hold one would be a UI component
// carrying a protocol type it can do nothing else with.
type dialogPager struct {
	mu     sync.Mutex
	cursor dialogCursor
	done   bool
}

func (p *dialogPager) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cursor, p.done = dialogCursor{}, false
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

// ListChats fetches the dialog list from the beginning without emitting any
// UI events. Telegram returns at most 100 dialogs per request, so larger
// limits are served by paginating until limit is reached or a short page
// arrives.
func (c *Client) ListChats(limit int) ([]*Chat, error) {
	chats, _, _, err := c.listChatsFrom(dialogCursor{}, limit)
	return chats, err
}

// listChatsFrom is the paging loop both entry points share.
//
// It returns the chats, the cursor to continue from, and whether the list is
// exhausted — a short page, a page with nothing to continue from, or a
// cursor the server did not advance. That last one is not paranoia: an
// unchanged cursor is a loop that never ends, and it is the failure mode a
// paging bug takes.
func (c *Client) listChatsFrom(cursor dialogCursor, limit int) ([]*Chat, dialogCursor, bool, error) {
	return pageDialogs(c.listChatsPage, cursor, limit)
}

// dialogPageFetcher is one request for one page: the same shape as
// [Client.listChatsPage], as a function so the paging RULES can be exercised
// without a server. Every decision that ends or advances pagination lives in
// pageDialogs below; listChatsPage only fetches and converts.
type dialogPageFetcher func(cursor dialogCursor, limit int) ([]*Chat, dialogCursor, int, error)

func pageDialogs(fetch dialogPageFetcher, cursor dialogCursor, limit int) (chats []*Chat, next dialogCursor, done bool, err error) {
	if limit <= 0 {
		limit = dialogsPageSize
	}
	if limit > MaxDialogsLimit {
		limit = MaxDialogsLimit
	}
	if cursor.peer == nil {
		cursor.peer = &tg.InputPeerEmpty{}
	}

	seen := make(map[int64]bool, limit)

	for len(chats) < limit {
		pageLimit := limit - len(chats)
		if pageLimit > dialogsPageSize {
			pageLimit = dialogsPageSize
		}

		page, after, raw, err := fetch(cursor, pageLimit)
		if err != nil {
			return nil, cursor, false, err
		}

		for _, chat := range page {
			if seen[chat.ID] {
				continue
			}
			seen[chat.ID] = true
			chats = append(chats, chat)
			if len(chats) == limit {
				break
			}
		}

		// A short page is the end of the list. after.peer is nil when the
		// page held no usable dialog to continue from, and an unchanged
		// cursor would loop forever.
		//
		// The PEER is part of "unchanged". A message id is scoped to its
		// peer, so two dialogs in different channels can share an id and a
		// date; comparing only those two would call the cursor stalled when
		// it had in fact moved, and every older dialog would be dropped for
		// the rest of the session.
		if raw < pageLimit || after.peer == nil || sameCursor(after, cursor) {
			return chats, cursor, true, nil
		}
		cursor = after
	}
	return chats, cursor, false, nil
}

// sameCursor reports whether two cursors point at the same dialog.
//
// By peer IDENTITY rather than by comparing the InputPeer values: those are
// interfaces over structs that carry access hashes, which the server is free
// to reissue, and two values describing one peer would then compare unequal.
// What matters here is only whether pagination advanced.
func sameCursor(a, b dialogCursor) bool {
	return a.date == b.date && a.id == b.id && peerIdentity(a.peer) == peerIdentity(b.peer)
}

// peerIdentity is a peer's ID, ignoring its access hash. Zero for the
// self/empty peers and for anything unrecognised, which is safe in the one
// place this is used: an unrecognised peer compares equal only to another
// unrecognised one, and the surrounding checks still bound the loop.
func peerIdentity(p tg.InputPeerClass) int64 {
	switch v := p.(type) {
	case *tg.InputPeerUser:
		return v.UserID
	case *tg.InputPeerChat:
		return v.ChatID
	case *tg.InputPeerChannel:
		return v.ChannelID
	case *tg.InputPeerUserFromMessage:
		return v.UserID
	case *tg.InputPeerChannelFromMessage:
		return v.ChannelID
	}
	return 0
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
		chat.UnreadReactionsCount = int32(d.UnreadReactionsCount)
		chat.UnreadMentionsCount = int32(d.UnreadMentionsCount)
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
//
// A forum topic is answered from the topic registry instead, with no round
// trip at all — see [Client.topicChat]. It is why the split below throws the
// forum away: this is one of the few methods where the topic's chat is the
// whole answer rather than something to translate on the way to a request.
func (c *Client) GetChat(chatID int64) (*Chat, error) {
	real, _ := c.splitTopic(chatID)
	if isSyntheticChatID(chatID) {
		return c.topicChat(chatID)
	}

	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.peers.ResolveTDLibID(ctx, constant.TDLibPeerID(real))
	if err != nil {
		return nil, fmt.Errorf("get chat %d: %w", real, err)
	}
	return c.resolvedChat(ctx, peer)
}

// ResolveUsername turns a public @username into the chat behind it.
//
// It is the network half of following a t.me link (see [ParseTmeLink]): a
// public link names a username, and a username is not a chat ID until the
// server says which peer it belongs to.
//
// The chat comes back rather than only its ID, because a username may well
// name a chat this account has never opened — that is the ordinary case for
// a link somebody pasted. ResolveDomain teaches the peer manager the access
// hash on the way through, so the history fetch that follows can work; the
// returned chat is what gives the caller a title to open it under, and the
// announcement below is what puts it in the chat list. Both halves of
// "a chat this client has not seen before" are therefore answered here,
// exactly as [Client.CreatePrivateChat] answers them for a contact.
func (c *Client) ResolveUsername(username string) (*Chat, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.peers.ResolveDomain(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("could not resolve @%s: %w", username, err)
	}
	chat, err := c.resolvedChat(ctx, peer)
	if err != nil {
		return nil, fmt.Errorf("could not resolve @%s: %w", username, err)
	}
	c.send(peerChatUpdate(chat))
	return chat, nil
}

// resolvedChat builds the domain chat for an already-resolved peer, mute
// flag included.
//
// EVERY peer-derived path goes through here, so "a chat from a peer carries
// the mute answer" is true by construction rather than by each caller
// remembering to ask. It is one function because it was two, and the second
// one forgot: CreatePrivateChat built its chat straight from the user
// entity, which has no notify settings on it, so the chat went to the store
// claiming Muted=false — and ChatStore.Merge, which copies the mute flag
// precisely so a fetch can update it, dutifully unmuted a muted contact the
// moment it was opened from the contact list.
func (c *Client) resolvedChat(ctx context.Context, peer peers.Peer) (*Chat, error) {
	var chat *Chat
	switch p := peer.(type) {
	case peers.User:
		chat = c.chatFromUser(p.Raw())
	case peers.Chat:
		chat = c.chatFromBasicGroup(p.Raw())
	case peers.Channel:
		chat = c.chatFromChannel(p.Raw())
	default:
		return nil, fmt.Errorf("unexpected peer type %T", peer)
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
//
// A forum topic is fetched as the thread it is — see [Client.historyPage] —
// and comes back as the same messages from the same call, because everything
// above this package reads a page of history and knows nothing about which
// RPC fetched it.
func (c *Client) GetChatHistory(chatID, fromMessageID int64, offset, limit int32) ([]*Message, error) {
	real, topicID := c.splitTopic(chatID)

	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, real)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	res, err := c.historyPage(ctx, peer, topicID, fromMessageID, offset, limit)
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

// historyPage fetches one page of a chat's history, or of one topic's.
//
// A topic's messages are the forum's, so messages.getHistory would answer
// with every topic's at once — the flat stream the reader opened a topic to
// get out of. A topic IS the thread under its root message, so
// messages.getReplies over that message is its history, and it takes the
// same three offsets, so the two calls page identically and the caller above
// need not know which one it got.
//
// General is passed like any other topic. Its root message is the forum's
// own first one and it exists, which is all getReplies asks for — unlike the
// reply header of a send, where naming General is what goes wrong.
//
// The remaining bounds are deliberately zero, as [Client.SearchChatMessages]
// leaves its own: no date, no min/max ID clamp and no result hash, all plain
// fields where zero reaches the wire as "unbounded" rather than "absent".
func (c *Client) historyPage(ctx context.Context, peer tg.InputPeerClass, topicID, fromMessageID int64, offset, limit int32) (tg.MessagesMessagesClass, error) {
	if topicID != 0 {
		return c.api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{
			Peer:       peer,
			MsgID:      int(topicID),
			OffsetID:   int(fromMessageID),
			OffsetDate: 0,
			AddOffset:  int(offset),
			Limit:      int(limit),
			MaxID:      0,
			MinID:      0,
			Hash:       0,
		})
	}
	return c.api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:       peer,
		OffsetID:   int(fromMessageID),
		OffsetDate: 0,
		AddOffset:  int(offset),
		Limit:      int(limit),
	})
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

	// Seed the peers manager, exactly as the dialog loader does for every
	// page. The entities built below are only for rendering these rows;
	// they teach the client nothing. Without this, a result the picker
	// labels "not in your chats" has no cached access hash, so the moment
	// anything tries to ACT on it — forward to it, open it — the peer
	// cannot be resolved. The results looked fine and were unusable.
	if err := c.peers.Apply(ctx, found.Users, found.Chats); err != nil {
		return nil, fmt.Errorf("search chats: apply peers: %w", err)
	}

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
// Inside a topic the search is scoped to that topic. The reader is looking
// at one conversation, and a hit in a different topic of the same forum
// would open onto a thread they are not in.
//
// An empty query returns an error rather than a guaranteed server-side
// SEARCH_QUERY_EMPTY round trip.
func (c *Client) SearchChatMessages(chatID int64, query string, fromMessageID int64, limit int32) ([]*Message, error) {
	if query == "" {
		return nil, fmt.Errorf("search chat messages: empty query")
	}
	real, topicID := c.splitTopic(chatID)

	ctx, cancel := opCtx()
	defer cancel()

	peer, err := c.inputPeer(ctx, real)
	if err != nil {
		return nil, fmt.Errorf("search chat messages: %w", err)
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	req := &tg.MessagesSearchRequest{
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
	}
	// top_msg_id is a flag field, so an unscoped search must leave it unset
	// rather than send a zero: a zero thread ID is not "every thread".
	if topicID != 0 {
		req.SetTopMsgID(int(topicID))
	}

	res, err := c.api.MessagesSearch(ctx, req)
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

// ViewMessages marks messages as read, and on success announces it with
// [ChatMarkedReadMsg]. The server's own receipt for this session's read does
// not reliably come back, so without the announcement the chat being read
// kept its unread badge. It is sent from the client, not from a caller, so
// every in-process caller gets it without having to remember. The REST and
// MCP servers are separate binaries that register no message sink, so there
// it goes nowhere.
//
// The announcement carries the chat ID the CALLER asked with, which for a
// topic is its synthetic one: the chat list keys the row on it, so naming
// the forum would clear a badge nobody was reading.
func (c *Client) ViewMessages(chatID int64, messageIDs []int64) error {
	real, topicID := c.splitTopic(chatID)

	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, real)
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

	// A topic takes neither of the two reads below. Its read pointer is its
	// own — the forum's says nothing about any one topic — and the call that
	// moves it is the thread's, so channels.readHistory here would mark
	// every topic in the forum read at once.
	if topicID != 0 {
		if _, err := c.api.MessagesReadDiscussion(ctx, &tg.MessagesReadDiscussionRequest{
			Peer:      peer,
			MsgID:     int(topicID),
			ReadMaxID: int(maxID),
		}); err != nil {
			return fmt.Errorf("read discussion history: %w", err)
		}
		c.send(ChatMarkedReadMsg{ChatId: chatID, MaxMessageId: maxID})
		return nil
	}

	if constant.TDLibPeerID(real).IsChannel() {
		inputChannel, ok := peerAsInputChannel(peer)
		if !ok {
			return fmt.Errorf("view messages: peer %d is not a channel", real)
		}
		if _, err := c.api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
			Channel: inputChannel,
			MaxID:   int(maxID),
		}); err != nil {
			return fmt.Errorf("read channel history: %w", err)
		}
		c.send(ChatMarkedReadMsg{ChatId: chatID, MaxMessageId: maxID})
		return nil
	}

	if _, err := c.api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{
		Peer:  peer,
		MaxID: int(maxID),
	}); err != nil {
		return fmt.Errorf("read history: %w", err)
	}
	c.send(ChatMarkedReadMsg{ChatId: chatID, MaxMessageId: maxID})
	return nil
}

// maxReadReactionsCalls bounds how many times ReadReactions repeats the
// call for one chat. The API says to repeat while the answer carries a
// positive offset, and says nothing that stops a server from carrying one
// for ever; a loop with only the operation's timeout to end it would hit
// the server as fast as it answers, which is what FLOOD_WAIT is for. Ten
// calls is far more than a chat's unread reactions should need, and
// small enough that a stuck walk costs little.
const maxReadReactionsCalls = 10

// ReadReactions clears a chat's unread reactions, and on success announces
// it with [ChatReactionsReadMsg], for the reason [ViewMessages] announces a
// read. It is the whole chat unless the chat is a topic, and then it is that
// topic: the counter the reader is clearing is the topic's row, and an
// unscoped clear would take every other topic in the forum with it.
//
// A positive offset in the server's answer means the call has to be made
// again, which is all the API documents about it. The call is repeated up
// to maxReadReactionsCalls times. Only an answer that says it is done is
// announced: stopping at the cap is not an error, but it is not a clear the
// server confirmed either, so the count stays and the next open asks again.
func (c *Client) ReadReactions(chatID int64) error {
	real, topicID := c.splitTopic(chatID)

	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, real)
	if err != nil {
		return fmt.Errorf("read reactions: %w", err)
	}

	done, err := repeatUntilDone(maxReadReactionsCalls, func() (*tg.MessagesAffectedHistory, error) {
		req := &tg.MessagesReadReactionsRequest{Peer: peer}
		// A flag field, so a whole-chat clear leaves it unset rather than
		// sending a zero: topic 0 is not "every topic".
		if topicID != 0 {
			req.SetTopMsgID(int(topicID))
		}
		return c.api.MessagesReadReactions(ctx, req)
	})
	if err != nil {
		return fmt.Errorf("read reactions: %w", err)
	}
	if done {
		c.send(ChatReactionsReadMsg{ChatId: chatID})
	}
	return nil
}

// repeatUntilDone makes call again while its answer carries a positive
// offset, which is how the API says a clear that answers
// messages.affectedHistory has more to do, and makes it at most calls times.
// It reports whether the server said it was done: stopping at the cap is
// not an error, and it is not a finished clear either.
func repeatUntilDone(calls int, call func() (*tg.MessagesAffectedHistory, error)) (bool, error) {
	for range calls {
		affected, err := call()
		if err != nil {
			return false, err
		}
		if affected.Offset <= 0 {
			return true, nil
		}
	}
	return false, nil
}

// UnreadMentions lists the IDs of a chat's unread mentions, oldest first,
// at most limit of them.
//
// Inside a topic it lists that topic's. The whole forum's would jump the
// reader out of the conversation they are walking.
func (c *Client) UnreadMentions(chatID int64, limit int) ([]int64, error) {
	real, topicID := c.splitTopic(chatID)

	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, real)
	if err != nil {
		return nil, fmt.Errorf("unread mentions: %w", err)
	}

	// Telegram Desktop's recipe for the OLDEST page: from message 1, with
	// the offset turned back by a whole page. With no offset the call
	// answers the newest, and a walk would start in the middle of a chat
	// with more than limit of them.
	req := &tg.MessagesGetUnreadMentionsRequest{
		Peer:      peer,
		OffsetID:  1,
		AddOffset: -limit,
		Limit:     limit,
	}
	// A flag field: unset means the whole chat, forum topics included, and
	// a zero would be a thread nothing is in.
	if topicID != 0 {
		req.SetTopMsgID(int(topicID))
	}

	res, err := c.api.MessagesGetUnreadMentions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("unread mentions: %w", err)
	}

	messages := messagesFromMessagesClass(res)
	ids := make([]int64, 0, len(messages))
	for _, m := range messages {
		ids = append(ids, int64(m.GetID()))
	}
	// The API does not say what order this call answers in, and a jump
	// that walks the mentions has to start at the oldest.
	slices.Sort(ids)
	return ids, nil
}

// ReadMentions clears the given unread mentions in a chat, and on success
// announces it with [ChatMentionsReadMsg], for the reason [ViewMessages]
// announces a read. A mention is cleared by reading its message's
// contents; reading the history does not do it.
//
// A channel's message IDs are its own numbering, so a supergroup takes the
// call that names the channel; every other chat takes the one with bare
// IDs.
//
// Which branch a topic takes is decided AFTER the split, and that is the
// whole of the fix here. A synthetic chat ID answers no to IsChannel, so a
// topic used to take the peerless call — and a topic's messages are the
// forum's channel messages, so those IDs are the channel's numbering and
// clearing them by the account's own cleared whatever else held them.
// Splitting first gives the forum's ID, which is a channel, so a topic now
// takes the channel branch with the forum's peer. There is nothing more to
// pass: channels.readMessageContents names no topic, and it needs none,
// because a message ID already belongs to exactly one topic.
func (c *Client) ReadMentions(chatID int64, messageIDs []int64) error {
	if len(messageIDs) == 0 {
		return nil
	}
	real, _ := c.splitTopic(chatID)

	ctx, cancel := opCtx()
	defer cancel()

	if constant.TDLibPeerID(real).IsChannel() {
		peer, err := c.inputPeer(ctx, real)
		if err != nil {
			return fmt.Errorf("read mentions: %w", err)
		}
		inputChannel, ok := peerAsInputChannel(peer)
		if !ok {
			return fmt.Errorf("read mentions: peer %d is not a channel", real)
		}
		if _, err := c.api.ChannelsReadMessageContents(ctx, &tg.ChannelsReadMessageContentsRequest{
			Channel: inputChannel,
			ID:      int64sToInts(messageIDs),
		}); err != nil {
			return fmt.Errorf("read channel mentions: %w", err)
		}
		c.send(ChatMentionsReadMsg{ChatId: chatID, MessageIds: messageIDs})
		return nil
	}

	if _, err := c.api.MessagesReadMessageContents(ctx, int64sToInts(messageIDs)); err != nil {
		return fmt.Errorf("read mentions: %w", err)
	}
	c.send(ChatMentionsReadMsg{ChatId: chatID, MessageIds: messageIDs})
	return nil
}

// maxReadMentionsCalls bounds how many times ReadAllMentions repeats the
// call for one chat, for the reason maxReadReactionsCalls gives: the API
// says to repeat while the answer carries a positive offset, and nothing
// stops a server from carrying one for ever.
const maxReadMentionsCalls = 10

// ReadAllMentions clears every unread mention in a chat, and on success
// announces it with [ChatMentionsReadMsg] with All set, for the reason
// [ViewMessages] announces a read. It is the whole chat, forum topics
// included — unless the chat IS a topic, and then it is that topic alone,
// because the @ the reader is clearing is the one on the topic's row.
//
// The call is repeated while the answer carries a positive offset, up to
// maxReadMentionsCalls times, the way [ReadReactions] repeats its own. Only
// an answer that says it is done is announced; stopping at the cap leaves
// the count for the next reload to correct.
//
// done reports whether the server said it had finished. Stopping at the cap
// is not an error, and it is not done either, so a caller that has to say
// whether the mentions are gone reads done rather than inferring it from a
// nil error.
func (c *Client) ReadAllMentions(chatID int64) (done bool, err error) {
	real, topicID := c.splitTopic(chatID)

	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, real)
	if err != nil {
		return false, fmt.Errorf("read all mentions: %w", err)
	}

	done, err = repeatUntilDone(maxReadMentionsCalls, func() (*tg.MessagesAffectedHistory, error) {
		req := &tg.MessagesReadMentionsRequest{Peer: peer}
		// A flag field, as in [Client.ReadReactions]: unset is the whole
		// chat, and a zero would be a thread nothing is in.
		if topicID != 0 {
			req.SetTopMsgID(int(topicID))
		}
		return c.api.MessagesReadMentions(ctx, req)
	})
	if err != nil {
		return false, fmt.Errorf("read all mentions: %w", err)
	}
	if done {
		c.send(ChatMentionsReadMsg{ChatId: chatID, All: true})
	}
	return done, nil
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
