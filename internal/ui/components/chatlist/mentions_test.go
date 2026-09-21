package chatlist

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// mentionedModel is a loaded list whose one chat, the group 1, has three
// unread mentions and no unread messages: the mentions sit below the read
// mark, which the dialog allows.
func mentionedModel(t *testing.T) Model {
	t.Helper()
	m := newLoadedModel(t)
	m.store.Chats.Set(&telegram.Chat{
		ID:                  1,
		Title:               "infra-oncall",
		Type:                telegram.ChatTypeSupergroup,
		Order:               1,
		UnreadMentionsCount: 3,
	})
	m.refreshList()
	return m
}

// unreadMentions is the chat's unread-mentions count as the store holds it.
func unreadMentions(t *testing.T, m Model, chatID int64) int32 {
	t.Helper()
	entry, ok := m.store.Chats.Get(chatID)
	if !ok {
		t.Fatalf("chat %d is not in the store", chatID)
	}
	return entry.UnreadMentionsCount
}

// Clearing some mentions takes exactly those off: no update carries the
// count, so the list's arithmetic is all there is until the next reload.
func TestClearingSomeMentionsTakesThemOffTheCount(t *testing.T) {
	m := mentionedModel(t)
	*m.dirty = false

	m, _ = m.Update(telegram.ChatMentionsReadMsg{ChatId: 1, MessageIds: []int64{4, 5}})

	if got := unreadMentions(t, m, 1); got != 1 {
		t.Errorf("unread mentions = %d after clearing 2 of 3, want 1", got)
	}
	if !*m.dirty {
		t.Error("the list was not marked for a redraw")
	}
}

// Clearing every mention leaves none, however many the list had counted.
func TestClearingAllMentionsZeroesTheCount(t *testing.T) {
	m := mentionedModel(t)
	*m.dirty = false

	m, _ = m.Update(telegram.ChatMentionsReadMsg{ChatId: 1, All: true})

	if got := unreadMentions(t, m, 1); got != 0 {
		t.Errorf("unread mentions = %d after clearing them all, want 0", got)
	}
	if !*m.dirty {
		t.Error("the list was not marked for a redraw")
	}
}
