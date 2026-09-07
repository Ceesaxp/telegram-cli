package store

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

func TestChatStoreSetMuted(t *testing.T) {
	s := NewChatStore()
	s.Set(&telegram.Chat{ID: 1, Muted: false})

	s.SetMuted(1, true)

	entry, ok := s.Get(1)
	if !ok {
		t.Fatal("chat 1 should exist in store")
	}
	if !entry.Chat.Muted {
		t.Fatal("SetMuted(1, true) should mark the chat muted")
	}

	s.SetMuted(1, false)
	entry, _ = s.Get(1)
	if entry.Chat.Muted {
		t.Fatal("SetMuted(1, false) should unmute the chat")
	}
}

func TestChatStoreSetMutedUnknownChatIsNoop(t *testing.T) {
	s := NewChatStore()
	// Should not panic and should not create an entry.
	s.SetMuted(999, true)
	if _, ok := s.Get(999); ok {
		t.Fatal("SetMuted on an unknown chat should not create an entry")
	}
}

func TestChatStoreSetPreservesMuted(t *testing.T) {
	s := NewChatStore()
	s.Set(&telegram.Chat{ID: 1, Muted: true})

	entry, ok := s.Get(1)
	if !ok {
		t.Fatal("chat 1 should exist in store")
	}
	if !entry.Chat.Muted {
		t.Fatal("Set should copy the Muted flag from the incoming chat")
	}
}

// A read receipt from the other side moves the outbox mark forward, and
// only forward: receipts can arrive out of order, and an older one must not
// take a tick back.
func TestChatStoreUpdateReadOutboxOnlyAdvances(t *testing.T) {
	s := NewChatStore()
	s.Set(&telegram.Chat{ID: 1, LastReadOutboxMessageID: 5})

	s.UpdateReadOutbox(1, 9)
	if entry, _ := s.Get(1); entry.Chat.LastReadOutboxMessageID != 9 {
		t.Fatalf("after advancing to 9: %d", entry.Chat.LastReadOutboxMessageID)
	}
	s.UpdateReadOutbox(1, 7)
	if entry, _ := s.Get(1); entry.Chat.LastReadOutboxMessageID != 9 {
		t.Fatalf("an older receipt moved the mark back to %d", entry.Chat.LastReadOutboxMessageID)
	}
	s.UpdateReadOutbox(2, 3) // unknown chat: nothing to mark, nothing to invent
	if _, ok := s.Get(2); ok {
		t.Fatal("a receipt must not invent a chat")
	}
}
