package telegram

import (
	"context"
	"errors"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
)

// supergroup is the channel 9 as a group people talk in; broadcast is the
// channel 10 as one that only posts.
var (
	supergroup = &tg.Channel{ID: 9, AccessHash: 90, Title: "g", Megagroup: true}
	broadcast  = &tg.Channel{ID: 10, AccessHash: 100, Title: "c", Broadcast: true}
)

// memberInvoker stands in for the server's member lists. It knows the
// channels supergroup and broadcast, answers channels.getParticipants with
// participants and messages.getFullChat with fullChat, or either with err,
// and records every request. Anything else is refused.
type memberInvoker struct {
	participants tg.ChannelsChannelParticipantsClass
	fullChat     *tg.MessagesChatFull
	err          error
	asked        []bin.Encoder
}

func (f *memberInvoker) Invoke(_ context.Context, input bin.Encoder, output bin.Decoder) error {
	f.asked = append(f.asked, input)
	switch input.(type) {
	case *tg.ChannelsGetChannelsRequest:
		output.(*tg.MessagesChatsBox).Chats = &tg.MessagesChats{
			Chats: []tg.ChatClass{supergroup, broadcast},
		}
		return nil
	case *tg.ChannelsGetParticipantsRequest:
		if f.err != nil {
			return f.err
		}
		output.(*tg.ChannelsChannelParticipantsBox).ChannelParticipants = f.participants
		return nil
	case *tg.MessagesGetFullChatRequest:
		if f.err != nil {
			return f.err
		}
		*output.(*tg.MessagesChatFull) = *f.fullChat
		return nil
	default:
		return errors.New("unexpected request")
	}
}

// memberClient is a client that asks inv for member lists, and a reader of
// the peer storage behind it: the access hashes a later send resolves users
// with.
func memberClient(inv *memberInvoker) (*Client, peerUserHasher) {
	store := &peers.InmemoryStorage{}
	api := tg.NewClient(inv)
	c := &Client{api: api, peers: peers.Options{Storage: store}.Build(api)}
	return c, peerUserHasher{storage: store}
}

// member is a user the server lists in a group, with an access hash of its
// own so a stored hash can be told apart from a guessed one.
func member(id int64, first, username string) *tg.User {
	u := &tg.User{ID: id, AccessHash: id * 1000, FirstName: first}
	if username != "" {
		u.SetUsername(username)
	}
	return u
}

// assertKnown checks that the peers manager has stored the user's access
// hash, which is what lets a later send name them: without it the server
// is asked about a user it has no reason to believe this account has met.
func assertKnown(t *testing.T, hashes peerUserHasher, u *tg.User) {
	t.Helper()
	hash, found, err := hashes.GetUserAccessHash(context.Background(), 0, u.ID)
	if err != nil {
		t.Fatalf("read the hash of user %d: %v", u.ID, err)
	}
	if !found || hash != u.AccessHash {
		t.Errorf("user %d: stored hash %d (found %v), want %d", u.ID, hash, found, u.AccessHash)
	}
}

// The context rail's member list teaches the peers manager every member it
// shows, so acting on one of them has the hash to do it with.
func TestSupergroupMembersAreKnownAfterwards(t *testing.T) {
	nadia := member(4, "Nadia", "nadia")
	inv := &memberInvoker{participants: &tg.ChannelsChannelParticipants{
		Participants: []tg.ChannelParticipantClass{&tg.ChannelParticipant{UserID: 4}},
		Users:        []tg.UserClass{nadia},
	}}
	c, hashes := memberClient(inv)

	if _, err := c.GetSupergroupMembers(channelChatID(9), 0, 50); err != nil {
		t.Fatalf("GetSupergroupMembers: %v", err)
	}

	assertKnown(t, hashes, nadia)
}

// A basic group's full info does the same for its members.
func TestBasicGroupMembersAreKnownAfterwards(t *testing.T) {
	nadia := member(4, "Nadia", "nadia")
	inv := &memberInvoker{fullChat: &tg.MessagesChatFull{
		FullChat: &tg.ChatFull{ID: 5, Participants: &tg.ChatParticipants{
			ChatID:       5,
			Participants: []tg.ChatParticipantClass{&tg.ChatParticipant{UserID: 4}},
		}},
		Users: []tg.UserClass{nadia},
	}}
	c, hashes := memberClient(inv)

	if _, err := c.GetBasicGroupFullInfo(basicGroupID); err != nil {
		t.Fatalf("GetBasicGroupFullInfo: %v", err)
	}

	assertKnown(t, hashes, nadia)
}
