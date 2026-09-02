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

	// Unresolved marks an entry this store INVENTED to hold a message for
	// a chat nobody had described yet — see [ChatStore.UpdateLastMessage].
	// It has an id and nothing else: no name, no type, no mute flag.
	//
	// The flag exists because "the title is empty" is not the same
	// question. A real chat can have an empty title (a deleted account
	// has no name at all), and a row for one of those must not spend the
	// rest of the session claiming to be loading.
	Unresolved bool

	// MemberCount is a group or channel's total, 0 when nobody has asked
	// yet. It is NOT part of a dialog — a dialog says who a chat is, not
	// how many people are in it — so it arrives from a separate full-info
	// call, and [ChatStore.Set] and [ChatStore.Merge] leave it alone.
	//
	// It lives here rather than in the two components that want it so
	// they cannot disagree, and so the second one to need it does not
	// make the call again.
	MemberCount int32
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

// Set adds or updates a chat entry from a DIALOG — a complete view.
//
// A dialog knows everything about a chat: who it is, and also whether it is
// muted, pinned, how many messages are unread and where it sits in the list.
// So this replaces. [Merge] is for the other kind of update, and mixing the
// two up is what silently unmuted every chat the reader opened.
func (s *ChatStore) Set(chat *telegram.Chat) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.chats[chat.ID]
	if !exists {
		entry = &ChatEntry{}
		s.chats[chat.ID] = entry
	}
	entry.Chat = chat
	entry.Unresolved = false
	entry.UnreadCount = chat.UnreadCount
	entry.Pinned = chat.Pinned

	if chat.LastMessage != nil {
		entry.LastMessage = chat.LastMessage
	}
	if chat.Order != 0 {
		entry.Order = chat.Order
	}
}

// Merge updates a chat entry from a PEER — a partial view.
//
// Resolving a peer answers "who is this": the id, the type, the name, the
// username, the photo. It says nothing about the reader's relationship with
// the chat, because that lives in the dialog and in the account's notify
// settings, neither of which a peer lookup returns.
//
// Handing such a chat to [Set] replaced the entry wholesale, so every field
// the peer lookup had no opinion about was overwritten with a zero value.
// Opening a chat calls GetChat, so opening a chat cleared its mute flag, its
// unread count, its pin and its last message — and the first symptom anybody
// noticed was desktop notifications from a chat they had muted, which had
// been muted right up until they read it.
//
// The field list below is therefore explicit rather than "copy anything that
// looks set": a bool cannot tell "false" from "not known", and guessing from
// zero values is the same bug wearing a different hat. Muted IS in the list,
// because the fetch goes and asks for it — see Client.GetChat.
func (s *ChatStore) Merge(chat *telegram.Chat) {
	if chat == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.chats[chat.ID]
	if !exists || entry.Chat == nil {
		// Nothing to preserve: a peer view is better than no view.
		s.chats[chat.ID] = &ChatEntry{Chat: chat}
		return
	}

	entry.Unresolved = false
	entry.Chat.Type = chat.Type
	entry.Chat.Title = chat.Title
	entry.Chat.Username = chat.Username
	entry.Chat.Muted = chat.Muted
	if chat.Photo != nil {
		entry.Chat.Photo = chat.Photo
	}
}

// SetMemberCount records a chat's total membership.
//
// A zero is dropped rather than stored: every caller gets it from a
// full-info call, and a call that failed hands back the zero value. Storing
// that would replace a number this client knows with one it does not, and
// the header would go from "24 members" to silence on a refresh.
func (s *ChatStore) SetMemberCount(chatID int64, count int32) {
	if count <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.chats[chatID]
	if !ok {
		entry = &ChatEntry{Unresolved: true, Chat: &telegram.Chat{ID: chatID}}
		s.chats[chatID] = entry
	}
	entry.MemberCount = count
}

// MemberCount is a chat's total membership, 0 when it is not known.
func (s *ChatStore) MemberCount(chatID int64) int32 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if entry, ok := s.chats[chatID]; ok {
		return entry.MemberCount
	}
	return 0
}

// TotalUnread is how many unread messages there are across every chat.
//
// Summed on demand rather than kept as a running total: an incremental
// counter has to be right on every path that changes a chat's unread count,
// and the one that forgets leaves a number on screen that drifts further
// from the truth the longer the session runs.
func (s *ChatStore) TotalUnread() int32 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total int32
	for _, entry := range s.chats {
		if entry.UnreadCount > 0 {
			total += entry.UnreadCount
		}
	}
	return total
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
		// A message has arrived from a chat that is not in the dialog page
		// this client loaded. The row has to exist — something was said —
		// but everything except the id is unknown, so it is marked as such
		// and somebody has to go and ask. Before the flag, this produced a
		// nameless row that stayed nameless until the reader opened it,
		// which is the only thing that fetched the chat.
		entry = &ChatEntry{Chat: &telegram.Chat{ID: chatID}, Unresolved: true}
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

// SetMuted updates a chat's muted flag. It is a no-op if the chat is not
// yet known to the store.
func (s *ChatStore) SetMuted(chatID int64, muted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.chats[chatID]
	if !ok || entry.Chat == nil {
		return
	}
	entry.Chat.Muted = muted
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
