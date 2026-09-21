package store

import (
	"testing"
)

// The dialog is the only thing that says how many unread reactions a chat
// has, and the entry keeps it the way it keeps the unread count.
func TestSetTakesTheDialogsUnreadReactions(t *testing.T) {
	s := NewChatStore()
	chat := dialogChat()
	chat.UnreadReactionsCount = 3

	s.Set(chat)

	if entry, _ := s.Get(7); entry.UnreadReactionsCount != 3 {
		t.Errorf("UnreadReactionsCount = %d, want the dialog's 3", entry.UnreadReactionsCount)
	}
}

// A peer lookup knows nothing about reactions, and opening a chat does
// one. Taking its zero would drop the count on every open, before the
// chat view had looked at it to decide whether there was anything to
// clear.
func TestMergeKeepsTheUnreadReactions(t *testing.T) {
	s := NewChatStore()
	chat := dialogChat()
	chat.UnreadReactionsCount = 3
	s.Set(chat)

	s.Merge(peerChat())

	if entry, _ := s.Get(7); entry.UnreadReactionsCount != 3 {
		t.Errorf("UnreadReactionsCount = %d after a peer merge, want 3", entry.UnreadReactionsCount)
	}
}

// A reaction, or clearing them, describes a chat; it does not introduce
// one. The row, if the chat ever gets one, comes from its dialog, which
// carries the count.
func TestReactionsInAnUnknownChatChangeNothing(t *testing.T) {
	s := NewChatStore()

	s.AddUnreadReaction(7)
	s.MarkReactionsRead(7)

	if _, ok := s.Get(7); ok {
		t.Error("a reaction invented an entry for a chat the store did not know")
	}
}
