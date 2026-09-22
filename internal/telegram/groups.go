package telegram

import (
	"context"
	"fmt"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/tg"
)

// SupergroupFullInfo holds full info about a supergroup or channel.
type SupergroupFullInfo struct {
	Description string
	MemberCount int32
}

// BasicGroupFullInfo holds full info about a basic group.
type BasicGroupFullInfo struct {
	Description string
	MemberCount int32
	Members     []*ChatMember
}

// GetSupergroupFullInfo returns full info for a supergroup/channel chat.
func (c *Client) GetSupergroupFullInfo(chatID int64) (*SupergroupFullInfo, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get supergroup full info: %w", err)
	}
	inputChannel, ok := peerAsInputChannel(peer)
	if !ok {
		return nil, fmt.Errorf("chat %d is not a channel", chatID)
	}

	res, err := c.api.ChannelsGetFullChannel(ctx, inputChannel)
	if err != nil {
		return nil, fmt.Errorf("get supergroup full info: %w", err)
	}

	full, ok := res.FullChat.(*tg.ChannelFull)
	if !ok {
		return nil, fmt.Errorf("unexpected full chat type %T", res.FullChat)
	}

	info := &SupergroupFullInfo{}
	info.Description = sanitizeTerminal(full.GetAbout())
	if count, ok := full.GetParticipantsCount(); ok {
		info.MemberCount = int32(count)
	}
	return info, nil
}

// GetSupergroupMembers returns members of a supergroup/channel.
func (c *Client) GetSupergroupMembers(chatID int64, offset, limit int32) ([]*ChatMember, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.inputPeer(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("get supergroup members: %w", err)
	}
	inputChannel, ok := peerAsInputChannel(peer)
	if !ok {
		return nil, fmt.Errorf("chat %d is not a channel", chatID)
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}

	res, err := c.api.ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
		Channel: inputChannel,
		Filter:  &tg.ChannelParticipantsRecent{},
		Offset:  int(offset),
		Limit:   int(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("get supergroup members: %w", err)
	}
	participants, err := c.seededParticipants(ctx, res)
	if err != nil {
		return nil, err
	}

	members := make([]*ChatMember, 0, len(participants.Participants))
	for _, p := range participants.Participants {
		members = append(members, chatMemberFromTG(p))
	}
	return members, nil
}

// GetBasicGroupFullInfo returns full info (incl. members) for a basic group.
func (c *Client) GetBasicGroupFullInfo(chatID int64) (*BasicGroupFullInfo, error) {
	ctx, cancel := opCtx()
	defer cancel()
	plain := plainChatID(chatID)

	res, err := c.api.MessagesGetFullChat(ctx, plain)
	if err != nil {
		return nil, fmt.Errorf("get basic group full info: %w", err)
	}
	full, err := c.seededChatFull(ctx, res)
	if err != nil {
		return nil, err
	}

	info := &BasicGroupFullInfo{}
	info.Description = sanitizeTerminal(full.GetAbout())

	if participants, ok := full.Participants.(*tg.ChatParticipants); ok {
		info.MemberCount = int32(len(participants.Participants))
		for _, p := range participants.Participants {
			switch v := p.(type) {
			case *tg.ChatParticipantCreator:
				info.Members = append(info.Members, &ChatMember{
					MemberID: &MessageSenderUser{UserID: v.UserID},
					Status:   &ChatMemberStatusCreator{},
				})
			case *tg.ChatParticipantAdmin:
				info.Members = append(info.Members, &ChatMember{
					MemberID: &MessageSenderUser{UserID: v.UserID},
					Status:   &ChatMemberStatusAdministrator{},
				})
			case *tg.ChatParticipant:
				info.Members = append(info.Members, &ChatMember{
					MemberID: &MessageSenderUser{UserID: v.UserID},
					Status:   &ChatMemberStatusMember{},
				})
			}
		}
	}
	return info, nil
}

// SearchChatMembers finds the members of a chat whose names match query,
// for the @-mention picker.
func (c *Client) SearchChatMembers(chatID int64, query string, limit int) ([]*User, error) {
	ctx, cancel := opCtx()
	defer cancel()

	var (
		users []*User
		err   error
	)
	if constant.TDLibPeerID(chatID).IsChat() {
		users, err = c.basicGroupMembers(ctx, chatID)
	} else {
		users, err = c.supergroupMembers(ctx, chatID, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search chat members: %w", err)
	}
	return users, nil
}

// supergroupMembers is the members of a supergroup who match query and can
// be mentioned, at most limit of them, in the server's order. An empty
// query lists the recent members.
func (c *Client) supergroupMembers(ctx context.Context, chatID int64, query string, limit int) ([]*User, error) {
	channel, err := c.peers.ResolveChannelID(ctx, plainChatID(chatID))
	if err != nil {
		return nil, err
	}

	var filter tg.ChannelParticipantsFilterClass = &tg.ChannelParticipantsSearch{Q: query}
	if query == "" {
		filter = &tg.ChannelParticipantsRecent{}
	}
	res, err := c.api.ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
		Channel: channel.InputChannel(),
		Filter:  filter,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	participants, err := c.seededParticipants(ctx, res)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(participants.Participants))
	for _, p := range participants.Participants {
		if id, ok := participantUserID(p); ok {
			ids = append(ids, id)
		}
	}
	return mentionable(ids, participants.Users), nil
}

