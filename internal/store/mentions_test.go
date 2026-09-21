package store

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// The dialog is the only thing that says how many unread mentions a chat
// has, and the entry keeps it the way it keeps the unread count.
func TestSetTakesTheDialogsUnreadMentions(t *testing.T) {
	s := NewChatStore()
	chat := dialogChat()
	chat.UnreadMentionsCount = 3

	s.Set(chat)

	if entry, _ := s.Get(7); entry.UnreadMentionsCount != 3 {
		t.Errorf("UnreadMentionsCount = %d, want the dialog's 3", entry.UnreadMentionsCount)
	}
}

// A peer lookup knows nothing about mentions, and opening a chat does one.
// Taking its zero would drop the @ on every open, before anything had
// cleared a single mention.
func TestMergeKeepsTheUnreadMentions(t *testing.T) {
	s := NewChatStore()
	chat := dialogChat()
	chat.UnreadMentionsCount = 3
	s.Set(chat)

	s.Merge(peerChat())

	if got := mentionCount(t, s, 7); got != 3 {
		t.Errorf("UnreadMentionsCount = %d after a peer merge, want 3", got)
	}
}

func mentionCount(t *testing.T, s *ChatStore, chatID int64) int32 {
	t.Helper()
	entry, ok := s.Get(chatID)
	if !ok {
		t.Fatalf("chat %d is not in the store", chatID)
	}
	return entry.UnreadMentionsCount
}

// No update carries a chat's mention count, so a mention arriving has to
// raise it here, or the @ appears only on the next dialog reload.
func TestAnArrivingMentionCountsAsAnUnreadMention(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())

	s.UpdateLastMessage(7, &telegram.Message{ID: 13, ChatID: 7, UnreadMention: true})

	if got := mentionCount(t, s, 7); got != 1 {
		t.Errorf("UnreadMentionsCount = %d after one arriving mention, want 1", got)
	}
}

// A getDifference replay delivers the same message again, and an older one
// can arrive late. Neither is a new mention, for the reason neither is a
// new unread message.
func TestAReplayedMentionDoesNotCountTwice(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())
	mention := &telegram.Message{ID: 13, ChatID: 7, UnreadMention: true}

	s.UpdateLastMessage(7, mention)
	s.UpdateLastMessage(7, mention)
	s.UpdateLastMessage(7, &telegram.Message{ID: 11, ChatID: 7, UnreadMention: true})

	if got := mentionCount(t, s, 7); got != 1 {
		t.Errorf("UnreadMentionsCount = %d after one mention and two replays, want 1", got)
	}
}

// What the reader sent is not something that mentions them to be read.
func TestAnOutgoingMessageIsNotAnUnreadMention(t *testing.T) {
	s := NewChatStore()
	s.Set(unreadChat())

	s.UpdateLastMessage(7, &telegram.Message{ID: 13, ChatID: 7, IsOutgoing: true, UnreadMention: true})

	if got := mentionCount(t, s, 7); got != 0 {
		t.Errorf("UnreadMentionsCount = %d after the reader's own message, want 0", got)
	}
}

// Clearing some mentions takes that many off. The count is this client's
// own arithmetic on top of the dialog's, so it can be behind what the
// server cleared, and a clear bigger than the count leaves none rather
// than a negative number.
func TestMarkMentionsReadTakesOffWhatWasCleared(t *testing.T) {
	s := NewChatStore()
	chat := dialogChat()
	chat.UnreadMentionsCount = 3
	s.Set(chat)

	s.MarkMentionsRead(7, 2)
	if got := mentionCount(t, s, 7); got != 1 {
		t.Errorf("UnreadMentionsCount = %d after clearing 2 of 3, want 1", got)
	}

	s.MarkMentionsRead(7, 5)
	if got := mentionCount(t, s, 7); got != 0 {
		t.Errorf("UnreadMentionsCount = %d after clearing more than were left, want 0", got)
	}
}

// Clearing every mention in a chat leaves none, whatever the count was.
func TestClearMentionsLeavesNone(t *testing.T) {
	s := NewChatStore()
	chat := dialogChat()
	chat.UnreadMentionsCount = 3
	s.Set(chat)

	s.ClearMentions(7)

	if got := mentionCount(t, s, 7); got != 0 {
		t.Errorf("UnreadMentionsCount = %d after clearing them all, want 0", got)
	}
}

// Clearing mentions describes a chat; it does not introduce one. The row,
// if the chat ever gets one, comes from its dialog, which carries the
// count.
func TestClearingMentionsInAnUnknownChatChangesNothing(t *testing.T) {
	s := NewChatStore()

	s.MarkMentionsRead(7, 1)
	s.ClearMentions(7)

	if _, ok := s.Get(7); ok {
		t.Error("clearing mentions invented an entry for a chat the store did not know")
	}
}
