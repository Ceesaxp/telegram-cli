package chatlist

import (
	"fmt"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// badgeOf draws the list and returns the unread badge on chatID's row, as
// the reader would see it.
func badgeOf(t *testing.T, m Model, chatID int64) string {
	t.Helper()
	m.View()
	for _, it := range m.list.Items {
		if it.ID == fmt.Sprint(chatID) {
			return it.Badge
		}
	}
	t.Fatalf("chat %d has no row", chatID)
	return ""
}

// A message from the other side put its text in the preview and left the
// badge where it was, until the next dialog reload happened to fix it —
// the server does not send a fresh count with each message.
func TestAnIncomingMessageRaisesTheBadge(t *testing.T) {
	m := newLoadedModel(t, "Ana")

	m, _ = m.Update(telegram.ChatLastMessageMsg{
		ChatId:      1,
		LastMessage: &telegram.Message{ID: 5, ChatID: 1, Date: 1700000000},
	})

	if got := badgeOf(t, m, 1); got != "[1]" {
		t.Errorf("badge = %q after one incoming message, want [1]", got)
	}
}

// Reading in this client clears the badge on the client's own say-so. The
// server's receipt for this session's read does not reliably come back, so
// counting arrivals without this left a badge on the chat being read.
func TestReadingTheChatClearsTheBadge(t *testing.T) {
	m := newLoadedModel(t, "Ana")
	m, _ = m.Update(telegram.ChatLastMessageMsg{
		ChatId:      1,
		LastMessage: &telegram.Message{ID: 5, ChatID: 1, Date: 1700000000},
	})

	m, _ = m.Update(telegram.ChatMarkedReadMsg{ChatId: 1, MaxMessageId: 5})

	if got := badgeOf(t, m, 1); got != "" {
		t.Errorf("badge = %q after reading up to the last message, want none", got)
	}
}

// The server's receipt carries how far the reader has read, and the list
// has to hand that on: read on another device, then the message that read
// covered arrives late. It is not news, and it raises no badge.
func TestAMessageCoveredByAReceiptRaisesNoBadge(t *testing.T) {
	m := newLoadedModel(t, "Ana")
	m, _ = m.Update(telegram.ChatReadInboxMsg{ChatId: 1, LastReadInboxMessageId: 20})

	m, _ = m.Update(telegram.ChatLastMessageMsg{
		ChatId:      1,
		LastMessage: &telegram.Message{ID: 18, ChatID: 1, Date: 1700000000},
	})

	if got := badgeOf(t, m, 1); got != "" {
		t.Errorf("badge = %q for a message the receipt had already covered, want none", got)
	}
}

// unreadFolderModel is a list showing a folder of unread chats, holding
// Ana (1) above Bob (2), each with one unread message, 9. The cursor is on
// Ana and nothing is open.
func unreadFolderModel(t *testing.T) Model {
	t.Helper()
	m := newLoadedModel(t, "Ana", "Bob")
	for id, name := range map[int64]string{1: "Ana", 2: "Bob"} {
		m.store.Chats.Set(&telegram.Chat{
			ID: id, Title: name, Type: telegram.ChatTypePrivate, Order: 3 - id,
			UnreadCount: 1, LastMessage: &telegram.Message{ID: 9, ChatID: id},
		})
	}
	m.SetFolderForTest(&telegram.ChatFolder{
		ID: 5, Title: "Unread", Contacts: true, NonContacts: true, ExcludeRead: true,
	})
	if got := listTitles(m); len(got) != 2 {
		t.Fatalf("precondition: the folder shows %q, want both chats", got)
	}
	return m
}

// Opening a chat in a folder of unread chats reads it, and the row used to
// vanish a moment later with the cursor sliding onto another chat. Telegram
// keeps the open chat in such a folder until the reader leaves it.
func TestAnUnreadFolderKeepsTheOpenChatOnceRead(t *testing.T) {
	m := unreadFolderModel(t)
	if id, ok := m.OpenCursor(); !ok || id != 1 {
		t.Fatalf("precondition: opened (%d, %v), want Ana", id, ok)
	}

	m, _ = m.Update(telegram.ChatMarkedReadMsg{ChatId: 1, MaxMessageId: 9})
	m.View()

	if got := listTitles(m); len(got) != 2 || got[0] != "Ana" {
		t.Errorf("the folder shows %q after reading the open chat, want Ana kept", got)
	}
	if got := m.CursorChatId(); got != 1 {
		t.Errorf("the cursor slid to %d, want it on the open chat", got)
	}
}

// And once the reader moves on, the chat they read is no longer unread and
// no longer open, so it leaves the folder — without waiting for some other
// update to redraw the list.
func TestAnUnreadFolderDropsTheReadChatOnceAnotherIsOpened(t *testing.T) {
	m := unreadFolderModel(t)
	m.OpenCursor()
	m, _ = m.Update(telegram.ChatMarkedReadMsg{ChatId: 1, MaxMessageId: 9})
	m.View()

	if id, ok := m.SelectDelta(1); !ok || id != 2 {
		t.Fatalf("precondition: opened (%d, %v), want Bob", id, ok)
	}
	m.View()

	if got := listTitles(m); len(got) != 1 || got[0] != "Bob" {
		t.Errorf("the folder shows %q after moving on to Bob, want [Bob]", got)
	}
}

// u goes to the chats that show a badge. It read the dialog's snapshot
// instead, so a chat whose unread messages all arrived live was a badge u
// could not reach.
func TestNextUnreadVisitsAChatUnreadOnlySinceTheDialog(t *testing.T) {
	m := newLoadedModel(t, "Ana", "Bob")
	m, _ = m.Update(telegram.ChatLastMessageMsg{
		ChatId:      2,
		LastMessage: &telegram.Message{ID: 5, ChatID: 2, Date: 1700000000},
	})
	if got := badgeOf(t, m, 2); got == "" {
		t.Fatal("precondition: the live message raised no badge")
	}

	if got, ok := m.SelectNextUnread(); !ok || got != 2 {
		t.Errorf("u chose (%d, %v), want the chat with the badge, 2", got, ok)
	}
}

// And the other way round: a chat read in this client has no badge, even
// though the dialog it was loaded from said it had unread messages, and u
// must not stop there.
func TestNextUnreadSkipsAChatReadSinceTheDialog(t *testing.T) {
	m := newLoadedModel(t, "Ana", "Bob")
	m.store.Chats.Set(&telegram.Chat{
		ID: 2, Title: "Bob", Type: telegram.ChatTypePrivate, Order: 1,
		UnreadCount: 3, LastMessage: &telegram.Message{ID: 9, ChatID: 2},
	})
	m, _ = m.Update(telegram.ChatMarkedReadMsg{ChatId: 2, MaxMessageId: 9})
	if got := badgeOf(t, m, 2); got != "" {
		t.Fatalf("precondition: the read left a badge %q", got)
	}

	if got, ok := m.SelectNextUnread(); ok {
		t.Errorf("u chose %d, a chat with no badge", got)
	}
}
