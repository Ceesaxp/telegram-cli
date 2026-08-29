package telegram

import (
	"testing"

	"github.com/gotd/td/tg"
)

func TestChatFolderNeedsPeerFetch(t *testing.T) {
	if (&ChatFolder{ID: 1}).NeedsPeerFetch() {
		t.Fatal("empty custom folder should not need a peer fetch")
	}
	f := chatFolderFromTG(&tg.DialogFilter{
		ID:           7,
		IncludePeers: []tg.InputPeerClass{&tg.InputPeerUser{UserID: 1, AccessHash: 2}},
	})
	if f == nil || !f.NeedsPeerFetch() {
		t.Fatal("include_peers should mark the folder as needing a fetch")
	}
	if len(f.IncludedChatIDs) != 1 || f.IncludedChatIDs[0] != userChatID(1) {
		t.Fatalf("IncludedChatIDs = %v", f.IncludedChatIDs)
	}
}

func TestUniqueInputPeersDedupsByID(t *testing.T) {
	a := &tg.InputPeerUser{UserID: 1, AccessHash: 10}
	b := &tg.InputPeerUser{UserID: 1, AccessHash: 11}
	c := &tg.InputPeerChannel{ChannelID: 2, AccessHash: 3}
	got := uniqueInputPeers([]tg.InputPeerClass{a, c}, []tg.InputPeerClass{b})
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
}

func TestLoadFolderDialogsNil(t *testing.T) {
	c := &Client{}
	chats, err := c.LoadFolderDialogs(nil, nil)
	if err != nil || chats != nil {
		t.Fatalf("LoadFolderDialogs(nil) = (%v, %v)", chats, err)
	}
	chats, err = c.LoadFolderDialogs(&ChatFolder{ID: 3}, nil)
	if err != nil || chats != nil {
		t.Fatalf("empty folder = (%v, %v)", chats, err)
	}
}

func TestDropKnownPeers(t *testing.T) {
	a := &tg.InputPeerUser{UserID: 1, AccessHash: 1}
	b := &tg.InputPeerChannel{ChannelID: 2, AccessHash: 2}
	peers := []tg.InputPeerClass{a, b}
	already := map[int64]struct{}{userChatID(1): {}}
	got := dropKnownPeers(peers, already)
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	ch, ok := got[0].(*tg.InputPeerChannel)
	if !ok || ch.ChannelID != 2 {
		t.Fatalf("remaining peer = %#v, want channel 2", got[0])
	}
	if got := dropKnownPeers(peers, nil); len(got) != 2 {
		t.Fatalf("nil already: len=%d, want 2", len(got))
	}
}
