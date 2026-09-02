package app

import (
	tea "charm.land/bubbletea/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatlist"
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

// TestOpeningAChatNumbersItImmediately.
//
// The buffer number was refreshed on the one-second chrome tick, so a
// freshly opened chat spent up to a second with no number on its header —
// and switching between rows could show the previous chat's, which is the
// exact stale state the field exists to avoid.
func TestOpeningAChatNumbersItImmediately(t *testing.T) {
	m := mainModel(t, PanelChatList)
	for id, title := range map[int64]string{1: "infra-oncall", 2: "relay-protocol"} {
		m.store.Chats.Set(&telegram.Chat{
			ID: id, Title: title, Type: telegram.ChatTypeSupergroup, Order: id,
		})
	}
	m.chatList.MarkLoadedForTest()
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = sized.(Model)
	_ = m.chatList.View()

	// Not a tick, not a resize: the open itself.
	updated, _ := m.Update(chatlist.ChatSelectedMsg{ChatId: 1})
	m = updated.(Model)

	want := m.chatList.BufferIndex(1)
	if want == 0 {
		t.Fatal("precondition: chat 1 should be in the list")
	}
	if got := ansi.Strip(m.chatView.View()); !strings.Contains(got, "buf "+strconv.Itoa(want)) {
		t.Fatalf("the header carries no buffer number on the frame it opened:\n%s", got)
	}

	// And switching does not leave the previous chat's number behind.
	updated, _ = m.Update(chatlist.ChatSelectedMsg{ChatId: 2})
	m = updated.(Model)

	second := m.chatList.BufferIndex(2)
	if second == want {
		t.Skip("both chats are the same row; nothing to tell apart")
	}
	if got := ansi.Strip(m.chatView.View()); !strings.Contains(got, "buf "+strconv.Itoa(second)) {
		t.Fatalf("the header kept the previous chat's number:\n%s", got)
	}
}
