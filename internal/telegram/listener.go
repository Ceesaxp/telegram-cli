package telegram

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/tg"
)

// Listener converts Telegram updates into tea messages and forwards
// them to the bubbletea program.
type Listener struct {
	client  *Client
	program *tea.Program
}

// NewListener registers update handlers on the client's dispatcher.
// main.go should now pass the wrapper client: telegram.NewListener(tgClient, p).
func NewListener(client *Client, program *tea.Program) *Listener {
	l := &Listener{
		client:  client,
		program: program,
	}
	client.setMsgSink(program.Send)
	l.registerHandlers()
	return l
}

// Start is a no-op kept for API compatibility: handlers are registered
// eagerly in NewListener and dispatch is driven by client.Run.
func (l *Listener) Start() {}

func (l *Listener) registerHandlers() {
	c := l.client
	d := c.dispatcher

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

// onMessage handles new messages (private/group/channel).
func (l *Listener) onMessage(mc tg.MessageClass) {
	m := l.client.messageClassFromTG(mc)
	if m == nil {
		return
	}
	l.client.send(NewMessageMsg{Message: m})
	l.client.send(ChatLastMessageMsg{ChatId: m.ChatID, LastMessage: m})
}

// onEdit handles edited messages.
func (l *Listener) onEdit(mc tg.MessageClass) {
	m := l.client.messageClassFromTG(mc)
	if m == nil {
		return
	}
	l.client.send(MessageEditedMsg{ChatId: m.ChatID, MessageId: m.ID})
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
