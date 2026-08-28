package telegram

import (
	"fmt"

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

	participants, ok := res.(*tg.ChannelsChannelParticipants)
	if !ok {
		return nil, fmt.Errorf("unexpected participants type %T", res)
	}

	// Seed the peers manager with member users.
	if err := c.peers.Apply(ctx, participants.Users, nil); err != nil {
		return nil, fmt.Errorf("apply peers: %w", err)
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

	full, ok := res.FullChat.(*tg.ChatFull)
	if !ok {
		return nil, fmt.Errorf("unexpected full chat type %T", res.FullChat)
	}

	// Seed the peers manager with member users.
	if err := c.peers.Apply(ctx, res.Users, res.Chats); err != nil {
		return nil, fmt.Errorf("apply peers: %w", err)
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
	chat := c.chatFromUser(peer.Raw())
	c.send(ChatUpdateMsg{Chat: chat})
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
