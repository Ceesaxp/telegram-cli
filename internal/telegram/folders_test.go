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
	chats, err := c.LoadFolderDialogs(nil)
	if err != nil || chats != nil {
		t.Fatalf("LoadFolderDialogs(nil) = (%v, %v)", chats, err)
	}
	chats, err = c.LoadFolderDialogs(&ChatFolder{ID: 3})
	if err != nil || chats != nil {
		t.Fatalf("empty folder = (%v, %v)", chats, err)
	}
}
