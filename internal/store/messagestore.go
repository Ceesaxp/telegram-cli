package store

import (
	"sort"
	"sync"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

const defaultMessageBufferSize = 200

// MessageStore caches messages per chat.
type MessageStore struct {
	mu       sync.RWMutex
	messages map[int64][]*telegram.Message // chatID -> messages (newest last)

	// byID indexes the same messages by ID, chatID -> message ID -> message,
	// so one message can be found without copying and walking the history.
	// It holds exactly what messages holds: every write that puts a message
	// into a chat's history or takes one out does the same here.
	byID map[int64]map[int64]*telegram.Message

	maxSize      int
	activeChatID int64
}

func NewMessageStore() *MessageStore {
	return &MessageStore{
		messages: make(map[int64][]*telegram.Message),
		byID:     make(map[int64]map[int64]*telegram.Message),
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
	previous := s.activeChatID
	s.activeChatID = chatID
	if previous != 0 {
		// No longer the active chat, so storing it again is what trims it.
		s.storeLocked(previous, s.messages[previous])
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
			s.indexLocked(chatID, msg)
			s.storeLocked(chatID, msgs)
			return
		}
	}

	msgs = append(msgs, msg)
	s.indexLocked(chatID, msg)
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
	s.indexLocked(chatID, toAdd...)

	// The cap goes through storeLocked like every other write: cutting the
	// oldest off the front by reslicing would keep them in the array ahead
	// of the slice, unseen and uncollected.
	s.storeLocked(chatID, combined)
	dropped := len(combined) - len(s.messages[chatID])
	if dropped >= len(toAdd) {
		return nil
	}
	return append([]*telegram.Message(nil), toAdd[dropped:]...)
}

// Merge folds a freshly fetched page into whatever is cached, wherever each
// message belongs, and returns the ones that were new.
//
// [Prepend] is for paging backwards: everything it is handed is older than
// everything it has. A page fetched to catch up after a sync gap is the
// opposite shape — mostly newer than the cache, sometimes filling a hole in
// the middle of it, always overlapping what is already there — so it is
// placed by ID rather than by which end it arrived at. A message already
// cached is REPLACED by the server's copy, since an edit or a reaction that
// happened during the gap is on that copy and not on ours.
//
// Local echoes (negative IDs, see [ReplaceMessageId]) stay at the bottom
// after everything confirmed: their IDs order nothing, and the row belongs
// under the newest message the server has, which is what the reader sent
// it after.
func (s *MessageStore) Merge(chatID int64, msgs []*telegram.Message) []*telegram.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.messages[chatID]
	index := make(map[int64]int, len(existing))
	for i, m := range existing {
		index[m.ID] = i
	}

	var added []*telegram.Message
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if i, ok := index[m.ID]; ok {
			if i >= 0 {
				existing[i] = m
				s.indexLocked(chatID, m)
			}
			continue
		}
		index[m.ID] = -1 // seen, but not in existing
		added = append(added, m)
	}
	s.indexLocked(chatID, added...)
	if len(added) == 0 {
		s.storeLocked(chatID, existing)
		return nil
	}

	confirmed := make([]*telegram.Message, 0, len(existing)+len(added))
	var echoes []*telegram.Message
	for _, m := range append(existing, added...) {
		if m.ID > 0 {
			confirmed = append(confirmed, m)
		} else {
			echoes = append(echoes, m)
		}
	}
	sort.SliceStable(confirmed, func(i, j int) bool { return confirmed[i].ID < confirmed[j].ID })

	s.storeLocked(chatID, append(confirmed, echoes...))
	return append([]*telegram.Message(nil), added...)
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

// GetByID returns one cached message by its ID, and whether the chat holds
// it. It neither copies the history nor walks it, which is the point: a
// caller after a single message — the one a reply quotes, say — used to pay
// for [Get]'s copy of the whole chat to find it.
func (s *MessageStore) GetByID(chatID, messageID int64) (*telegram.Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.byID[chatID][messageID]
	return m, ok
}

