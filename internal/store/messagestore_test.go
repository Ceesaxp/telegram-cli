package store

import (
	"testing"

	"github.com/imtaqin/telegram-cli/internal/telegram"
)

func storedMessage(chatID, id int64) *telegram.Message {
	return &telegram.Message{ChatID: chatID, ID: id}
}

func TestActiveChatHistoryCanPagePastBackgroundCap(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	for id := int64(101); id <= 300; id++ {
		s.Append(chatID, storedMessage(chatID, id))
	}
	s.Activate(chatID)

	older := make([]*telegram.Message, 0, 50)
	for id := int64(51); id <= 100; id++ {
		older = append(older, storedMessage(chatID, id))
	}
	inserted := s.Prepend(chatID, older)

	if len(inserted) != 50 || s.Count(chatID) != 250 {
		t.Fatalf("inserted=%d count=%d, want 50 and 250", len(inserted), s.Count(chatID))
	}
	if oldest := s.OldestMessageId(chatID); oldest != 51 {
		t.Fatalf("oldest=%d, want 51", oldest)
	}
}

func TestLeavingActiveChatRestoresBound(t *testing.T) {
	s := NewMessageStore()
	s.maxSize = 3
	s.Activate(1)
	for id := int64(1); id <= 5; id++ {
		s.Append(1, storedMessage(1, id))
	}

	s.Activate(2)
	got := s.Get(1)
	if len(got) != 3 || got[0].ID != 3 || got[2].ID != 5 {
		t.Fatalf("trimmed previous chat = %v, want IDs 3..5", messageIDs(got))
	}
}

func TestPrependReportsOnlyNewSurvivingMessages(t *testing.T) {
	s := NewMessageStore()
	s.maxSize = 3
	s.Append(1, storedMessage(1, 3))
	s.Append(1, storedMessage(1, 4))
	s.Append(1, storedMessage(1, 5))

	inserted := s.Prepend(1, []*telegram.Message{
		storedMessage(1, 1),
		storedMessage(1, 2),
		storedMessage(1, 2),
		storedMessage(1, 3),
	})
	if len(inserted) != 0 {
		t.Fatalf("surviving inserts = %v, want none", messageIDs(inserted))
	}
	if got := messageIDs(s.Get(1)); len(got) != 3 || got[0] != 3 || got[2] != 5 {
		t.Fatalf("background cache = %v, want [3 4 5]", got)
	}
}

func TestReplaceMessageIDAlwaysRepairsBackgroundCap(t *testing.T) {
	s := NewMessageStore()
	s.maxSize = 3
	for id := int64(1); id <= 3; id++ {
		s.Append(1, storedMessage(1, id))
	}

	s.ReplaceMessageId(1, 0, storedMessage(1, 4))
	s.ReplaceMessageId(1, 0, storedMessage(1, 4)) // dispatcher echo
	got := s.Get(1)
	if len(got) != 3 || got[0].ID != 2 || got[2].ID != 4 {
		t.Fatalf("messages = %v, want [2 3 4]", messageIDs(got))
	}
}

func messageIDs(messages []*telegram.Message) []int64 {
	ids := make([]int64, len(messages))
	for i, message := range messages {
		ids[i] = message.ID
	}
	return ids
}
