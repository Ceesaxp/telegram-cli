package store

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// A reply row quotes the message it answers, and finding that message by
// walking a copy of the whole history, once per reply, made redrawing a long
// chat quadratic. GetByID answers from an index instead.
func TestGetByIDFindsTheStoredMessage(t *testing.T) {
	s := NewMessageStore()
	want := storedMessage(1, 7)
	s.Append(1, storedMessage(1, 6))
	s.Append(1, want)

	if got, ok := s.GetByID(1, 7); !ok || got != want {
		t.Fatalf("GetByID(1, 7) = %v, %v; want the stored message", got, ok)
	}
	if got, ok := s.GetByID(1, 8); ok {
		t.Fatalf("GetByID(1, 8) found %v in a chat that never had it", got)
	}
	if got, ok := s.GetByID(2, 7); ok {
		t.Fatalf("GetByID(2, 7) found %v, which is chat 1's", got)
	}
}

// The store is shared between the update dispatcher and the UI, so a lookup
// runs while other goroutines write. This is here for -race to watch.
func TestGetByIDIsSafeAlongsideWriters(t *testing.T) {
	const chatID = int64(1)
	s := NewMessageStore()
	s.Activate(chatID)

	var wg sync.WaitGroup
	wg.Go(func() {
		for id := int64(1); id <= 500; id++ {
			s.Append(chatID, storedMessage(chatID, id))
			if id%7 == 0 {
				s.Delete(chatID, []int64{id - 3})
			}
		}
	})
	wg.Go(func() {
		for id := int64(-1); id >= -100; id-- {
			s.Append(chatID, storedMessage(chatID, id))
			s.ReplaceMessageId(chatID, id, storedMessage(chatID, 1000-id))
		}
	})
	wg.Go(func() {
		for range 2000 {
			if m, ok := s.GetByID(chatID, 250); ok && m.ID != 250 {
				t.Errorf("GetByID(250) returned message %d", m.ID)
				return
			}
		}
	})
	wg.Wait()
}

// The index is a second copy of what the history holds, and a second copy
// is only worth having if it can never disagree with the first. So every
// mutation the store has is thrown at it in a random order — pages that
// overlap, placeholders swapped for real IDs, deletes of things that are
// there and things that are not, and chat switches, so the background cap
// trims as it goes — and after every single step each chat's index must
// agree with a plain walk of its history.
func TestGetByIDAgreesWithTheHistoryThroughAnyMutation(t *testing.T) {
	for seed := uint64(1); seed <= 25; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			h := newIndexHarness(seed)
			for step := 0; step < 1500; step++ {
				op := h.step()
				if err := h.check(); err != nil {
					t.Fatalf("step %d, after %s: %v", step, op, err)
				}
			}
		})
	}
}

// Chats the harness touches, and one it never does, so a lookup in a chat
// with no history at all is checked too.
var (
	harnessChats = []int64{1, 2, 3}
	checkedChats = []int64{1, 2, 3, 4}
)

// IDs are drawn from a narrow range so that pages overlap, deletes hit and
// swaps collide; lookups are checked across a range a little wider than
// that, so an ID that has gone is looked for as well.
const (
	harnessMaxID        = 60
	harnessPlaceholders = 30 // local echoes are -1 .. -30
	harnessMinCheck     = -harnessPlaceholders - 5
	harnessMaxCheck     = harnessMaxID + 5
)

type indexHarness struct {
	s   *MessageStore
	rng *rand.Rand
}

func newIndexHarness(seed uint64) *indexHarness {
	s := NewMessageStore()
	// A small cap, so a background chat trims every few writes rather than
	// once in a long while.
	s.maxSize = 10
	return &indexHarness{s: s, rng: rand.New(rand.NewPCG(seed, 0x1d))}
}

func (h *indexHarness) chat() int64 { return harnessChats[h.rng.IntN(len(harnessChats))] }

func (h *indexHarness) id() int64 { return 1 + h.rng.Int64N(harnessMaxID) }

// present is an ID the chat holds right now, or a random one when it holds
// nothing, so operations aimed at existing messages mostly land.
func (h *indexHarness) present(chatID int64) int64 {
	msgs := h.s.Get(chatID)
	if len(msgs) == 0 || h.rng.IntN(5) == 0 {
		return h.id()
	}
	return msgs[h.rng.IntN(len(msgs))].ID
}

// absent is an ID the chat does not hold, or else fallback. It asks the
// history rather than the index, which is the thing under test.
func (h *indexHarness) absent(chatID, fallback int64) int64 {
	held := make(map[int64]bool)
	for _, m := range h.s.Get(chatID) {
		held[m.ID] = true
	}
	for _, i := range h.rng.Perm(harnessMaxCheck) {
		if id := int64(i + 1); !held[id] {
			return id
		}
	}
	return fallback
}

func (h *indexHarness) newPlaceholder() int64 { return -1 - h.rng.Int64N(harnessPlaceholders) }

