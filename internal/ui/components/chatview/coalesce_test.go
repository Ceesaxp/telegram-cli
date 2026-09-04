package chatview

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// These tests assert on the accumulator rather than on the wire. The chat
// view holds a concrete *telegram.Client, so there is no seam to count
// calls at without making one — but the accumulator IS the argument list:
// pendingReadID is the ID readHistory is called with, and loadedPending()
// is the ID list getMessages is called with. What one call carries and how
// many calls are scheduled are both visible here.

func incoming(id int64) telegram.NewMessageMsg {
	return telegram.NewMessageMsg{Message: textMessage(id, 100, "hi")}
}

func edited(id int64) telegram.MessageEditedMsg {
	return telegram.MessageEditedMsg{ChatId: testChatID, MessageId: id}
}

// A burst of arrivals in the open, focused chat schedules ONE flush, and it
// carries the highest ID — a read receipt is cumulative, so the lower ones
// are covered by it.
func TestABurstOfArrivalsIsOneReadReceipt(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)

	var scheduled int
	for id := int64(1); id <= 5; id++ {
		var cmd tea.Cmd
		m, cmd = m.Update(incoming(id))
		if cmd != nil {
			scheduled++
		}
	}

	if scheduled != 1 {
		t.Fatalf("%d flushes scheduled for a burst of five, want 1", scheduled)
	}
	if m.pendingReadID != 5 {
		t.Fatalf("pendingReadID = %d, want the highest arrival", m.pendingReadID)
	}
}

// After the flush the next arrival schedules again: the window closes, it
// does not latch.
func TestTheReadWindowReopensAfterFlushing(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m, _ = m.Update(incoming(1))

	m, _ = m.Update(readFlushMsg{chatID: testChatID})
	if m.readFlushPending {
		t.Fatal("a flush left itself pending")
	}
	if m.pendingReadID != 0 {
		t.Fatalf("pendingReadID = %d after flushing, want it consumed", m.pendingReadID)
	}

	var cmd tea.Cmd
	m, cmd = m.Update(incoming(2))
	if cmd == nil {
		t.Fatal("the arrival after a flush scheduled nothing")
	}
}

// A flush tick that outlives a chat switch belongs to the chat that is
// gone. Acting on it would send the new chat's receipt early — or worse,
// the old chat's ID against the new chat.
func TestAFlushForAnotherChatIsIgnored(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m, _ = m.Update(incoming(7))

	m, cmd := m.Update(readFlushMsg{chatID: testChatID + 1})
	if cmd != nil {
		t.Fatal("a flush for another chat produced a command")
	}
	if m.pendingReadID != 7 {
		t.Fatalf("pendingReadID = %d, want this chat's pending read untouched", m.pendingReadID)
	}
}

// Reactions and poll tallies arrive as edits, so a post collecting
// reactions used to be a round trip per reaction. A burst is one request
// now, with the IDs deduplicated.
func TestABurstOfEditsIsOneFetch(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	for id := int64(1); id <= 3; id++ {
		m.store.Messages.Append(testChatID, textMessage(id, 100, "loaded"))
	}

	var scheduled int
	for _, id := range []int64{3, 1, 2, 1, 3} { // repeats: one reader reacting twice
		var cmd tea.Cmd
		m, cmd = m.Update(edited(id))
		if cmd != nil {
			scheduled++
		}
	}

	if scheduled != 1 {
		t.Fatalf("%d fetches scheduled for five updates, want 1", scheduled)
	}
	got := m.loadedPending()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("fetch list = %v, want the three distinct IDs in order", got)
	}
}

// A reaction can land on a message that has scrolled out of the bounded
// store, or on one deleted since. Fetching it spends a request on
// something with nowhere to go.
func TestUnloadedMessagesAreNotFetched(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m.store.Messages.Append(testChatID, textMessage(2, 100, "loaded"))

	m, _ = m.Update(edited(2))
	m, _ = m.Update(edited(999)) // never loaded

	got := m.loadedPending()
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("fetch list = %v, want only the loaded message", got)
	}
}

// The refetch flush is chat-checked for the read flush's reason.
func TestARefetchFlushForAnotherChatIsIgnored(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m.store.Messages.Append(testChatID, textMessage(1, 100, "loaded"))
	m, _ = m.Update(edited(1))

	m, cmd := m.Update(refetchFlushMsg{chatID: testChatID + 1})
	if cmd != nil {
		t.Fatal("a refetch flush for another chat produced a command")
	}
	if len(m.pendingRefetch) != 1 {
		t.Fatalf("pendingRefetch = %v, want this chat's pending fetch untouched", m.pendingRefetch)
	}
}

// The batch comes back and every message in it is applied — the whole
// point of asking for several at once.
func TestARefetchedBatchIsAllApplied(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	for id := int64(1); id <= 2; id++ {
		m.store.Messages.Append(testChatID, textMessage(id, 100, "before"))
	}

	m, _ = m.Update(refetchedMsg{chatID: testChatID, messages: []*telegram.Message{
		textMessage(1, 100, "after"),
		nil, // a message the server did not return
		textMessage(2, 100, "after"),
	}})

	for _, msg := range m.store.Messages.Get(testChatID) {
		text, ok := msg.Content.(*telegram.MessageText)
		if !ok || text.Text.Text != "after" {
			t.Fatalf("message %d was not updated from the batch: %+v", msg.ID, msg.Content)
		}
	}
}

// Opening another chat drops what was pending: the IDs belong to the chat
// that was open, and its receipt must not be sent against the new one.
func TestOpeningAnotherChatDropsWhatWasPending(t *testing.T) {
	m := newTestModel()
	m.store.Messages.Activate(testChatID)
	m.store.Messages.Append(testChatID, textMessage(1, 100, "loaded"))
	m, _ = m.Update(incoming(1))
	m, _ = m.Update(edited(1))

	m.OpenChatAt(testChatID+1, "elsewhere", 0)

	if m.pendingReadID != 0 || len(m.pendingRefetch) != 0 {
		t.Fatalf("the switch left state behind: read=%d refetch=%v", m.pendingReadID, m.pendingRefetch)
	}
	if m.readFlushPending || m.refetchFlushPending {
		t.Fatal("the switch left a flush marked pending, so the next update would schedule nothing")
	}
}
