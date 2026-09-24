package telegram

import (
	"context"
	"log"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/tg"
)

// Listener converts Telegram updates into tea messages and forwards
// them to the bubbletea program.
type Listener struct {
	client  *Client
	program *tea.Program
}

// NewListener registers update handlers on the client's dispatcher. It must
// be called before Client.Start; gotd's dispatcher is not safe to mutate once
// update delivery begins.
func NewListener(client *Client, program *tea.Program) (*Listener, error) {
	l := &Listener{
		client:  client,
		program: program,
	}
	if err := client.registerUpdateHandlers(l.registerHandlers); err != nil {
		return nil, err
	}
	return l, nil
}

// Start attaches the Bubble Tea sink and starts Telegram. Call it from a
// goroutine immediately before Program.Run: attaching can replay buffered
// startup notices, and Program.Send intentionally blocks until Run begins.
func (l *Listener) Start() error {
	l.client.setMsgSink(l.program.Send)
	return l.client.Start()
}

func (l *Listener) registerHandlers(d tg.UpdateDispatcher) {
	c := l.client

	d.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		l.onMessage(u.Message)
		return nil
	})
	d.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		l.onMessage(u.Message)
		return nil
	})
	d.OnEditMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditMessage) error {
		l.onEdit(u.Message)
		return nil
	})
	d.OnEditChannelMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		l.onEdit(u.Message)
		return nil
	})
	// A reaction or a poll vote changes a message that is already on
	// screen, and both arrive as their own update rather than as an edit.
	// Route them through the same refetch an edit takes: the tallies come
	// back attached to the message they belong to, so nothing here has to
	// merge a partial update into a message it cannot see.
	//
	// A reaction to the reader's own message also raises the chat's unread
	// reactions, the heart on the phone, until something reads it. The
	// update flags each recent reaction the reader has not seen.
	//
	// Both name the forum topic the message is in, and both are published
	// for that topic as well as for the forum — the refetch and the chip
	// alike, because the row that carries the heart is the topic's row when
	// the reader is inside one. See [Client.chatsForUpdate].
	d.OnMessageReactions(func(ctx context.Context, e tg.Entities, u *tg.UpdateMessageReactions) error {
		topMsgID, _ := u.GetTopMsgID()
		if edited, ok := messageEdited(u.Peer, u.MsgID); ok {
			c.publishRefetch(edited, topMsgID)
		}
		if hasUnreadReaction(u.Reactions) {
			for _, chatID := range c.chatsForUpdate(chatIDFromPeer(u.Peer), topMsgID) {
				c.send(ChatUnreadReactionsMsg{ChatId: chatID})
			}
		}
		return nil
	})
	d.OnMessagePoll(func(ctx context.Context, e tg.Entities, u *tg.UpdateMessagePoll) error {
		// The peer is OPTIONAL on this update: Telegram sends the poll's
		// new tally to everyone who has it open, without saying which chat
		// each of them is looking at.
		peer, _ := u.GetPeer()
		topMsgID, _ := u.GetTopMsgID()
		if edited, ok := messageEdited(peer, u.MsgID); ok {
			c.publishRefetch(edited, topMsgID)
		}
		return nil
	})
	d.OnDeleteMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteMessages) error {
		c.send(MessageDeletedMsg{
			ChatId:     0, // the update carries no peer
			MessageIds: intsToInt64s(u.Messages),
		})
		return nil
	})
	// This is the one update about a message that cannot say which topic it
	// is about: it carries message IDs and a channel, and the schema has no
	// field for a topic. The messages are gone, and which topic held them is
	// something the server has stopped saying.
	//
	// So it is announced for the forum and for every topic of it this
	// session has a chat for. That is honest because of what a message ID is
	// in a channel — the forum's own numbering, one message in exactly one
	// topic — so the announcement removes the messages from the topic that
	// held them and is a no-op everywhere else. The forum alone was the
	// alternative, and it left a message deleted on the phone on screen in
	// the topic for as long as it stayed open.
	d.OnDeleteChannelMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		forum := channelChatID(u.ChannelID)
		ids := intsToInt64s(u.Messages)
		c.send(MessageDeletedMsg{ChatId: forum, MessageIds: ids})
		for _, topic := range c.topics.topicChatIDsOf(forum) {
			c.send(MessageDeletedMsg{ChatId: topic, MessageIds: ids})
		}
		return nil
	})
	d.OnReadHistoryInbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadHistoryInbox) error {
		c.send(ChatReadInboxMsg{
			ChatId:                 chatIDFromPeer(u.Peer),
			LastReadInboxMessageId: int64(u.MaxID),
			UnreadCount:            int32(u.StillUnreadCount),
		})
		return nil
	})
	d.OnReadHistoryOutbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadHistoryOutbox) error {
		c.send(ChatReadOutboxMsg{
			ChatId:                  chatIDFromPeer(u.Peer),
			LastReadOutboxMessageId: int64(u.MaxID),
		})
		return nil
	})
	d.OnReadChannelInbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadChannelInbox) error {
		c.send(ChatReadInboxMsg{
			ChatId:                 channelChatID(u.ChannelID),
			LastReadInboxMessageId: int64(u.MaxID),
			UnreadCount:            int32(u.GetStillUnreadCount()),
		})
		return nil
	})
	// A forum topic's read marks arrive as the discussion updates: they are
	// what the server echoes for messages.readDiscussion, whether the read
	// was made here or on the reader's phone. Both are published under the
	// topic's own chat ID, because the topic is the chat whose row carries
	// the badge and whose thread draws the ticks — keyed by the forum they
	// would clear the forum's badge and leave every topic's where it was.
	//
	// Neither carries a count, and none is invented: per-topic unread
	// counts never arrive in an update at all (TDLib passes -1 for this one
	// and keeps its own), so the read pointer is the whole of what the
	// server said and the whole of what is published.
	//
	// Which is why the inbox half is published as a mark, not as a read
	// inbox: ChatReadInboxMsg carries a count and the store believes it, so
	// a phone-side read of the three oldest unread messages would have
	// zeroed the badge outright. A mark only advances the pointer, and the
	// count survives until the read reaches the newest message seen.
	d.OnReadChannelDiscussionInbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadChannelDiscussionInbox) error {
		topic, ok := c.readTopicChatID(u.ChannelID, u.TopMsgID)
		if !ok {
			return nil
		}
		c.send(ChatMarkedReadMsg{
			ChatId:       topic,
			MaxMessageId: int64(u.ReadMaxID),
		})
		return nil
	})
	d.OnReadChannelDiscussionOutbox(func(ctx context.Context, e tg.Entities, u *tg.UpdateReadChannelDiscussionOutbox) error {
		topic, ok := c.readTopicChatID(u.ChannelID, u.TopMsgID)
		if !ok {
			return nil
		}
		c.send(ChatReadOutboxMsg{
			ChatId:                  topic,
			LastReadOutboxMessageId: int64(u.ReadMaxID),
		})
		return nil
	})
	d.OnNotifySettings(func(ctx context.Context, e tg.Entities, u *tg.UpdateNotifySettings) error {
		// Only per-peer settings map to a chat; the class-wide variants
		// (notifyUsers/notifyChats/notifyBroadcasts/…) change defaults,
		// which we do not model.
		p, ok := u.Peer.(*tg.NotifyPeer)
		if !ok {
			return nil
		}
		chatID := chatIDFromPeer(p.Peer)
		if chatID == 0 {
			return nil
		}
		c.send(ChatMuteChangedMsg{
			ChatId: chatID,
			Muted:  mutedFromNotifySettings(u.NotifySettings, time.Now().Unix()),
		})
		return nil
	})
	d.OnDialogFilter(func(ctx context.Context, e tg.Entities, u *tg.UpdateDialogFilter) error {
		l.refreshFolders()
		return nil
	})
	d.OnDialogFilterOrder(func(ctx context.Context, e tg.Entities, u *tg.UpdateDialogFilterOrder) error {
		l.refreshFolders()
		return nil
	})
	d.OnUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateUserTyping) error {
		c.send(ChatActionMsg{
			ChatId: u.UserID,
			UserId: u.UserID,
			Action: chatActionFromTG(u.Action),
		})
		return nil
	})
	d.OnChatUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateChatUserTyping) error {
		c.send(ChatActionMsg{
			ChatId: basicGroupChatID(u.ChatID),
			UserId: senderUserID(u.FromID),
			Action: chatActionFromTG(u.Action),
		})
		return nil
	})
	// Typing inside a forum is drawn by whichever of the two chats the
	// reader has open, so it is announced for both. The thread keeps the
	// actions for the chat it is showing and drops the rest, which is why
	// the forum's copy is kept as well: somebody typing in a topic is typing
	// in the forum's flat stream too.
	d.OnChannelUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateChannelUserTyping) error {
		topMsgID, _ := u.GetTopMsgID()
		action := ChatActionMsg{
			UserId: senderUserID(u.FromID),
			Action: chatActionFromTG(u.Action),
		}
		for _, chatID := range c.chatsForUpdate(channelChatID(u.ChannelID), topMsgID) {
			action.ChatId = chatID
			c.send(action)
		}
		return nil
	})
}

