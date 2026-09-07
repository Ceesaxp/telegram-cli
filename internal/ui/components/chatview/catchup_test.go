package chatview

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// catchUpPage is what the server hands back for the newest page: newest
// first, like every history response.
func catchUpPage(ids ...int64) []*telegram.Message {
	page := make([]*telegram.Message, 0, len(ids))
	for _, id := range ids {
		page = append(page, textMessage(id, 200, "caught up"))
	}
	return page
}

func storedIDs(m Model) []int64 {
	var out []int64
	for _, msg := range m.store.Messages.Get(testChatID) {
		out = append(out, msg.ID)
	}
	return out
}

// A catch-up page is the newest page fetched again after a sync gap. What
// it carries that the thread lacks goes in at the bottom, in order; what
// overlaps is not a reason to declare the history exhausted, the way the
// same overlap would be when paging backwards.
func TestCatchUpAppendsWhatTheGapSwallowed(t *testing.T) {
	m := newTestModel()
	for id := int64(1); id <= 3; id++ {
		m.store.Messages.Append(testChatID, textMessage(id, 100, "before"))
	}

	next, _ := m.Update(historyLoadedMsg{
		gen: m.gen, chatID: testChatID, catchUp: true, messages: catchUpPage(5, 4, 3),
	})

	if got, want := storedIDs(next), []int64{1, 2, 3, 4, 5}; !equalInt64s(got, want) {
		t.Fatalf("store after catch-up = %v, want %v", got, want)
	}
	if next.historyEnd {
		t.Fatal("a catch-up page must not mark the history exhausted")
	}
}

// A page that brought nothing new changes nothing and asks for nothing.
func TestCatchUpWithNothingNewIsQuiet(t *testing.T) {
	m := newTestModel()
	for id := int64(1); id <= 3; id++ {
		m.store.Messages.Append(testChatID, textMessage(id, 100, "before"))
	}

	next, cmd := m.Update(historyLoadedMsg{
		gen: m.gen, chatID: testChatID, catchUp: true, messages: catchUpPage(3, 2, 1),
	})

	if cmd != nil {
		t.Fatal("a catch-up that added nothing scheduled follow-up work")
	}
	if got, want := storedIDs(next), []int64{1, 2, 3}; !equalInt64s(got, want) {
		t.Fatalf("store after empty catch-up = %v, want %v", got, want)
	}
}

// There is nothing to catch up on without a chat, and nothing to catch up
// WITH while the first page is still on its way — that page is the newest
// one already.
func TestCatchUpCmdOnlyWhenAChatIsOpenAndLoaded(t *testing.T) {
	m := newTestModel()
	m.tg = &telegram.Client{}

	if m.CatchUpCmd() == nil {
		t.Fatal("open, loaded chat: want a catch-up command")
	}
	m.loading = true
	if m.CatchUpCmd() != nil {
		t.Fatal("first page in flight: want no catch-up command")
	}
	m.loading = false
	m.chatID = 0
	if m.CatchUpCmd() != nil {
		t.Fatal("no chat open: want no catch-up command")
	}
}

func equalInt64s(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
