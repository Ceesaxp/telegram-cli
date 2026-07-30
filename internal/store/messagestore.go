package store

import (
	"sync"

	"github.com/tegal1337/telegram-cli/internal/telegram"
)

const defaultMessageBufferSize = 200

// MessageStore caches messages per chat.
type MessageStore struct {
	mu       sync.RWMutex
	messages map[int64][]*telegram.Message // chatID -> messages (newest last)
	maxSize  int
}

func NewMessageStore() *MessageStore {
	return &MessageStore{
		messages: make(map[int64][]*telegram.Message),
		maxSize:  defaultMessageBufferSize,
	}
}

// Append adds a new message to the end of the chat's message list.
func (s *MessageStore) Append(chatID int64, msg *telegram.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.messages[chatID]

	// Deduplicate by message ID.
	for i, m := range msgs {
		if m.ID == msg.ID {
			msgs[i] = msg
			return
		}
	}

	msgs = append(msgs, msg)

	// Trim if exceeding max size.
	if len(msgs) > s.maxSize {
		msgs = msgs[len(msgs)-s.maxSize:]
	}

	s.messages[chatID] = msgs
}

// Prepend adds older messages to the beginning of the chat's message list.
func (s *MessageStore) Prepend(chatID int64, msgs []*telegram.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.messages[chatID]

	// Build a set of existing IDs to avoid duplicates.
	idSet := make(map[int64]struct{}, len(existing))
	for _, m := range existing {
		idSet[m.ID] = struct{}{}
	}

	var toAdd []*telegram.Message
	for _, m := range msgs {
		if _, exists := idSet[m.ID]; !exists {
			toAdd = append(toAdd, m)
		}
	}

	combined := make([]*telegram.Message, 0, len(toAdd)+len(existing))
	combined = append(combined, toAdd...)
	combined = append(combined, existing...)

	if len(combined) > s.maxSize {
		combined = combined[len(combined)-s.maxSize:]
	}

	s.messages[chatID] = combined
}

// Get returns all cached messages for a chat.
func (s *MessageStore) Get(chatID int64) []*telegram.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[chatID]
	result := make([]*telegram.Message, len(msgs))
	copy(result, msgs)
	return result
}

// UpdateMessage replaces a message in the store (for edits).
func (s *MessageStore) UpdateMessage(chatID int64, messageID int64, newMsg *telegram.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.messages[chatID]
	for i, m := range msgs {
		if m.ID == messageID {
			msgs[i] = newMsg
			return
		}
	}
}

// Delete removes messages from the store.
func (s *MessageStore) Delete(chatID int64, messageIDs []int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteLocked(chatID, messageIDs)
}

// DeleteFromAll removes messages from every chat. Used for non-channel
// delete updates, which carry no peer (ChatId == 0).
func (s *MessageStore) DeleteFromAll(messageIDs []int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for chatID := range s.messages {
		s.deleteLocked(chatID, messageIDs)
	}
}

func (s *MessageStore) deleteLocked(chatID int64, messageIDs []int64) {
	msgs := s.messages[chatID]
	idSet := make(map[int64]struct{}, len(messageIDs))
	for _, id := range messageIDs {
		idSet[id] = struct{}{}
	}

	filtered := msgs[:0]
	for _, m := range msgs {
		if _, del := idSet[m.ID]; !del {
			filtered = append(filtered, m)
		}
	}
	s.messages[chatID] = filtered
}

// Clear removes all cached messages for a chat.
func (s *MessageStore) Clear(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.messages, chatID)
}

// ReplaceMessageId replaces a temporary message ID with the real one (after send).
func (s *MessageStore) ReplaceMessageId(chatID int64, oldID int64, newMsg *telegram.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.messages[chatID]
	for i, m := range msgs {
		if m.ID == oldID {
			msgs[i] = newMsg
			return
		}
	}
	// If old ID not found, append — unless the message is already there
	// (it may have arrived via the update dispatcher first).
	for _, m := range msgs {
		if m.ID == newMsg.ID {
			return
		}
	}
	s.messages[chatID] = append(msgs, newMsg)
}

// OldestMessageId returns the oldest cached message ID for a chat.
func (s *MessageStore) OldestMessageId(chatID int64) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs := s.messages[chatID]
	if len(msgs) == 0 {
		return 0
	}
	return msgs[0].ID
}

// Count returns the number of cached messages for a chat.
func (s *MessageStore) Count(chatID int64) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages[chatID])
}
