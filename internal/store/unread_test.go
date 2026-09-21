package store

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// unreadChat is a chat as a dialog describes it: two unread messages after
// the read mark at 10, the newest of them 12.
func unreadChat() *telegram.Chat {
	return &telegram.Chat{
		ID:                     7,
		Type:                   telegram.ChatTypePrivate,
		Title:                  "Ana",
		UnreadCount:            2,
		LastReadInboxMessageID: 10,
		LastMessage:            &telegram.Message{ID: 12, ChatID: 7},
	}
}

func unreadCount(t *testing.T, s *ChatStore, chatID int64) int32 {
	t.Helper()
	entry, ok := s.Get(chatID)
	if !ok {
		t.Fatalf("chat %d is not in the store", chatID)
	}
	return entry.UnreadCount
}

// MTProto does not push a fresh unread count with every message the way
// TDLib did; the client has to count arrivals itself. Without this the
// preview changed and the badge did not, until the next dialog reload.
func TestAnIncomingNewerMessageCountsAsUnread(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())

	s.UpdateLastMessage(7, &telegram.Message{ID: 13, ChatID: 7})

	if got := unreadCount(t, s, 7); got != 3 {
		t.Errorf("unread = %d after one new incoming message, want 3", got)
	}
}

// What the reader sent is not something they have to read.
func TestAnOutgoingMessageDoesNotCountAsUnread(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())

	s.UpdateLastMessage(7, &telegram.Message{ID: 13, ChatID: 7, IsOutgoing: true})

	if got := unreadCount(t, s, 7); got != 2 {
		t.Errorf("unread = %d after the reader's own message, want 2", got)
	}
}

// The same message can arrive twice — a getDifference replay — and an
// older one can arrive late. Neither is news, so neither may count again.
func TestARedeliveredMessageDoesNotCountTwice(t *testing.T) {
	for name, id := range map[string]int64{"the same message": 12, "an older one": 11} {
		t.Run(name, func(t *testing.T) {
			s := NewChatStore()
			s.Set(unreadChat())

			s.UpdateLastMessage(7, &telegram.Message{ID: id, ChatID: 7})

			if got := unreadCount(t, s, 7); got != 2 {
				t.Errorf("unread = %d after message %d arrived again, want 2", got, id)
			}
		})
	}
}

// A message the reader has already read is not unread, however late it
// turns up. A replay can deliver the read mark before the messages it
// covers — read on another device, then the gap filled in.
func TestAMessageAtOrBelowTheReadMarkDoesNotCount(t *testing.T) {
	for _, id := range []int64{14, 15} {
		s := NewChatStore()
		chat := unreadChat()
		chat.UnreadCount = 0
		chat.LastReadInboxMessageID = 15
		s.Set(chat)

		s.UpdateLastMessage(7, &telegram.Message{ID: id, ChatID: 7})

		if got := unreadCount(t, s, 7); got != 0 {
			t.Errorf("unread = %d after message %d with the read mark at 15, want 0", got, id)
		}
	}
}

// A message without a server ID — a local echo, which is negative — is not
// one the server will ever mark read, so it must not count even where
// nothing else about the entry would stop it.
func TestAMessageWithoutAServerIDDoesNotCount(t *testing.T) {
	s := NewChatStore()
	s.chats[7] = &ChatEntry{}

	s.UpdateLastMessage(7, &telegram.Message{ID: -3, ChatID: 7})

	if got := unreadCount(t, s, 7); got != 0 {
		t.Errorf("unread = %d after a message with ID -3, want 0", got)
	}
}

// A chat the store has never seen, announced by a message from the other
// side, has exactly that one message unread.
func TestAMessageFromAnUnknownChatStartsItAtOneUnread(t *testing.T) {
	s := NewChatStore()

	s.UpdateLastMessage(7, &telegram.Message{ID: 40, ChatID: 7})

	entry, ok := s.Get(7)
	if !ok || !entry.Unresolved {
		t.Fatalf("the message did not make an unresolved entry: %v, %+v", ok, entry)
	}
	if entry.UnreadCount != 1 {
		t.Errorf("unread = %d, want 1", entry.UnreadCount)
	}
}

// Whether a message counts is a question about the badge only. The preview
// and the position in the list follow the message either way.
func TestAMessageThatDoesNotCountStillBecomesThePreview(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())

	sent := &telegram.Message{ID: 13, ChatID: 7, Date: 1700000500, IsOutgoing: true}
	s.UpdateLastMessage(7, sent)

	entry, _ := s.Get(7)
	if entry.LastMessage != sent {
		t.Errorf("preview = %+v, want the message just sent", entry.LastMessage)
	}
	if entry.Order != 1700000500 {
		t.Errorf("order = %d, want the message date", entry.Order)
	}
}

