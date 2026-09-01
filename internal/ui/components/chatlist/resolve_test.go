package chatlist

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// A message from a chat outside the loaded dialog page produced a row with
// no name, and opening it was the only thing that ever fetched the chat —
// so the name appeared when, and only when, the reader visited it.
//
// The row has to exist regardless: something was said. What it must not do
// is sit there blank, which reads as a rendering fault rather than as a
// name this client has not been told yet.
func TestARowForAnUnknownChatSaysItIsLoading(t *testing.T) {
	m := newTestModel()
	m.store.Chats.UpdateLastMessage(7, &telegram.Message{
		ID: 1, ChatID: 7, Date: 1700000000,
		Content: &telegram.MessageText{Text: &telegram.FormattedText{Text: "hello"}},
	})
	m.refreshList()

	titles := listTitles(m)
	if len(titles) != 1 {
		t.Fatalf("the message produced %d rows, want 1: %q", len(titles), titles)
	}
	if titles[0] != unresolvedTitle {
		t.Errorf("the row reads %q, want the placeholder %q", titles[0], unresolvedTitle)
	}
	if strings.TrimSpace(titles[0]) == "" {
		t.Error("the row is blank")
	}
}

// And once the fetch comes back, it is the real name — no placeholder left
// behind, and nothing needed the reader to open anything.
func TestTheFetchedNameReplacesThePlaceholder(t *testing.T) {
	m := newTestModel()
	m.store.Chats.UpdateLastMessage(7, &telegram.Message{ID: 1, ChatID: 7, Date: 1700000000})

	m, _ = m.Update(telegram.ChatUpdateMsg{
		Chat:     &telegram.Chat{ID: 7, Type: telegram.ChatTypePrivate, Title: "Ana"},
		FromPeer: true,
	})
	m.refreshList()

	if got := listTitles(m); len(got) != 1 || got[0] != "Ana" {
		t.Errorf("titles = %q, want [Ana]", got)
	}
}

// A real chat with no name is a deleted account, and telling the reader it
// is loading forever would be a lie. Only an entry the store invented gets
// the placeholder.
func TestARealChatWithNoNameIsNotCalledLoading(t *testing.T) {
	entry := &store.ChatEntry{Chat: &telegram.Chat{ID: 7, Title: ""}}
	if got := chatTitle(entry); got != "" {
		t.Errorf("a resolved nameless chat reads %q, want empty", got)
	}

	entry.Unresolved = true
	if got := chatTitle(entry); got != unresolvedTitle {
		t.Errorf("an unresolved chat reads %q, want %q", got, unresolvedTitle)
	}
}

// The fetch is issued once. Two messages from the same unknown chat arrive
// back to back — a burst is the normal case, not the edge one — and each
// refresh must not fire another request.
func TestTheChatIsOnlyFetchedOnce(t *testing.T) {
	m := newTestModel()
	// A client is needed for the command to exist at all; it is never run,
	// so no request is made. What is under test is how many times one
	// would be.
	m.tg = &telegram.Client{}

	deliver := func() tea.Cmd {
		m.store.Chats.UpdateLastMessage(7, &telegram.Message{ID: 1, ChatID: 7})
		next, cmd := m.Update(telegram.ChatLastMessageMsg{
			ChatId: 7, LastMessage: &telegram.Message{ID: 1, ChatID: 7},
		})
		m = next
		return cmd
	}

	if cmd := deliver(); cmd == nil {
		t.Fatal("the first message issued no fetch")
	}
	if cmd := deliver(); cmd != nil {
		t.Error("the second message issued a second fetch")
	}

	// A failure releases the marker, or a chat that was briefly
	// unreachable would stay nameless for the rest of the session.
	m, _ = m.Update(chatResolveFailedMsg{chatID: 7})
	if cmd := deliver(); cmd == nil {
		t.Error("after a failed fetch, a later message did not retry")
	}
}

// A chat that is already known is not fetched: the dialog told us who it is.
func TestAKnownChatIsNotFetched(t *testing.T) {
	m := newTestModel()
	m.tg = &telegram.Client{}
	m.store.Chats.Set(&telegram.Chat{ID: 7, Title: "Ana", Type: telegram.ChatTypePrivate})

	_, cmd := m.Update(telegram.ChatLastMessageMsg{
		ChatId: 7, LastMessage: &telegram.Message{ID: 1, ChatID: 7},
	})
	if cmd != nil {
		t.Error("a chat we already know was fetched again")
	}
}