// readTopicChatID is the chat ID a discussion read belongs to, and whether
// it belongs to one this client knows at all. The channel is named the way
// the two read updates name it, by its bare ID.
func (c *Client) readTopicChatID(channelID int64, topMsgID int) (int64, bool) {
	return c.topicOf(channelChatID(channelID), topMsgID)
}

// topicOf is the chat ID of the forum topic an update is about, and whether
// it is about one this client has a chat for.
//
// Every update that touches a message already on screen carries an optional
// top_msg_id — a reaction, a poll tally, a typing notice, a read mark — and
// every one of them asks this same question of it, so it is asked once.
//
// The registry is the gate, as it is for an arriving message. top_msg_id in
// something that is not a forum names a CHANNEL POST'S COMMENT THREAD — the
// same structure, in a discussion group — and a comment thread is not a
// chat here, has no row to draw and no thread to redraw. A forum whose
// topics this session has listed has a chat per topic; nothing else does.
//
// An update inside a listed forum that names NO topic is about General, the
// one topic every forum has. That is the rule a message's reply header is
// already read by (see messageFiledUnderItsTopic), and answering the same
// question two ways inside one package is how a reader ends up with a
// General that never redraws anything.
func (c *Client) topicOf(forumID int64, topMsgID int) (int64, bool) {
	if !c.topics.hasListed(forumID) {
		return 0, false
	}
	topicID := int64(topMsgID)
	if topicID == 0 {
		topicID = generalTopicID
	}
	return c.topicChatID(forumID, topicID), true
}