// The server's read receipt says two things: how many are still unread,
// which stays the authority it always was, and how far the reader has read,
// which the counting above needs. The mark only moves forward — receipts
// can arrive out of order — and a receipt for an unknown chat invents
// nothing.
func TestUpdateReadInboxMovesTheReadMarkForwardAndTakesTheCount(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())

	s.UpdateReadInbox(7, 12, 0)
	entry, _ := s.Get(7)
	if entry.Chat.LastReadInboxMessageID != 12 || entry.UnreadCount != 0 {
		t.Fatalf("after reading to 12: mark %d, unread %d; want 12, 0",
			entry.Chat.LastReadInboxMessageID, entry.UnreadCount)
	}

	s.UpdateReadInbox(7, 11, 1)
	entry, _ = s.Get(7)
	if entry.Chat.LastReadInboxMessageID != 12 {
		t.Errorf("an older receipt moved the mark back to %d", entry.Chat.LastReadInboxMessageID)
	}
	if entry.UnreadCount != 1 {
		t.Errorf("unread = %d, want the server's 1", entry.UnreadCount)
	}

	s.UpdateReadInbox(8, 5, 3)
	if _, ok := s.Get(8); ok {
		t.Error("a receipt for an unknown chat invented one")
	}
}

// Which is what the mark is for: read on another device, then the messages
// that read covered arrive late. They are not news.
func TestAMessageCoveredByAReadReceiptDoesNotCount(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())
	s.UpdateReadInbox(7, 20, 0)

	s.UpdateLastMessage(7, &telegram.Message{ID: 18, ChatID: 7})

	if got := unreadCount(t, s, 7); got != 0 {
		t.Errorf("unread = %d after a message the reader had already read, want 0", got)
	}
}

// Reading in this client clears the badge without waiting for the server
// to say so, which for the reading session it effectively never does.
// Reading as far as the newest message leaves nothing unread.
func TestMarkReadUpToTheLastMessageClearsTheCount(t *testing.T) {
	for _, maxID := range []int64{12, 13} {
		s := NewChatStore()
		s.Set(unreadChat())

		s.MarkReadUpTo(7, maxID)

		entry, _ := s.Get(7)
		if entry.UnreadCount != 0 {
			t.Errorf("unread = %d after reading to %d with the last message at 12, want 0",
				entry.UnreadCount, maxID)
		}
		if entry.Chat.LastReadInboxMessageID != maxID {
			t.Errorf("read mark = %d after reading to %d", entry.Chat.LastReadInboxMessageID, maxID)
		}
	}
}

// Reading part of the way leaves some unread, but how many is not
// something this client can work out: message IDs are not dense. The count
// stays as it was until the next dialog reload corrects it; the mark still
// moves, so what was read does not count again.
func TestMarkReadUpToShortOfTheLastMessageKeepsTheCount(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())

	s.MarkReadUpTo(7, 11)

	entry, _ := s.Get(7)
	if entry.UnreadCount != 2 {
		t.Errorf("unread = %d after reading to 11 of 12, want 2 left alone", entry.UnreadCount)
	}
	if entry.Chat.LastReadInboxMessageID != 11 {
		t.Errorf("read mark = %d, want 11", entry.Chat.LastReadInboxMessageID)
	}
}

// Marking an older message read — the reader scrolled back — does not
// un-read the newer ones the mark already covers.
func TestMarkReadUpToNeverMovesTheMarkBack(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())

	s.MarkReadUpTo(7, 9)

	entry, _ := s.Get(7)
	if entry.Chat.LastReadInboxMessageID != 10 {
		t.Errorf("read mark = %d after marking 9 read, want 10 kept", entry.Chat.LastReadInboxMessageID)
	}
}

// Reading describes a chat; it does not introduce one.
func TestMarkReadUpToAnUnknownChatDoesNothing(t *testing.T) {
	s := NewChatStore()

	s.MarkReadUpTo(8, 5)

	if _, ok := s.Get(8); ok {
		t.Error("marking an unknown chat read invented it")
	}
}

// A chat known only from a peer lookup has no last message, yet a receipt
// can still give it an unread count. Reading it leaves nothing unread —
// there is no newer message the read could have fallen short of.
func TestMarkReadUpToAChatWithNoLastMessageClearsTheCount(t *testing.T) {
	s := NewChatStore()
	s.Merge(peerChat())
	s.UpdateReadInbox(7, 2, 3)

	s.MarkReadUpTo(7, 5)

	if got := unreadCount(t, s, 7); got != 0 {
		t.Errorf("unread = %d after reading a chat with no last message, want 0", got)
	}
}
