package store

import (
	"runtime"
	"testing"
	"weak"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// The open chat is never trimmed, so after paging back it can hold
// thousands of messages — and a slice shortened in place keeps every
// pointer past its new length alive in the backing array. Nothing can see
// those messages any more, and nothing can collect them either.

// retainedPastLen counts the messages a chat's backing array still points
// at beyond the slice's length: the ones the store has let go of but the
// collector cannot.
func retainedPastLen(s *MessageStore, chatID int64) int {
	msgs := s.messages[chatID]
	n := 0
	for _, m := range msgs[len(msgs):cap(msgs)] {
		if m != nil {
			n++
		}
	}
	return n
}

// Deleting a thousand messages from the open chat left none visible and
// all thousand reachable.
func TestDeletingAThousandMessagesLetsThemAllGo(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	s.Activate(chatID)
	ids := make([]int64, 0, 1000)
	for id := int64(1); id <= 1000; id++ {
		s.Append(chatID, storedMessage(chatID, id))
		ids = append(ids, id)
	}

	s.Delete(chatID, ids)

	if got := s.Count(chatID); got != 0 {
		t.Fatalf("count after deleting everything = %d, want 0", got)
	}
	if n := retainedPastLen(s, chatID); n != 0 {
		t.Fatalf("%d deleted messages are still reachable from the backing array", n)
	}
}

// A delete that leaves most of the history in place keeps the array, and
// clears only the slots the filter moved out of.
func TestDeleteClearsTheSlotsItVacates(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	s.Activate(chatID)
	for id := int64(1); id <= 8; id++ {
		s.Append(chatID, storedMessage(chatID, id))
	}

	s.Delete(chatID, []int64{1})

	if got := ids(s.Get(chatID)); !equalIDs(got, []int64{2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("messages = %v, want [2 .. 8]", got)
	}
	if n := retainedPastLen(s, chatID); n != 0 {
		t.Fatalf("%d deleted messages are still reachable from the backing array", n)
	}
}

// A delete that leaves the array mostly empty gives the array back: the
// room a long history needed is not room a short one should keep holding.
func TestDeleteReleasesAnArrayItNoLongerNeeds(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	s.Activate(chatID)
	var gone []int64
	for id := int64(1); id <= 1000; id++ {
		s.Append(chatID, storedMessage(chatID, id))
		if id > 100 {
			gone = append(gone, id)
		}
	}

	s.Delete(chatID, gone)

	msgs := s.messages[chatID]
	if len(msgs) != 100 {
		t.Fatalf("len = %d, want 100", len(msgs))
	}
	if cap(msgs) != len(msgs) {
		t.Fatalf("cap = %d for %d messages: the thousand-slot array was kept", cap(msgs), len(msgs))
	}
}

// Non-channel deletes carry no chat, so they filter every chat, and every
// one of them has to let go the same way.
func TestDeleteFromAllLetsGoInEveryChat(t *testing.T) {
	s := NewMessageStore()
	s.Activate(1)
	for id := int64(1); id <= 8; id++ {
		s.Append(1, storedMessage(1, id))
		s.Append(2, storedMessage(2, id))
	}

	s.DeleteFromAll([]int64{2, 5})

	for _, chatID := range []int64{1, 2} {
		if got := ids(s.Get(chatID)); !equalIDs(got, []int64{1, 3, 4, 6, 7, 8}) {
			t.Fatalf("chat %d = %v, want [1 3 4 6 7 8]", chatID, got)
		}
		if n := retainedPastLen(s, chatID); n != 0 {
			t.Fatalf("chat %d: %d deleted messages are still reachable", chatID, n)
		}
	}
}

// collected reports whether the collector could reclaim every one of the
// messages: whether anything — a slot past a slice's length, the dead head
// of a resliced array — is still holding one of them.
func collected(msgs []weak.Pointer[telegram.Message]) bool {
	runtime.GC()
	runtime.GC()
	for _, w := range msgs {
		if w.Value() != nil {
			return false
		}
	}
	return true
}

// olderPage builds a page of messages the caller keeps no hold on, and
// hands back weak pointers to watch them by.
func olderPage(chatID int64, ids ...int64) ([]*telegram.Message, []weak.Pointer[telegram.Message]) {
	page := make([]*telegram.Message, len(ids))
	watch := make([]weak.Pointer[telegram.Message], len(ids))
	for i, id := range ids {
		page[i] = storedMessage(chatID, id)
		watch[i] = weak.Make(page[i])
	}
	return page, watch
}

// A background chat's page that the cap drops straight away was cut off
// the FRONT of the combined slice by reslicing it — so the dropped messages
// sat in the array ahead of the slice's start, where no length check sees
// them and nothing ever frees them.
func TestPrependLetsGoOfWhatTheCapDrops(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	s.maxSize = 3
	for id := int64(3); id <= 5; id++ {
		s.Append(chatID, storedMessage(chatID, id))
	}

	watch := func() []weak.Pointer[telegram.Message] {
		page, watch := olderPage(chatID, 1, 2)
		s.Prepend(chatID, page)
		return watch
	}()

	if got := ids(s.Get(chatID)); !equalIDs(got, []int64{3, 4, 5}) {
		t.Fatalf("messages = %v, want [3 4 5]", got)
	}
	if !collected(watch) {
		t.Fatal("the messages the cap dropped are still reachable from the stored array")
	}
	// Past this line nothing reads the store, so without this the collector
	// is free to take all of it — and the check above passes for the wrong
	// reason.
	runtime.KeepAlive(s)
}

// appendWatched appends messages the caller keeps no hold on, and hands
// back weak pointers to watch them by.
func appendWatched(s *MessageStore, chatID int64, ids ...int64) []weak.Pointer[telegram.Message] {
	msgs, watch := olderPage(chatID, ids...)
	for _, m := range msgs {
		s.Append(chatID, m)
	}
	return watch
}

// The background cap drops the oldest messages as new ones arrive. It
// copies the newest into a slice of exactly the cap, so what it drops has
// nowhere left to be held.
func TestTheBackgroundCapLetsGoOfTheOldest(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	s.maxSize = 3
	oldest := appendWatched(s, chatID, 1, 2, 3)
	appendWatched(s, chatID, 4, 5, 6)

	if got := ids(s.Get(chatID)); !equalIDs(got, []int64{4, 5, 6}) {
		t.Fatalf("messages = %v, want [4 5 6]", got)
	}
	if msgs := s.messages[chatID]; cap(msgs) != len(msgs) {
		t.Fatalf("cap = %d for %d messages", cap(msgs), len(msgs))
	}
	if !collected(oldest) {
		t.Fatal("messages the cap dropped are still reachable")
	}
	runtime.KeepAlive(s)
}

// Leaving the open chat trims it to the background cap, which is the one
// moment a paged-back history gives up the bulk of what it loaded.
func TestLeavingTheOpenChatLetsGoOfWhatItTrims(t *testing.T) {
	s := NewMessageStore()
	s.maxSize = 3
	s.Activate(1)
	paged := appendWatched(s, 1, 1, 2, 3, 4, 5, 6, 7)
	appendWatched(s, 1, 8, 9, 10)

	s.Activate(2)

	if got := ids(s.Get(1)); !equalIDs(got, []int64{8, 9, 10}) {
		t.Fatalf("messages = %v, want [8 9 10]", got)
	}
	if msgs := s.messages[1]; cap(msgs) != len(msgs) {
		t.Fatalf("cap = %d for %d messages", cap(msgs), len(msgs))
	}
	if !collected(paged) {
		t.Fatal("the history trimmed on leaving the chat is still reachable")
	}
	runtime.KeepAlive(s)
}

// A placeholder dropped because the dispatcher delivered the real message
// first is gone from the thread, and has to be gone from memory too.
func TestADroppedPlaceholderIsLetGo(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	s.Activate(chatID)
	s.Append(chatID, storedMessage(chatID, 8))
	placeholder := appendWatched(s, chatID, -1)
	s.Append(chatID, storedMessage(chatID, 9)) // the dispatcher won the race

	s.ReplaceMessageId(chatID, -1, storedMessage(chatID, 9))

	if got := ids(s.Get(chatID)); !equalIDs(got, []int64{8, 9}) {
		t.Fatalf("messages = %v, want [8 9]", got)
	}
	if n := retainedPastLen(s, chatID); n != 0 {
		t.Fatalf("%d dropped messages are still reachable from the backing array", n)
	}
	if !collected(placeholder) {
		t.Fatal("the dropped placeholder is still reachable")
	}
	runtime.KeepAlive(s)
}