// chatsForUpdate is every chat an update about one message is announced
// for: the chat it named, and the topic inside it when that chat is a forum
// whose topics this session has listed.
//
// The pair is [Client.publishNewMessage]'s, and the argument for it is the
// same. A message in a forum belongs to two chats that both draw it — the
// topic the reader has open, and the forum's own flat stream — and the
// thread acts only on what is announced for the chat it HAS open, so a
// message in a topic announced under the forum alone reached nothing: an
// edit or a reaction from another device left the row exactly as it was
// until the topic was closed and opened again.
//
// Both rather than the topic alone because both chats want it, and it costs
// nothing to be sure of: a topic and its forum cannot both be the chat on
// screen, so the second announcement is a switch that does not match rather
// than a second round trip.
func (c *Client) chatsForUpdate(chatID int64, topMsgID int) []int64 {
	if topic, ok := c.topicOf(chatID, topMsgID); ok {
		return []int64{chatID, topic}
	}
	return []int64{chatID}
}

// publishRefetch asks the thread to fetch a message again, once for each
// chat the message is drawn in.
//
// An edit comes through here too, with the topic taken off the message
// itself rather than out of a flag field — the same question answered from
// a better source, since the server sent the whole message.
func (c *Client) publishRefetch(edited MessageEditedMsg, topMsgID int) {
	for _, chatID := range c.chatsForUpdate(edited.ChatId, topMsgID) {
		edited.ChatId = chatID
		c.send(edited)
	}
}

// refreshFolders re-reads the folder list off the update goroutine.
// The updates themselves carry only a partial view, so a refetch is both
// simpler and more correct; it must not block dispatch.
func (l *Listener) refreshFolders() {
	go func() {
		folders, err := l.client.GetChatFolders()
		if err != nil {
			log.Printf("refresh chat folders: %s", err)
			return
		}
		l.client.send(ChatFoldersMsg{Folders: folders})
	}()
}

// onMessage handles new messages (private/group/channel).
func (l *Listener) onMessage(mc tg.MessageClass) {
	m := l.client.messageClassFromTG(mc)
	if m == nil {
		return
	}
	l.client.publishNewMessage(m)
}

// onEdit handles edited messages.
//
// The topic comes off the message rather than out of a flag field: the
// server sent the whole message, and it is read for its topic by the same
// rule an arriving one is.
func (l *Listener) onEdit(mc tg.MessageClass) {
	m := l.client.messageClassFromTG(mc)
	if m == nil {
		return
	}
	l.client.publishRefetch(MessageEditedMsg{ChatId: m.ChatID, MessageId: m.ID}, int(m.TopicID))
}

// messageEdited is the refetch request for a message an update touched,
// and whether the update named a chat to fetch it from.
//
// An update with no usable peer is DROPPED rather than sent with a chat ID
// of zero. Zero is not a chat, and the one message type in this package
// that uses it means something specific by it — MessageDeletedMsg reads a
// zero chat as "every chat" — so a refetch request that carries one is a
// request aimed at nothing and one rename away from being aimed at
// everything. The tally arrives with the next edit or the next open.
func messageEdited(peer tg.PeerClass, msgID int) (MessageEditedMsg, bool) {
	chatID := chatIDFromPeer(peer)
	if chatID == 0 || msgID == 0 {
		return MessageEditedMsg{}, false
	}
	return MessageEditedMsg{ChatId: chatID, MessageId: int64(msgID)}, true
}

// hasUnreadReaction says whether any of a message's recent reactions is
// one the reader has not seen. Only reactions to the reader's own messages
// are ever flagged.
func hasUnreadReaction(r tg.MessageReactions) bool {
	for _, p := range r.RecentReactions {
		if p.Unread {
			return true
		}
	}
	return false
}

func intsToInt64s(ids []int) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}

// senderUserID extracts the user ID from a peer, 0 for chats/channels.
func senderUserID(p tg.PeerClass) int64 {
	if u, ok := p.(*tg.PeerUser); ok {
		return u.UserID
	}
	return 0
}

// chatActionFromTG maps a tg action to the domain typing/cancel pair.
func chatActionFromTG(a tg.SendMessageActionClass) ChatAction {
	if _, ok := a.(*tg.SendMessageCancelAction); ok {
		return &ChatActionCancel{}
	}
	return &ChatActionTyping{}
}
