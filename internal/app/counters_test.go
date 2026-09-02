package app

import (
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// TestTheHintBarCountsOnlyWhatItHas. A client with nothing unread should
// not spend six cells saying "0 unread", and a client with no chat open has
// no history to size.
func TestTheHintBarCountsOnlyWhatItHas(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m.chatList.MarkLoadedForTest()

	if got := m.hintBarCounters(); got != "0 buffers" {
		t.Fatalf("an empty client said %q", got)
	}

	m.store.Chats.Set(&telegram.Chat{ID: 1, Title: "infra-oncall", Type: telegram.ChatTypeSupergroup})
	m.store.Chats.Set(&telegram.Chat{ID: 2, Title: "relay", Type: telegram.ChatTypeSupergroup, UnreadCount: 2})
	m.chatList.MarkLoadedForTest()
	m.chatList.SetSize(38, 20)
	_ = m.chatList.View() // the list only counts what it has drawn

	if got := m.hintBarCounters(); !strings.Contains(got, "2 buffers") ||
		!strings.Contains(got, "2 unread") || strings.Contains(got, "idx") {
		t.Fatalf("got %q", got)
	}

	m.chatView.OpenChat(1, "infra-oncall")
	for i := range 3 {
		m.store.Messages.Append(1, &telegram.Message{
			ID: int64(i + 1), ChatID: 1,
			SenderID: &telegram.MessageSenderUser{UserID: 9},
			Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hi"}},
		})
	}
	if got := m.hintBarCounters(); !strings.HasPrefix(got, "idx 3 msgs · ") {
		t.Fatalf("got %q, want the history size first", got)
	}
}

// TestTheCountersReadInPriorityOrder. The bar cuts from the left, so what
// is unread outlives how many chats there are, which outlives the size of
// the history already on screen.
func TestTheCountersReadInPriorityOrder(t *testing.T) {
	m := mainModel(t, PanelChatList)
	m.store.Chats.Set(&telegram.Chat{ID: 1, Title: "a", Type: telegram.ChatTypeSupergroup, UnreadCount: 4})
	m.chatList.MarkLoadedForTest()
	m.chatList.SetSize(38, 20)
	_ = m.chatList.View()
	m.chatView.OpenChat(1, "a")
	m.store.Messages.Append(1, &telegram.Message{
		ID: 1, ChatID: 1,
		SenderID: &telegram.MessageSenderUser{UserID: 9},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hi"}},
	})

	got := m.hintBarCounters()
	idx, buffers, unread := strings.Index(got, "idx"), strings.Index(got, "buffers"), strings.Index(got, "unread")
	if idx < 0 || buffers < 0 || unread < 0 {
		t.Fatalf("got %q, want all three counts", got)
	}
	if !(idx < buffers && buffers < unread) {
		t.Fatalf("got %q, want the counts in priority order", got)
	}
}
