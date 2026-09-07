package chatview

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// The other side reading a message is a live event, and the tick has to
// follow it live: the row is cached once drawn, so the receipt must both
// move the mark in the store and drop the cached row that still shows one
// tick.
func TestReadReceiptFlipsTheTickWithoutReopening(t *testing.T) {
	m := echoModel()
	m.store.Chats.Set(&telegram.Chat{ID: testChatID})
	sent := textMessage(5, 100, "are you there")
	sent.IsOutgoing = true
	m.store.Messages.Append(testChatID, sent)

	if out, _ := renderOne(m, sent); !strings.Contains(out, sendSent.glyph()) || strings.Contains(out, sendRead.glyph()) {
		t.Fatalf("before the receipt: %q, want a single tick", out)
	}

	m, _ = m.Update(telegram.ChatReadOutboxMsg{ChatId: testChatID, LastReadOutboxMessageId: 5})

	if out, _ := renderOne(m, sent); !strings.Contains(out, sendRead.glyph()) {
		t.Fatalf("after the receipt: %q, want a double tick", out)
	}
}

// A receipt for a chat that is not open still has to land in the store:
// it is what the tick is read from when that chat is opened later.
func TestReadReceiptForAnotherChatIsKept(t *testing.T) {
	m := echoModel()
	m.store.Chats.Set(&telegram.Chat{ID: 77})

	m, _ = m.Update(telegram.ChatReadOutboxMsg{ChatId: 77, LastReadOutboxMessageId: 12})

	if entry, _ := m.store.Chats.Get(77); entry.Chat.LastReadOutboxMessageID != 12 {
		t.Fatalf("outbox mark for the other chat = %d, want 12", entry.Chat.LastReadOutboxMessageID)
	}
}