// UpdateMessage replaces a message in the store (for edits).
func (s *MessageStore) UpdateMessage(chatID int64, messageID int64, newMsg *telegram.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.messages[chatID]
	for i, m := range msgs {
		if m.ID == messageID {
			msgs[i] = newMsg
			s.forgetLocked(chatID, []*telegram.Message{m}, msgs)
			s.indexLocked(chatID, newMsg)
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

	var gone []*telegram.Message
	filtered := msgs[:0]
	for _, m := range msgs {
		if _, del := idSet[m.ID]; del {
			gone = append(gone, m)
		} else {
			filtered = append(filtered, m)
		}
	}
	kept := shrunk(msgs, len(filtered))
	s.forgetLocked(chatID, gone, kept)
	s.storeLocked(chatID, kept)
}

// shrunk is msgs after an in-place filter has packed the survivors into its
// first n slots, with nothing left behind.
//
// Filtering in place moves the survivors down and leaves the tail of the
// array exactly as it was, so every message the filter dropped would still
// be reachable from it — invisible past the slice's length, and never
// collected. Those slots are cleared. And once the survivors fill less than
// a quarter of the array, the array itself goes: the open chat is never
// trimmed, and a history that was thousands long should not keep that room
// after being cut to a handful.
func shrunk(msgs []*telegram.Message, n int) []*telegram.Message {
	clear(msgs[n:])
	if n >= cap(msgs)/4 {
		return msgs[:n]
	}
	if n == 0 {
		return nil
	}
	kept := make([]*telegram.Message, n)
	copy(kept, msgs)
	return kept
}

// Clear removes all cached messages for a chat.
func (s *MessageStore) Clear(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.messages, chatID)
	delete(s.byID, chatID)
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
			kept := append(msgs[:i:i], msgs[i+1:]...)
			s.forgetLocked(chatID, []*telegram.Message{m}, kept)
			s.storeLocked(chatID, kept)
			return
		}
		msgs[i] = newMsg
		s.forgetLocked(chatID, []*telegram.Message{m}, msgs)
		s.indexLocked(chatID, newMsg)
		s.storeLocked(chatID, msgs)
		return
	}

	// If old ID not found, append — unless the message is already there
	// (it may have arrived via the update dispatcher first).
	if alreadyThere {
		s.storeLocked(chatID, msgs)
		return
	}
	s.indexLocked(chatID, newMsg)
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

// indexLocked records messages that have just gone into chatID's history.
func (s *MessageStore) indexLocked(chatID int64, msgs ...*telegram.Message) {
	if len(msgs) == 0 {
		return
	}
	index := s.byID[chatID]
	if index == nil {
		index = make(map[int64]*telegram.Message)
		s.byID[chatID] = index
	}
	for _, m := range msgs {
		index[m.ID] = m
	}
}

// forgetLocked takes messages that have just left chatID's history out of
// its index; kept is the history they left.
//
// An entry goes only if it is still that very message, so a swap that
// indexes the incoming message before forgetting the outgoing one under the
// same ID cannot lose it. And when more went than stayed, the index is built
// again from what is left instead: a Go map never gives back the room it
// grew into, and an index sized for a history paged thousands deep should
// not outlive it.
func (s *MessageStore) forgetLocked(chatID int64, gone, kept []*telegram.Message) {
	if len(gone) == 0 {
		return
	}
	if len(gone) > len(kept) {
		delete(s.byID, chatID)
		s.indexLocked(chatID, kept...)
		return
	}
	index := s.byID[chatID]
	for _, m := range gone {
		if index[m.ID] == m {
			delete(index, m.ID)
		}
	}
}

// storeLocked makes msgs chatID's history, trimmed to the background cap
// unless it is the chat being read.
func (s *MessageStore) storeLocked(chatID int64, msgs []*telegram.Message) {
	if chatID != s.activeChatID {
		kept := trimNewest(msgs, s.maxSize)
		s.forgetLocked(chatID, msgs[:len(msgs)-len(kept)], kept)
		msgs = kept
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
