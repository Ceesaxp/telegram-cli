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