// placeholder is a local echo the chat holds right now, when it holds one
// and the dice agree, and otherwise one it may or may not.
func (h *indexHarness) placeholder(chatID int64) int64 {
	for _, m := range h.s.Get(chatID) {
		if m.ID < 0 && h.rng.IntN(2) == 0 {
			return m.ID
		}
	}
	return h.newPlaceholder()
}

func (h *indexHarness) page(chatID int64, lo, hi int64) []*telegram.Message {
	n := 1 + h.rng.IntN(8)
	page := make([]*telegram.Message, 0, n)
	for range n {
		page = append(page, storedMessage(chatID, lo+h.rng.Int64N(hi-lo+1)))
	}
	return page
}

func (h *indexHarness) step() string {
	s, chatID := h.s, h.chat()
	switch r := h.rng.IntN(100); {
	case r < 25:
		id := h.id()
		if h.rng.IntN(3) == 0 {
			id = h.newPlaceholder()
		}
		s.Append(chatID, storedMessage(chatID, id))
		return fmt.Sprintf("Append(%d, %d)", chatID, id)
	case r < 35:
		oldest := max(s.OldestMessageId(chatID), 1)
		page := h.page(chatID, max(oldest-12, 1), oldest+3)
		s.Prepend(chatID, page)
		return fmt.Sprintf("Prepend(%d, %v)", chatID, ids(page))
	case r < 45:
		page := h.page(chatID, 1, harnessMaxID)
		if h.rng.IntN(4) == 0 {
			page = append(page, nil)
		}
		s.Merge(chatID, page)
		return fmt.Sprintf("Merge(%d, %d messages)", chatID, len(page))
	case r < 55:
		id := h.present(chatID)
		newID := id
		if h.rng.IntN(4) == 0 {
			// Every caller hands over the same ID it names, but the index
			// has to follow the message it was given, not the one named.
			newID = h.absent(chatID, id)
		}
		s.UpdateMessage(chatID, id, storedMessage(chatID, newID))
		return fmt.Sprintf("UpdateMessage(%d, %d -> %d)", chatID, id, newID)
	case r < 65:
		gone := []int64{h.present(chatID), h.present(chatID), h.id()}
		s.Delete(chatID, gone)
		return fmt.Sprintf("Delete(%d, %v)", chatID, gone)
	case r < 70:
		gone := []int64{h.present(chatID), h.id(), h.placeholder(chatID)}
		s.DeleteFromAll(gone)
		return fmt.Sprintf("DeleteFromAll(%v)", gone)
	case r < 82:
		oldID := h.placeholder(chatID)
		newID := h.id()
		switch h.rng.IntN(4) {
		case 0:
			newID = h.present(chatID) // the dispatcher got there first
		case 1:
			newID = oldID // degenerate, but the index must follow it too
		}
		s.ReplaceMessageId(chatID, oldID, storedMessage(chatID, newID))
		return fmt.Sprintf("ReplaceMessageId(%d, %d -> %d)", chatID, oldID, newID)
	case r < 87:
		id := h.placeholder(chatID)
		s.MarkSendFailed(chatID, id)
		return fmt.Sprintf("MarkSendFailed(%d, %d)", chatID, id)
	case r < 89:
		s.Clear(chatID)
		return fmt.Sprintf("Clear(%d)", chatID)
	default:
		s.Activate(chatID)
		return fmt.Sprintf("Activate(%d)", chatID)
	}
}

// check holds every chat's index up against a linear walk of its history.
func (h *indexHarness) check() error {
	for _, chatID := range checkedChats {
		msgs := h.s.Get(chatID)
		first := make(map[int64]*telegram.Message, len(msgs))
		for _, m := range msgs {
			if _, dup := first[m.ID]; dup {
				return fmt.Errorf("chat %d holds ID %d twice: %v", chatID, m.ID, ids(msgs))
			}
			first[m.ID] = m
		}

		for id := int64(harnessMinCheck); id <= harnessMaxCheck; id++ {
			got, ok := h.s.GetByID(chatID, id)
			want, wantOK := first[id]
			if ok != wantOK || got != want {
				return fmt.Errorf("chat %d: GetByID(%d) = %p, %v; the history has %p, %v (history %v)",
					chatID, id, got, ok, want, wantOK, ids(msgs))
			}
		}

		// Entries outside the checked range would hide from the loop
		// above; a stale one of those would still be holding its message.
		h.s.mu.RLock()
		indexed := len(h.s.byID[chatID])
		h.s.mu.RUnlock()
		if indexed != len(msgs) {
			return fmt.Errorf("chat %d: index holds %d entries for %d messages", chatID, indexed, len(msgs))
		}

		h.s.mu.RLock()
		retained := retainedPastLen(h.s, chatID)
		h.s.mu.RUnlock()
		if retained != 0 {
			return fmt.Errorf("chat %d: %d dropped messages still reachable past len", chatID, retained)
		}
	}
	return nil
}
