package store

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// dialogChat is what a chat looks like when it came off the dialog list:
// everything known, including the parts only a dialog carries.
func dialogChat() *telegram.Chat {
	return &telegram.Chat{
		ID:                     7,
		Type:                   telegram.ChatTypePrivate,
		Title:                  "Ana",
		Muted:                  true,
		Pinned:                 true,
		UnreadCount:            4,
		LastReadInboxMessageID: 90,
		Order:                  1700000000,
		LastMessage:            &telegram.Message{ID: 99, ChatID: 7},
	}
}

// peerChat is what a chat looks like when it came from resolving a peer:
// who it is, and nothing about the reader's relationship with it.
func peerChat() *telegram.Chat {
	return &telegram.Chat{
		ID:    7,
		Type:  telegram.ChatTypePrivate,
		Title: "Ana Marić",
		Muted: true,
	}
}

// Opening a chat unmuted it.
//
// GetChat resolves a peer, which reports who a chat is and nothing else, and
// the result went through Set — which replaces the entry. So every field the
// peer lookup had no opinion about was overwritten with a zero value, and the
// first one anybody noticed was the mute flag: desktop notifications started
// arriving from a chat that had been muted right up until it was read.
func TestMergeKeepsWhatAPeerLookupDoesNotKnow(t *testing.T) {
	s := NewChatStore()
	s.Set(dialogChat())

	s.Merge(peerChat())

	entry, ok := s.Get(7)
	if !ok {
		t.Fatal("the chat is gone")
	}
	if !entry.Chat.Muted {
		t.Error("merging a peer view unmuted the chat")
	}
	if !entry.Pinned {
		t.Error("merging a peer view unpinned the chat")
	}
	if entry.UnreadCount != 4 {
		t.Errorf("unread count = %d, want 4", entry.UnreadCount)
	}
	if entry.Order != 1700000000 {
		t.Errorf("order = %d, want the dialog's", entry.Order)
	}
	if entry.LastMessage == nil || entry.LastMessage.ID != 99 {
		t.Error("merging a peer view dropped the last message")
	}

	// The same fields on the Chat itself, which are a separate copy and
	// were the ones a wholesale replacement actually lost. The read marker
	// is the one that shows: the chat view places the unread divider from
	// Chat.LastReadInboxMessageID, so losing it puts the divider at the
	// top of the history — on the chat the reader has just opened.
	if entry.Chat.LastReadInboxMessageID != 90 {
		t.Errorf("Chat.LastReadInboxMessageID = %d, want 90 — the unread "+
			"divider lost its position", entry.Chat.LastReadInboxMessageID)
	}
	if !entry.Chat.Pinned {
		t.Error("Chat.Pinned was cleared")
	}
	if entry.Chat.UnreadCount != 4 {
		t.Errorf("Chat.UnreadCount = %d, want 4", entry.Chat.UnreadCount)
	}
	if entry.Chat.LastMessage == nil || entry.Chat.Order == 0 {
		t.Error("Chat.LastMessage or Chat.Order was cleared")
	}
}

// What it DOES know, it updates — that is the point of the fetch.
func TestMergeTakesTheIdentityItCameFor(t *testing.T) {
	s := NewChatStore()
	s.Set(dialogChat())

	s.Merge(peerChat())

	entry, _ := s.Get(7)
	if entry.Chat.Title != "Ana Marić" {
		t.Errorf("title = %q, want the fetched one", entry.Chat.Title)
	}
}

// The mute flag is in the merged set, because GetChat goes and asks for it.
// A chat outside the loaded dialog page has no other way to learn it.
func TestMergeCarriesTheMuteFlagItFetched(t *testing.T) {
	s := NewChatStore()
	s.UpdateLastMessage(7, &telegram.Message{ID: 1, ChatID: 7})

	fetched := peerChat()
	fetched.Muted = true
	s.Merge(fetched)

	entry, _ := s.Get(7)
	if !entry.Chat.Muted {
		t.Error("the fetched mute flag did not reach the store")
	}

	// And unmuting through the same path works: this is a real value, not
	// a "leave it alone if false" guess.
	fetched2 := peerChat()
	fetched2.Muted = false
	s.Merge(fetched2)
	if entry, _ = s.Get(7); entry.Chat.Muted {
		t.Error("a fetch reporting unmuted was ignored")
	}
}

// A dialog IS complete, so it still replaces — including clearing an unread
// count down to zero, which a merge would have had no way to express.
func TestSetStillReplaces(t *testing.T) {
	s := NewChatStore()
	s.Set(dialogChat())

	read := dialogChat()
	read.UnreadCount = 0
	read.Muted = false
	read.Pinned = false
	s.Set(read)

	entry, _ := s.Get(7)
	if entry.UnreadCount != 0 || entry.Pinned || entry.Chat.Muted {
		t.Errorf("a dialog update did not replace: unread=%d pinned=%v muted=%v",
			entry.UnreadCount, entry.Pinned, entry.Chat.Muted)
	}
}

// Merging into nothing is still better than nothing.
func TestMergeIntoAnUnknownChat(t *testing.T) {
	s := NewChatStore()
	s.Merge(peerChat())

	entry, ok := s.Get(7)
	if !ok || entry.Chat.Title != "Ana Marić" {
		t.Error("merging a peer view into an empty store stored nothing")
	}
	if entry.Unresolved {
		t.Error("a chat that was just fetched is still marked unresolved")
	}
}

// A message from a chat this client has never been told about has to produce
// a row — something was said — but the row knows only an id. Marking it says
// so, which is what lets the list ask, and lets it draw a placeholder rather
// than an empty line.
func TestAMessageFromAnUnknownChatMakesAnUnresolvedEntry(t *testing.T) {
	s := NewChatStore()
	s.UpdateLastMessage(7, &telegram.Message{ID: 1, ChatID: 7, Date: 1700000000})

	entry, ok := s.Get(7)
	if !ok {
		t.Fatal("the message produced no entry at all")
	}
	if !entry.Unresolved {
		t.Error("the invented entry is not marked unresolved")
	}
	if entry.Order != 1700000000 {
		t.Errorf("order = %d, want the message date", entry.Order)
	}

	// And a message for a chat we DO know does not mark it.
	s.Set(dialogChat())
	s.UpdateLastMessage(7, &telegram.Message{ID: 2, ChatID: 7})
	if entry, _ = s.Get(7); entry.Unresolved {
		t.Error("a message marked a known chat as unresolved")
	}
}

// Both ways of learning who a chat is clear the flag, or the row would ask
// again on every message for the rest of the session.
func TestResolvingClearsTheFlag(t *testing.T) {
	tests := map[string]func(*ChatStore){
		"a dialog":     func(s *ChatStore) { s.Set(dialogChat()) },
		"a peer fetch": func(s *ChatStore) { s.Merge(peerChat()) },
	}

	for name, resolve := range tests {
		t.Run(name, func(t *testing.T) {
			s := NewChatStore()
			s.UpdateLastMessage(7, &telegram.Message{ID: 1, ChatID: 7})
			resolve(s)

			if entry, _ := s.Get(7); entry.Unresolved {
				t.Error("the entry is still marked unresolved")
			}
		})
	}
}