// basicGroupMembers is every member of a basic group who can be mentioned.
// A group that hides its members from the reader, which it does once the
// reader has been removed, has nobody.
func (c *Client) basicGroupMembers(ctx context.Context, chatID int64) ([]*User, error) {
	res, err := c.api.MessagesGetFullChat(ctx, plainChatID(chatID))
	if err != nil {
		return nil, err
	}
	full, err := c.seededChatFull(ctx, res)
	if err != nil {
		return nil, err
	}
	participants, ok := full.Participants.(*tg.ChatParticipants)
	if !ok {
		return nil, nil
	}

	ids := make([]int64, 0, len(participants.Participants))
	for _, p := range participants.Participants {
		ids = append(ids, p.GetUserID())
	}
	return mentionable(ids, res.Users), nil
}

// participantUserID is the user a supergroup participant is, if it is a
// user still in the group. A restricted member is listed as banned without
// having left, and is still there to be mentioned.
func participantUserID(p tg.ChannelParticipantClass) (int64, bool) {
	switch v := p.(type) {
	case *tg.ChannelParticipant:
		return v.UserID, true
	case *tg.ChannelParticipantSelf:
		return v.UserID, true
	case *tg.ChannelParticipantCreator:
		return v.UserID, true
	case *tg.ChannelParticipantAdmin:
		return v.UserID, true
	case *tg.ChannelParticipantBanned:
		if u, ok := v.Peer.(*tg.PeerUser); ok && !v.Left {
			return u.UserID, true
		}
	}
	return 0, false
}

// mentionable is the users behind the member IDs an answer lists, in the
// order listed, as the picker offers them: each once, and neither the reader
// nor a deleted account.
//
// It goes by the IDs rather than the answer's users because those are more
// than the members. They include whoever invited, promoted or removed a
// listed member, and that person may have left long ago. The reader is the
// user the server flags as self, which needs no lookup of its own.
func mentionable(memberIDs []int64, users []tg.UserClass) []*User {
	byID := tg.UserClassArray(users).NotEmptyToMap()
	seen := make(map[int64]bool, len(memberIDs))
	out := make([]*User, 0, len(memberIDs))
	for _, id := range memberIDs {
		u, ok := byID[id]
		if !ok || u.Self || u.Deleted || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, userFromTG(u))
	}
	return out
}

// seededParticipants reads a channels.getParticipants answer and seeds the
// peers manager with the users it carries, so every member it names has
// the access hash that acting on them takes.
func (c *Client) seededParticipants(ctx context.Context, res tg.ChannelsChannelParticipantsClass) (*tg.ChannelsChannelParticipants, error) {
	participants, ok := res.(*tg.ChannelsChannelParticipants)
	if !ok {
		return nil, fmt.Errorf("unexpected participants type %T", res)
	}
	if err := c.peers.Apply(ctx, participants.Users, nil); err != nil {
		return nil, fmt.Errorf("apply peers: %w", err)
	}
	return participants, nil
}

// seededChatFull reads a messages.getFullChat answer and seeds the peers
// manager with its users and chats, for the reason [seededParticipants]
// does.
func (c *Client) seededChatFull(ctx context.Context, res *tg.MessagesChatFull) (*tg.ChatFull, error) {
	full, ok := res.FullChat.(*tg.ChatFull)
	if !ok {
		return nil, fmt.Errorf("unexpected full chat type %T", res.FullChat)
	}
	if err := c.peers.Apply(ctx, res.Users, res.Chats); err != nil {
		return nil, fmt.Errorf("apply peers: %w", err)
	}
	return full, nil
}

// CreatePrivateChat returns a (synthetic) private chat entry for a user.
// No RPC is needed beyond resolving the user — the real chat is created
// server-side when the first message is sent.
func (c *Client) CreatePrivateChat(userID int64) (*Chat, error) {
	ctx, cancel := opCtx()
	defer cancel()
	peer, err := c.peers.ResolveUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("create private chat: %w", err)
	}
	chat, err := c.resolvedChat(ctx, peer)
	if err != nil {
		return nil, fmt.Errorf("create private chat: %w", err)
	}
	c.send(peerChatUpdate(chat))
	return chat, nil
}

// chatMemberFromTG maps a channel participant to a domain ChatMember.
func chatMemberFromTG(p tg.ChannelParticipantClass) *ChatMember {
	switch v := p.(type) {
	case *tg.ChannelParticipantCreator:
		return &ChatMember{
			MemberID: &MessageSenderUser{UserID: v.UserID},
			Status:   &ChatMemberStatusCreator{},
		}
	case *tg.ChannelParticipantAdmin:
		return &ChatMember{
			MemberID: &MessageSenderUser{UserID: v.UserID},
			Status:   &ChatMemberStatusAdministrator{},
		}
	case *tg.ChannelParticipantSelf:
		return &ChatMember{
			MemberID: &MessageSenderUser{UserID: v.UserID},
			Status:   &ChatMemberStatusMember{},
		}
	case *tg.ChannelParticipantBanned:
		return &ChatMember{
			MemberID: senderFromPeer(v.Peer),
			Status:   &ChatMemberStatusBanned{},
		}
	case *tg.ChannelParticipantLeft:
		return &ChatMember{
			MemberID: senderFromPeer(v.Peer),
			Status:   &ChatMemberStatusLeft{},
		}
	default:
		// ChannelParticipant and anything else.
		if pc, ok := p.(*tg.ChannelParticipant); ok {
			return &ChatMember{
				MemberID: &MessageSenderUser{UserID: pc.UserID},
				Status:   &ChatMemberStatusMember{},
			}
		}
		return &ChatMember{Status: &ChatMemberStatusMember{}}
	}
}
