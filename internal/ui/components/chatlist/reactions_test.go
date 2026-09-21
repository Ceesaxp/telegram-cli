package chatlist

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// unreadReactions is the chat's unread-reactions count as the store holds
// it, which is what the chat view reads when the chat is opened.
func unreadReactions(t *testing.T, m Model, chatID int64) int32 {
	t.Helper()
	entry, ok := m.store.Chats.Get(chatID)
	if !ok {
		t.Fatalf("chat %d is not in the store", chatID)
	}
	return entry.UnreadReactionsCount
}

// A reaction to the reader's message arriving live has to reach the
// store, or a chat that had none when the dialogs loaded is never cleared
// on open. The update cannot say how many, so all it promises is some.
func TestAnUnreadReactionMarksTheChat(t *testing.T) {
	m := newLoadedModel(t, "Ana")
	*m.dirty = false

	m, _ = m.Update(telegram.ChatUnreadReactionsMsg{ChatId: 1})

	if got := unreadReactions(t, m, 1); got <= 0 {
		t.Errorf("unread reactions = %d after an unread reaction, want some", got)
	}
	if !*m.dirty {
		t.Error("the list was not marked for a redraw")
	}
}

// Clearing the reactions leaves none, however many there were: the call
// reads every one in the chat. Anything left over would have the next
// open ask the server again for nothing.
func TestClearingTheReactionsZeroesTheCount(t *testing.T) {
	m := newLoadedModel(t, "Ana")
	m, _ = m.Update(telegram.ChatUnreadReactionsMsg{ChatId: 1})
	m, _ = m.Update(telegram.ChatUnreadReactionsMsg{ChatId: 1})
	*m.dirty = false

	m, _ = m.Update(telegram.ChatReactionsReadMsg{ChatId: 1})

	if got := unreadReactions(t, m, 1); got != 0 {
		t.Errorf("unread reactions = %d after the clear, want 0", got)
	}
	if !*m.dirty {
		t.Error("the list was not marked for a redraw")
	}
}
