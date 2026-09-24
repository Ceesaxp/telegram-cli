package store

import (
	"log"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

func storedMessage(chatID, id int64) *telegram.Message {
	return &telegram.Message{ChatID: chatID, ID: id}
}

func TestActiveChatHistoryCanPagePastBackgroundCap(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	for id := int64(101); id <= 300; id++ {
		s.Append(chatID, storedMessage(chatID, id))
	}
	s.Activate(chatID)

	older := make([]*telegram.Message, 0, 50)
	for id := int64(51); id <= 100; id++ {
		older = append(older, storedMessage(chatID, id))
	}
	inserted := s.Prepend(chatID, older)

	if len(inserted) != 50 || s.Count(chatID) != 250 {
		t.Fatalf("inserted=%d count=%d, want 50 and 250", len(inserted), s.Count(chatID))
	}
	if oldest := s.OldestMessageId(chatID); oldest != 51 {
		t.Fatalf("oldest=%d, want 51", oldest)
	}
}

func TestLeavingActiveChatRestoresBound(t *testing.T) {
	s := NewMessageStore()
	s.maxSize = 3
	s.Activate(1)
	for id := int64(1); id <= 5; id++ {
		s.Append(1, storedMessage(1, id))
	}

	s.Activate(2)
	got := s.Get(1)
	if len(got) != 3 || got[0].ID != 3 || got[2].ID != 5 {
		t.Fatalf("trimmed previous chat = %v, want IDs 3..5", messageIDs(got))
	}
}

func TestPrependReportsOnlyNewSurvivingMessages(t *testing.T) {
	s := NewMessageStore()
	s.maxSize = 3
	s.Append(1, storedMessage(1, 3))
	s.Append(1, storedMessage(1, 4))
	s.Append(1, storedMessage(1, 5))

	inserted := s.Prepend(1, []*telegram.Message{
		storedMessage(1, 1),
		storedMessage(1, 2),
		storedMessage(1, 2),
		storedMessage(1, 3),
	})
	if len(inserted) != 0 {
		t.Fatalf("surviving inserts = %v, want none", messageIDs(inserted))
	}
	if got := messageIDs(s.Get(1)); len(got) != 3 || got[0] != 3 || got[2] != 5 {
		t.Fatalf("background cache = %v, want [3 4 5]", got)
	}
}

// Prepend's contract is that the page is entirely older than what is held.
// Handed the newest page instead — which is what a reopen of a cached chat
// fetches — it used to place the one message that arrived while the reader
// was away at the OLDEST end, above the whole history, where a reader
// sitting at the bottom never saw it. It refuses such a page now, so the
// mistake is a page that visibly does not arrive rather than a thread
// that silently reads in the wrong order.
func TestPrependRefusesAPageThatIsNotOlderThanTheCache(t *testing.T) {
	const chatID = int64(7)
	s := NewMessageStore()
	s.Activate(chatID)
	s.Prepend(chatID, page(chatID, 1, 2, 3, 4, 5)) // a first visit

	complaints := captureLog(t)

	// Reopening refetches the newest page; 6 arrived while away.
	inserted := s.Prepend(chatID, page(chatID, 1, 2, 3, 4, 5, 6))

	if got, want := ids(s.Get(chatID)), []int64{1, 2, 3, 4, 5}; !equalIDs(got, want) {
		t.Fatalf("history after a refused page = %v, want %v", got, want)
	}
	if len(inserted) != 0 {
		t.Fatalf("inserted = %v, want none: 6 is newer than the cache and Prepend cannot place it", ids(inserted))
	}
	// Refusing must not be another kind of silence: the log names the
	// message and the method that should have been called instead.
	if got := complaints.String(); !strings.Contains(got, "message 6") || !strings.Contains(got, "Merge") {
		t.Fatalf("refusal logged %q, want it to name message 6 and Merge", got)
	}
}

// Paging backwards is what Prepend is for, and a page that repeats what is
// already held — the same request issued twice at the end of the history —
// is the normal way that walk finishes, not a contract violation.
func TestPrependAcceptsAnOlderPageAndItsOverlap(t *testing.T) {
	const chatID = int64(7)
	s := NewMessageStore()
	s.Activate(chatID)
	s.Prepend(chatID, page(chatID, 51, 52, 53))

	inserted := s.Prepend(chatID, page(chatID, 48, 49, 50, 51))
	if got, want := ids(inserted), []int64{48, 49, 50}; !equalIDs(got, want) {
		t.Fatalf("inserted = %v, want %v", got, want)
	}
	if got := ids(s.Prepend(chatID, page(chatID, 48, 49, 50, 51))); len(got) != 0 {
		t.Fatalf("a page of pure repeats inserted %v, want nothing", got)
	}
	if got, want := ids(s.Get(chatID)), []int64{48, 49, 50, 51, 52, 53}; !equalIDs(got, want) {
		t.Fatalf("history after paging backwards = %v, want %v", got, want)
	}
}

// page is a chat's history oldest first, which is the order Prepend and
// Merge are both handed.
func page(chatID int64, msgIDs ...int64) []*telegram.Message {
	msgs := make([]*telegram.Message, 0, len(msgIDs))
	for _, id := range msgIDs {
		msgs = append(msgs, storedMessage(chatID, id))
	}
	return msgs
}

// captureLog redirects the standard logger for the duration of a test.
func captureLog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestReplaceMessageIDAlwaysRepairsBackgroundCap(t *testing.T) {
	s := NewMessageStore()
	s.maxSize = 3
	for id := int64(1); id <= 3; id++ {
		s.Append(1, storedMessage(1, id))
	}

	s.ReplaceMessageId(1, 0, storedMessage(1, 4))
	s.ReplaceMessageId(1, 0, storedMessage(1, 4)) // dispatcher echo
	got := s.Get(1)
	if len(got) != 3 || got[0].ID != 2 || got[2].ID != 4 {
		t.Fatalf("messages = %v, want [2 3 4]", messageIDs(got))
	}
}

// The update dispatcher and the send call race each other. When the
// dispatcher wins, the confirmed message is already in the thread by the
// time the send returns, and swapping the placeholder for it would leave
// the reader looking at the same message twice — forever, if the success
// message were the thing that got lost.
func TestReplaceMessageIdDropsThePlaceholderWhenTheDispatcherWonTheRace(t *testing.T) {
	s := NewMessageStore()
	s.Append(1, storedMessage(1, 8))
	s.Append(1, storedMessage(1, -1)) // local echo
	s.Append(1, storedMessage(1, 9))  // the real message, via the dispatcher

	s.ReplaceMessageId(1, -1, storedMessage(1, 9))

	if got := messageIDs(s.Get(1)); len(got) != 2 || got[0] != 8 || got[1] != 9 {
		t.Fatalf("messages = %v, want [8 9]", got)
	}
}

// The ordinary case: the send won, so the placeholder is swapped in place
// and keeps its position in the thread rather than jumping to the end.
func TestReplaceMessageIdSwapsThePlaceholderInPlace(t *testing.T) {
	s := NewMessageStore()
	s.Append(1, storedMessage(1, -1))
	s.Append(1, storedMessage(1, 8))

	s.ReplaceMessageId(1, -1, storedMessage(1, 9))

	if got := messageIDs(s.Get(1)); len(got) != 2 || got[0] != 9 || got[1] != 8 {
		t.Fatalf("messages = %v, want [9 8]", got)
	}
}

func TestMarkSendFailedFlagsOnlyTheNamedMessage(t *testing.T) {
	s := NewMessageStore()
	s.Append(1, storedMessage(1, -1))
	s.Append(1, storedMessage(1, -2))

	if !s.MarkSendFailed(1, -2) {
		t.Fatal("marking a message that is present reported it missing")
	}
	msgs := s.Get(1)
	if msgs[0].SendFailed || !msgs[1].SendFailed {
		t.Fatalf("failed flags = %v/%v, want false/true", msgs[0].SendFailed, msgs[1].SendFailed)
	}
	if s.MarkSendFailed(1, -3) {
		t.Fatal("marking an absent message reported success")
	}
}

// Paging backwards from a locally invented ID asks Telegram for history
// around a message it has never heard of. It can only be the oldest entry
// when the cache holds nothing else, which is exactly the case of sending
// the first thing into a freshly opened chat.
func TestOldestMessageIdSkipsLocalEchoes(t *testing.T) {
	s := NewMessageStore()
	s.Append(1, storedMessage(1, -1))
	if got := s.OldestMessageId(1); got != 0 {
		t.Fatalf("oldest over echoes alone = %d, want 0", got)
	}

	s.Append(1, storedMessage(1, 20))
	if got := s.OldestMessageId(1); got != 20 {
		t.Fatalf("oldest = %d, want 20", got)
	}
}

func messageIDs(messages []*telegram.Message) []int64 {
	ids := make([]int64, len(messages))
	for i, message := range messages {
		ids[i] = message.ID
	}
	return ids
}

// A refetch of the newest page after a sync gap can carry messages newer
// than anything cached, older ones that fell in a hole, and copies of what
// is already there. Merge has to put each where it belongs by ID, report
// only what was new, and leave a local echo — a negative ID the server has
// never seen — where it was: at the bottom, after everything confirmed.
func TestMergePlacesNewMessagesByIDAndKeepsEchoesLast(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	for _, id := range []int64{1, 2, 5, -1} {
		s.Append(chatID, storedMessage(chatID, id))
	}

	inserted := s.Merge(chatID, []*telegram.Message{
		storedMessage(chatID, 3),
		storedMessage(chatID, 5),
		storedMessage(chatID, 6),
		storedMessage(chatID, 7),
	})

	gotInserted := ids(inserted)
	if want := []int64{3, 6, 7}; !equalIDs(gotInserted, want) {
		t.Fatalf("inserted = %v, want %v", gotInserted, want)
	}
	got := ids(s.Get(chatID))
	if want := []int64{1, 2, 3, 5, 6, 7, -1}; !equalIDs(got, want) {
		t.Fatalf("order after merge = %v, want %v", got, want)
	}
}

// The server's copy is fresher than ours — an edit or a reaction that
// happened during the gap is on it — so a message already cached is
// replaced by the merge, not skipped.
func TestMergeReplacesTheCachedCopy(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	stale := storedMessage(chatID, 4)
	s.Append(chatID, stale)

	fresh := storedMessage(chatID, 4)
	if inserted := s.Merge(chatID, []*telegram.Message{fresh}); len(inserted) != 0 {
		t.Fatalf("a replaced message must not be reported as inserted, got %d", len(inserted))
	}
	if got := s.Get(chatID); len(got) != 1 || got[0] != fresh {
		t.Fatalf("store holds %v, want the fresh copy alone", got)
	}
}

func ids(msgs []*telegram.Message) []int64 {
	out := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

func equalIDs(a, b []int64) bool {
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
