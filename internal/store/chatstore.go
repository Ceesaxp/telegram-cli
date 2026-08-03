package store

import (
	"sort"
	"sync"

	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// ChatEntry holds chat metadata and its position in the main list.
type ChatEntry struct {
	Chat        *telegram.Chat
	LastMessage *telegram.Message
	UnreadCount int32
	Pinned      bool
	Order       int64
}

// ChatStore is a thread-safe in-memory cache of chats.
type ChatStore struct {
	mu    sync.RWMutex
	chats map[int64]*ChatEntry
}

func NewChatStore() *ChatStore {
	return &ChatStore{
		chats: make(map[int64]*ChatEntry),
	}
}

// Set adds or updates a chat entry.
func (s *ChatStore) Set(chat *telegram.Chat) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.chats[chat.ID]
	if !exists {
		entry = &ChatEntry{}
		s.chats[chat.ID] = entry
	}
	entry.Chat = chat
	entry.UnreadCount = chat.UnreadCount
	entry.Pinned = chat.Pinned

	if chat.LastMessage != nil {
		entry.LastMessage = chat.LastMessage
	}
	if chat.Order != 0 {
		entry.Order = chat.Order
	}
}

// Get returns a chat entry by ID.
func (s *ChatStore) Get(chatID int64) (*ChatEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.chats[chatID]
	return entry, ok
}

// UpdateLastMessage updates a chat's last message and sort order.
func (s *ChatStore) UpdateLastMessage(chatID int64, msg *telegram.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.chats[chatID]
	if !ok {
		entry = &ChatEntry{Chat: &telegram.Chat{ID: chatID}}
		s.chats[chatID] = entry
	}

	entry.LastMessage = msg
	if msg != nil {
		entry.Order = int64(msg.Date)
	}
}

// UpdateReadInbox updates the unread count for a chat.
func (s *ChatStore) UpdateReadInbox(chatID int64, unreadCount int32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.chats[chatID]; ok {
		entry.UnreadCount = unreadCount
	}
}

// OrderedChats returns all chats: pinned first, then by last message
// date (descending).
func (s *ChatStore) OrderedChats() []*ChatEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]*ChatEntry, 0, len(s.chats))
	for _, entry := range s.chats {
		if entry.Chat != nil {
			entries = append(entries, entry)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Pinned != entries[j].Pinned {
			return entries[i].Pinned
		}
		return entries[i].Order > entries[j].Order
	})

	return entries
}

// Count returns the number of cached chats.
func (s *ChatStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chats)
}
