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
	d.OnMessageReactions(func(ctx context.Context, e tg.Entities, u *tg.UpdateMessageReactions) error {
		if edited, ok := messageEdited(u.Peer, u.MsgID); ok {
			c.send(edited)
		}
		return nil
	})
	d.OnMessagePoll(func(ctx context.Context, e tg.Entities, u *tg.UpdateMessagePoll) error {
		// The peer is OPTIONAL on this update: Telegram sends the poll's
		// new tally to everyone who has it open, without saying which chat
		// each of them is looking at.
		peer, _ := u.GetPeer()
		if edited, ok := messageEdited(peer, u.MsgID); ok {
			c.send(edited)
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
	d.OnDeleteChannelMessages(func(ctx context.Context, e tg.Entities, u *tg.UpdateDeleteChannelMessages) error {
		c.send(MessageDeletedMsg{
			ChatId:     channelChatID(u.ChannelID),
			MessageIds: intsToInt64s(u.Messages),
		})
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
	d.OnChannelUserTyping(func(ctx context.Context, e tg.Entities, u *tg.UpdateChannelUserTyping) error {
		c.send(ChatActionMsg{
			ChatId: channelChatID(u.ChannelID),
			UserId: senderUserID(u.FromID),
			Action: chatActionFromTG(u.Action),
		})
		return nil
	})
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
func (l *Listener) onEdit(mc tg.MessageClass) {
	m := l.client.messageClassFromTG(mc)
	if m == nil {
		return
	}
	l.client.send(MessageEditedMsg{ChatId: m.ChatID, MessageId: m.ID})
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
