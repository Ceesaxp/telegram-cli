package chatview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// serverPage is a history response as Telegram gives one: newest first.
func serverPage(newest, oldest int64) []*telegram.Message {
	var page []*telegram.Message
	for id := newest; id >= oldest; id-- {
		page = append(page, textMessage(id, 100, fmt.Sprintf("msg %d", id)))
	}
	return page
}

// The reader opens a chat, leaves for another, and comes back. The message
// store outlives the visit — it caches per chat and is only emptied when
// the process exits — so the newest page fetched on the reopen is almost
// entirely already held, and the message that arrived while the reader was
// away is the only thing new about it. That message is NEWER than
// everything cached, and it belongs at the bottom of the thread, where the
// reader is sitting.
func TestReopeningAChatShowsWhatArrivedWhileAway(t *testing.T) {
	m := newTestModel()

	// A first visit: the newest page, and the reader reads it.
	m.OpenChat(testChatID, "test")
	m, _ = m.Update(historyLoadedMsg{
		gen: m.gen, chatID: testChatID, fromID: 0, messages: serverPage(40, 1),
	})

	// Away in another chat while message 41 arrives. The chat list sees
	// it; this thread, not being open, never does.
	m.OpenChat(testChatID+1, "elsewhere")

	// Back again. The page the reopen fetches is the same forty messages
	// plus the one that arrived.
	m.OpenChat(testChatID, "test")
	next, _ := m.Update(historyLoadedMsg{
		gen: m.gen, chatID: testChatID, fromID: 0, messages: serverPage(41, 1),
	})

	got := storedIDs(next)
	if want := int64(41); len(got) == 0 || got[len(got)-1] != want {
		t.Fatalf("thread after reopening = %v, want it to end at %d", got, want)
	}
	if body := next.View(); !strings.Contains(body, "msg 41") {
		t.Fatalf("the message that arrived while away is not on screen:\n%s", body)
	}
}

// An overlap with the cache means the end of the history only while paging
// backwards, where the page was asked for from the oldest message held. On
// the newest page it means the reader has seen everything since — a chat
// reopened without a single new message would otherwise refuse to page
// backwards at all, its history declared exhausted at its own bottom.
func TestReopeningWithNothingNewLeavesTheHistoryPageable(t *testing.T) {
	m := newTestModel()
	m.OpenChat(testChatID, "test")
	m, _ = m.Update(historyLoadedMsg{
		gen: m.gen, chatID: testChatID, fromID: 0, messages: serverPage(40, 1),
	})

	m.OpenChat(testChatID, "test")
	next, _ := m.Update(historyLoadedMsg{
		gen: m.gen, chatID: testChatID, fromID: 0, messages: serverPage(40, 1),
	})

	if next.historyEnd {
		t.Fatal("a reopen that brought nothing new marked the history exhausted")
	}
	if next.loading {
		t.Fatal("the reopened page never finished loading")
	}

	// Paging backwards, on the other hand, still stops: a page from the
	// oldest message held that brings nothing is the end of the history.
	next.loading = true
	next, _ = next.Update(historyLoadedMsg{
		gen: next.gen, chatID: testChatID, fromID: 1, messages: serverPage(40, 1),
	})
	if !next.historyEnd {
		t.Fatal("a backwards page that brought nothing left the history unexhausted")
	}
}
