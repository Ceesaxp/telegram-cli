package store

import (
	"sync"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

const defaultMessageBufferSize = 200

// MessageStore caches messages per chat.
type MessageStore struct {
	mu           sync.RWMutex
	messages     map[int64][]*telegram.Message // chatID -> messages (newest last)
	maxSize      int
	activeChatID int64
}

func NewMessageStore() *MessageStore {
	return &MessageStore{
		messages: make(map[int64][]*telegram.Message),
		maxSize:  defaultMessageBufferSize,
	}
}

// Activate makes chatID the one pageable chat. Its history may grow while the
// reader explicitly walks backwards; when the reader leaves, the previous
// chat is reduced to the same newest-message bound as every background chat.
func (s *MessageStore) Activate(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeChatID == chatID {
		return
	}
	if s.activeChatID != 0 {
		previous := s.activeChatID
		s.messages[previous] = trimNewest(s.messages[previous], s.maxSize)
	}
	s.activeChatID = chatID
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
			s.storeLocked(chatID, msgs)
			return
		}
	}

	msgs = append(msgs, msg)
	s.storeLocked(chatID, msgs)
}

// Prepend adds older messages to the beginning of the chat's message list and
// returns the newly inserted messages that survived the cache policy.
func (s *MessageStore) Prepend(chatID int64, msgs []*telegram.Message) []*telegram.Message {
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
			idSet[m.ID] = struct{}{}
		}
	}

	combined := make([]*telegram.Message, 0, len(toAdd)+len(existing))
	combined = append(combined, toAdd...)
	combined = append(combined, existing...)

	dropped := 0
	if chatID != s.activeChatID && len(combined) > s.maxSize {
		dropped = len(combined) - s.maxSize
		combined = combined[dropped:]
	}

	s.messages[chatID] = combined
	if dropped >= len(toAdd) {
		return nil
	}
	return append([]*telegram.Message(nil), toAdd[dropped:]...)
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
			s.storeLocked(chatID, msgs)
			return
		}
	}
	s.storeLocked(chatID, msgs)
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
	s.storeLocked(chatID, filtered)
}

// Clear removes all cached messages for a chat.
func (s *MessageStore) Clear(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.messages, chatID)
	if s.activeChatID == chatID {
		s.activeChatID = 0
	}
}

// ReplaceMessageId replaces a temporary message ID with the real one (after send).
//
// The update dispatcher races the send: the server's copy of the message
// can be appended here before the send call returns. Both halves of this
// therefore have to cope with the confirmed message already being present —
// the placeholder is dropped rather than swapped, because swapping it would
// leave the thread showing the same message twice.
func (s *MessageStore) ReplaceMessageId(chatID int64, oldID int64, newMsg *telegram.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.messages[chatID]
	alreadyThere := false
	for _, m := range msgs {
		if m.ID == newMsg.ID {
			alreadyThere = true
			break
		}
	}

	for i, m := range msgs {
		if m.ID != oldID {
			continue
		}
		if alreadyThere {
			// The dispatcher won the race. Remove the placeholder row and
			// keep the copy that is already in the right place.
			s.storeLocked(chatID, append(msgs[:i:i], msgs[i+1:]...))
			return
		}
		msgs[i] = newMsg
		s.storeLocked(chatID, msgs)
		return
	}

	// If old ID not found, append — unless the message is already there
	// (it may have arrived via the update dispatcher first).
	if alreadyThere {
		s.storeLocked(chatID, msgs)
		return
	}
	s.storeLocked(chatID, append(msgs, newMsg))
}

// MarkSendFailed flags a locally echoed message as one whose send failed,
// and reports whether the row was still there to flag. The placeholder can
// legitimately be gone by the time a failure lands — the chat was cleared,
// or the cache trimmed past it — and that is not an error.
//
// The flag is set here rather than by the caller mutating the message it
// found, because every other write to a stored message goes through the
// store's lock and this one has no reason to be the exception.
func (s *MessageStore) MarkSendFailed(chatID int64, messageID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.messages[chatID] {
		if m.ID == messageID {
			m.SendFailed = true
			return true
		}
	}
	return false
}

func (s *MessageStore) storeLocked(chatID int64, msgs []*telegram.Message) {
	if chatID != s.activeChatID {
		msgs = trimNewest(msgs, s.maxSize)
	}
	s.messages[chatID] = msgs
}

func trimNewest(msgs []*telegram.Message, maxSize int) []*telegram.Message {
	if maxSize >= 0 && len(msgs) > maxSize {
		trimmed := make([]*telegram.Message, maxSize)
		copy(trimmed, msgs[len(msgs)-maxSize:])
		return trimmed
	}
	return msgs
}

// OldestMessageId returns the oldest cached message ID for a chat.
//
// Locally echoed sends are skipped: their IDs are negative placeholders
// this client invented, and handing one to Telegram as the point to page
// backwards from asks for history either side of a message the server has
// never heard of.
func (s *MessageStore) OldestMessageId(chatID int64) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, m := range s.messages[chatID] {
		if m.ID > 0 {
			return m.ID
		}
	}
	return 0
}

// Count returns the number of cached messages for a chat.
func (s *MessageStore) Count(chatID int64) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages[chatID])
}
