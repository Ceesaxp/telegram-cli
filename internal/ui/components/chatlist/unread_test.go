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

	m, _ = m.Update(telegram.ChatMarkedReadMsg{ChatId: 1, MaxID: 5})

	if got := badgeOf(t, m, 1); got != "" {
		t.Errorf("badge = %q after reading up to the last message, want none", got)
	}
}
