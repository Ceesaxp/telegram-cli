package telegram

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
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

// The context rail's member list asks for a page the server will serve: no
// limit, or one past the server's cap, is asked as the cap.
func TestSupergroupMembersAsksForAPageTheServerServes(t *testing.T) {
	for limit, want := range map[int32]int{0: 200, -1: 200, 500: 200, 50: 50} {
		inv := &memberInvoker{participants: &tg.ChannelsChannelParticipants{}}
		c, _ := memberClient(inv)

		if _, err := c.GetSupergroupMembers(channelChatID(9), 0, limit); err != nil {
			t.Fatalf("GetSupergroupMembers(limit %d): %v", limit, err)
		}
		if asked := inv.participantsRequests(); len(asked) != 1 || asked[0].Limit != want {
			t.Errorf("limit %d was asked as %v, want %d", limit, asked, want)
		}
	}
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

// participantsRequests is what the client asked channels.getParticipants.
func (f *memberInvoker) participantsRequests() []*tg.ChannelsGetParticipantsRequest {
	var out []*tg.ChannelsGetParticipantsRequest
	for _, req := range f.asked {
		if r, ok := req.(*tg.ChannelsGetParticipantsRequest); ok {
			out = append(out, r)
		}
	}
	return out
}

// memberRequests counts the member lists the client asked for, of either
// kind.
func (f *memberInvoker) memberRequests() int {
	n := 0
	for _, req := range f.asked {
		switch req.(type) {
		case *tg.ChannelsGetParticipantsRequest, *tg.MessagesGetFullChatRequest:
			n++
		}
	}
	return n
}

// userIDs lists the users' IDs, in order.
func userIDs(users []*User) []int64 {
	out := make([]int64, len(users))
	for i, u := range users {
		out[i] = u.ID
	}
	return out
}

// A supergroup can be far too big to list, so what is typed after the @
// goes to the server as a participant search, and the members it answers
// come back as users the picker can show.
func TestSearchChatMembersSearchesASupergroup(t *testing.T) {
	nadia := member(4, "Nadia", "nadia")
	nadiaS := member(6, "Nadia S.", "")
	inv := &memberInvoker{participants: &tg.ChannelsChannelParticipants{
		Participants: []tg.ChannelParticipantClass{
			&tg.ChannelParticipant{UserID: 4},
			&tg.ChannelParticipant{UserID: 6},
		},
		Users: []tg.UserClass{nadia, nadiaS},
	}}
	c, _ := memberClient(inv)

	got, err := c.SearchChatMembers(channelChatID(9), "na", 20)
	if err != nil {
		t.Fatalf("SearchChatMembers: %v", err)
	}

	asked := inv.participantsRequests()
	if len(asked) != 1 {
		t.Fatalf("asked for participants %d times, want once", len(asked))
	}
	req := asked[0]
	if ch, ok := req.Channel.(*tg.InputChannel); !ok || ch.ChannelID != 9 || ch.AccessHash != 90 {
		t.Errorf("asked channel %#v, want the supergroup 9 with its hash", req.Channel)
	}
	if f, ok := req.Filter.(*tg.ChannelParticipantsSearch); !ok || f.Q != "na" {
		t.Errorf("asked with filter %#v, want a search for %q", req.Filter, "na")
	}
	if req.Limit != 20 || req.Offset != 0 {
		t.Errorf("asked with limit %d, offset %d; want 20, 0", req.Limit, req.Offset)
	}

	want := []*User{userFromTG(nadia), userFromTG(nadiaS)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SearchChatMembers = %#v, want %#v", got, want)
	}
}

// A bare @ has nothing to search for yet, so it asks for the members who
// were active lately, which is who the reader most likely means.
func TestSearchChatMembersWithoutAQueryAsksForRecentMembers(t *testing.T) {
	inv := &memberInvoker{participants: &tg.ChannelsChannelParticipants{}}
	c, _ := memberClient(inv)

	if _, err := c.SearchChatMembers(channelChatID(9), "", 20); err != nil {
		t.Fatalf("SearchChatMembers: %v", err)
	}

	asked := inv.participantsRequests()
	if len(asked) != 1 {
		t.Fatalf("asked for participants %d times, want once", len(asked))
	}
	if _, ok := asked[0].Filter.(*tg.ChannelParticipantsRecent); !ok {
		t.Errorf("asked with filter %#v, want the recent members", asked[0].Filter)
	}
	if asked[0].Limit != 20 {
		t.Errorf("asked with limit %d, want 20", asked[0].Limit)
	}
}

// The members a search finds are handed to the peers manager on the way
// through, so the one picked can be sent a mention by ID.
func TestSearchedMembersAreKnownAfterwards(t *testing.T) {
	nadia := member(4, "Nadia", "nadia")
	inv := &memberInvoker{participants: &tg.ChannelsChannelParticipants{
		Participants: []tg.ChannelParticipantClass{&tg.ChannelParticipant{UserID: 4}},
		Users:        []tg.UserClass{nadia},
	}}
	c, hashes := memberClient(inv)

	if _, err := c.SearchChatMembers(channelChatID(9), "na", 20); err != nil {
		t.Fatalf("SearchChatMembers: %v", err)
	}

	assertKnown(t, hashes, nadia)
}

// A basic group is small enough to hand over whole: every member comes back
// whatever is typed, and the composer narrows them down itself as the query
// grows, without asking again. The reader and whoever only invited
// somebody are still not among them.
func TestSearchChatMembersListsAllOfABasicGroup(t *testing.T) {
	reader := member(1, "Me", "me")
	reader.Self = true
	nadia, bob, nate := member(4, "Nadia", "nadia"), member(8, "Bob", "bob"), member(3, "Nate", "")
	inv := &memberInvoker{fullChat: &tg.MessagesChatFull{
		FullChat: &tg.ChatFull{ID: 5, Participants: &tg.ChatParticipants{
			ChatID: 5,
			Participants: []tg.ChatParticipantClass{
				&tg.ChatParticipantCreator{UserID: 8},
				&tg.ChatParticipantAdmin{UserID: 4, InviterID: 8},
				&tg.ChatParticipant{UserID: 1, InviterID: 3},
			},
		}},
		Users: []tg.UserClass{reader, nadia, bob, nate},
	}}
	c, hashes := memberClient(inv)

	got, err := c.SearchChatMembers(basicGroupID, "zz", 20)
	if err != nil {
		t.Fatalf("SearchChatMembers: %v", err)
	}

	if len(inv.asked) != 1 {
		t.Fatalf("asked the server %d times, want once: %#v", len(inv.asked), inv.asked)
	}
	if req, ok := inv.asked[0].(*tg.MessagesGetFullChatRequest); !ok || req.ChatID != 5 {
		t.Errorf("asked %#v, want the full chat 5", inv.asked[0])
	}
	if ids, want := userIDs(got), []int64{8, 4}; !slices.Equal(ids, want) {
		t.Errorf("SearchChatMembers = %v, want %v", ids, want)
	}
	assertKnown(t, hashes, nadia)
}

// A basic group that no longer shows the reader its members, because the
// reader was removed from it, has nobody to offer.
func TestSearchChatMembersOffersNobodyFromAHiddenBasicGroup(t *testing.T) {
	inv := &memberInvoker{fullChat: &tg.MessagesChatFull{
		FullChat: &tg.ChatFull{ID: 5, Participants: &tg.ChatParticipantsForbidden{ChatID: 5}},
	}}
	c, _ := memberClient(inv)

	got, err := c.SearchChatMembers(basicGroupID, "", 20)
	if err != nil {
		t.Fatalf("SearchChatMembers: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("SearchChatMembers = %v, want nobody", userIDs(got))
	}
}

// The picker is only for chats with members to choose between. A private
// chat has one other person in it, and nobody is mentioned in a broadcast
// channel's posts, so both offer nobody without a member list being asked
// for. A private chat is told by its ID alone. A channel's broadcast flag
// comes from resolving it, the lookup every channel call in this client
// starts with.
func TestSearchChatMembersOffersNobodyOutsideAGroup(t *testing.T) {
	t.Run("a private chat", func(t *testing.T) {
		inv := &memberInvoker{}
		c, _ := memberClient(inv)

		got, err := c.SearchChatMembers(userChatID(4), "na", 20)
		if err != nil {
			t.Fatalf("SearchChatMembers: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("SearchChatMembers = %v, want nobody", userIDs(got))
		}
		if len(inv.asked) != 0 {
			t.Errorf("asked the server %#v, want nothing", inv.asked)
		}
	})

	t.Run("a broadcast channel", func(t *testing.T) {
		inv := &memberInvoker{}
		c, _ := memberClient(inv)

		got, err := c.SearchChatMembers(channelChatID(10), "na", 20)
		if err != nil {
			t.Fatalf("SearchChatMembers: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("SearchChatMembers = %v, want nobody", userIDs(got))
		}
		if n := inv.memberRequests(); n != 0 {
			t.Errorf("asked for the members %d times, want never", n)
		}
	})
}

// A refusal is the caller's to handle, and a FLOOD_WAIT most of all: the
// picker is typed into, and a lookup that waited out the flood here would
// hold a stale query open for as long as the server asked. It is asked
// once, and the answer says which call it was.
func TestSearchChatMembersReturnsTheRefusal(t *testing.T) {
	for name, chatID := range map[string]int64{
		"a supergroup":  channelChatID(9),
		"a basic group": basicGroupID,
	} {
		t.Run(name, func(t *testing.T) {
			flood := tgerr.New(420, "FLOOD_WAIT_7")
			inv := &memberInvoker{err: flood}
			c, _ := memberClient(inv)

			_, err := c.SearchChatMembers(chatID, "na", 20)

			if d, ok := tgerr.AsFloodWait(err); !ok || d != 7*time.Second {
				t.Fatalf("SearchChatMembers error = %v, want the FLOOD_WAIT of 7s", err)
			}
			if !strings.HasPrefix(err.Error(), "search chat members: ") {
				t.Errorf("error %q does not say which call failed", err)
			}
			if n := inv.memberRequests(); n != 1 {
				t.Errorf("asked for the members %d times, want once", n)
			}
		})
	}
}

// What comes back is who can be mentioned: the members the answer lists,
// each once, in the server's order. The answer's users are more than that.
// They include whoever invited, promoted or removed a listed member,
// whether or not that person is still in the group. The reader cannot
// usefully mention themselves, and a deleted account has nobody behind it.
func TestSearchChatMembersReturnsOnlyWhoCanBeMentioned(t *testing.T) {
	reader := member(1, "Me", "me")
	reader.Self = true
	deleted := member(7, "Deleted Account", "")
	deleted.Deleted = true
	nadia, nadiaS, bob, nate := member(4, "Nadia", "nadia"), member(6, "Nadia S.", ""), member(8, "Bob", "bob"), member(3, "Nate", "")
	users := []tg.UserClass{reader, deleted, nadia, nadiaS, bob, nate}

	for name, tc := range map[string]struct {
		participants []tg.ChannelParticipantClass
		want         []int64
	}{
		"in the server's order": {
			participants: []tg.ChannelParticipantClass{
				&tg.ChannelParticipant{UserID: 6},
				&tg.ChannelParticipant{UserID: 4},
			},
			want: []int64{6, 4},
		},
		"each member once": {
			participants: []tg.ChannelParticipantClass{
				&tg.ChannelParticipant{UserID: 4},
				&tg.ChannelParticipantAdmin{UserID: 4},
			},
			want: []int64{4},
		},
		"not the reader": {
			participants: []tg.ChannelParticipantClass{
				&tg.ChannelParticipantSelf{UserID: 1},
				&tg.ChannelParticipant{UserID: 4},
			},
			want: []int64{4},
		},
		"not a deleted account": {
			participants: []tg.ChannelParticipantClass{
				&tg.ChannelParticipant{UserID: 7},
				&tg.ChannelParticipant{UserID: 4},
			},
			want: []int64{4},
		},
		"not whoever promoted or invited a member": {
			participants: []tg.ChannelParticipantClass{
				&tg.ChannelParticipantAdmin{UserID: 4, PromotedBy: 8, InviterID: 8},
			},
			want: []int64{4},
		},
		"every kind of member, and nobody who has gone": {
			participants: []tg.ChannelParticipantClass{
				&tg.ChannelParticipantCreator{UserID: 8},
				&tg.ChannelParticipantAdmin{UserID: 4},
				// Restricted, and still in the group.
				&tg.ChannelParticipantBanned{Peer: &tg.PeerUser{UserID: 6}},
				// Removed, and gone of their own accord.
				&tg.ChannelParticipantBanned{Peer: &tg.PeerUser{UserID: 3}, Left: true},
				&tg.ChannelParticipantLeft{Peer: &tg.PeerUser{UserID: 3}},
			},
			want: []int64{8, 4, 6},
		},
	} {
		t.Run(name, func(t *testing.T) {
			inv := &memberInvoker{participants: &tg.ChannelsChannelParticipants{
				Participants: tc.participants,
				Users:        users,
			}}
			c, _ := memberClient(inv)

			got, err := c.SearchChatMembers(channelChatID(9), "n", 20)
			if err != nil {
				t.Fatalf("SearchChatMembers: %v", err)
			}
			if ids := userIDs(got); !slices.Equal(ids, tc.want) {
				t.Errorf("SearchChatMembers = %v, want %v", ids, tc.want)
			}
		})
	}
}
