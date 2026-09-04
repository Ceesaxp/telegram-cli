package chatview

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// The open chat used to answer every update with its own RPC: one
// messages.readHistory per incoming message, one getMessages per edit,
// reaction or poll tally. In a quiet private chat that is invisible. In a
// busy group, or a channel where a post collects reactions, it is one
// request per update — which is the pattern Telegram answers with
// FLOOD_WAIT, and the failure was silent because every one of those results
// was discarded.
//
// Both are now accumulated and flushed on a short tick. The shape was
// already here for read receipts: the blurred path kept only the maximum
// message ID and sent one call on refocus, because a read receipt is
// cumulative. This applies the same reasoning to the focused path, and the
// batching Telegram's own list-taking RPCs were always willing to accept to
// the refetches.
//
// See issue #46.

// coalesceWindow is how long an update waits for company. Short enough that
// a read receipt is not perceptibly late and an edit redraws promptly; long
// enough that a burst — a group waking up, a post collecting reactions —
// collapses into one request.
const coalesceWindow = 300 * time.Millisecond

// readFlushMsg flushes the pending read receipt. It carries the chat it was
// scheduled for: a tick that outlives a chat switch must not send the new
// chat's receipt early, and must not resurrect the old chat's.
type readFlushMsg struct {
	chatID int64
}

// refetchFlushMsg flushes the pending refetches, and carries its chat for
// the same reason.
type refetchFlushMsg struct {
	chatID int64
}

// refetchedMsg carries a batch of refetched messages back to the model.
type refetchedMsg struct {
	chatID   int64
	messages []*telegram.Message
}

// noteRead records that everything up to id has been seen in the open,
// focused chat, and schedules the flush if one is not already pending.
//
// A read receipt is cumulative — readHistory takes a maximum ID and marks
// everything below it — so N arrivals in the window cost one call carrying
// the highest, not N calls.
func (m *Model) noteRead(id int64) tea.Cmd {
	if id > m.pendingReadID {
		m.pendingReadID = id
	}
	if m.readFlushPending {
		return nil
	}
	m.readFlushPending = true
	chatID := m.chatID
	return tea.Tick(coalesceWindow, func(time.Time) tea.Msg {
		return readFlushMsg{chatID: chatID}
	})
}

// flushRead sends the accumulated receipt, if there is one and the chat is
// still open.
func (m *Model) flushRead() tea.Cmd {
	m.readFlushPending = false
	if m.chatID == 0 || m.pendingReadID == 0 || m.blurred {
		// Blurred keeps the pending ID deliberately: FocusMsg is what
		// catches it up, and consuming it here would lose the receipt.
		return nil
	}
	chatID, msgID := m.chatID, m.pendingReadID
	m.pendingReadID = 0
	// Consumed before the client is checked, so the accumulator behaves
	// the same with and without one. A nil client skips the RPC, not the
	// state machine.
	tg := m.tg
	if tg == nil {
		return nil
	}
	return func() tea.Msg {
		// The error is deliberately dropped, as it always was here. A
		// FLOOD_WAIT on a background read receipt is a reason to have sent
		// fewer of them, which is what this file is; it is not something
		// to interrupt the reader with.
		_ = tg.ViewMessages(chatID, []int64{msgID})
		return nil
	}
}

// noteRefetch records that a message needs fetching again — it was edited,
// or its reactions or poll tally changed — and schedules the flush if one
// is not already pending.
func (m *Model) noteRefetch(id int64) tea.Cmd {
	if m.pendingRefetch == nil {
		m.pendingRefetch = map[int64]struct{}{}
	}
	m.pendingRefetch[id] = struct{}{}
	if m.refetchFlushPending {
		return nil
	}
	m.refetchFlushPending = true
	chatID := m.chatID
	return tea.Tick(coalesceWindow, func(time.Time) tea.Msg {
		return refetchFlushMsg{chatID: chatID}
	})
}

// flushRefetch fetches every message accumulated in the window in one
// request, dropping the ones that are no longer loaded.
func (m *Model) flushRefetch() tea.Cmd {
	m.refetchFlushPending = false
	ids := m.loadedPending()
	m.pendingRefetch = nil
	if len(ids) == 0 || m.tg == nil {
		return nil
	}
	chatID := m.chatID
	tg := m.tg
	return func() tea.Msg {
		fetched, err := tg.GetMessages(chatID, ids)
		if err != nil {
			return nil
		}
		return refetchedMsg{chatID: chatID, messages: fetched}
	}
}

// loadedPending is the pending set narrowed to messages the store still
// holds, in ascending ID order.
//
// A reaction can land on a message that scrolled out of the bounded cache,
// or on one deleted since the update. Fetching it would spend the request
// on something with nowhere to go. Sorting is not cosmetic: a map's
// iteration order is randomised, and a request whose argument list differs
// run to run cannot be asserted on.
func (m *Model) loadedPending() []int64 {
	if len(m.pendingRefetch) == 0 {
		return nil
	}
	loaded := map[int64]bool{}
	for _, msg := range m.store.Messages.Get(m.chatID) {
		loaded[msg.ID] = true
	}
	ids := make([]int64, 0, len(m.pendingRefetch))
	for id := range m.pendingRefetch {
		if loaded[id] {
			ids = append(ids, id)
		}
	}
	sortInt64s(ids)
	return ids
}

// sortInt64s is an insertion sort: the pending set is a handful of IDs from
// one 300ms window, which is the size where this beats reaching for a
// package.
func sortInt64s(ids []int64) {
	for i := 1; i < len(ids); i++ {
		v := ids[i]
		j := i - 1
		for j >= 0 && ids[j] > v {
			ids[j+1] = ids[j]
			j--
		}
		ids[j+1] = v
	}
}

// clearCoalescing drops everything pending. Called when the open chat
// changes: the accumulated IDs belong to the chat that was open, and a tick
// already in flight is ignored by its chat check.
func (m *Model) clearCoalescing() {
	m.pendingReadID = 0
	m.pendingRefetch = nil
	m.readFlushPending = false
	m.refetchFlushPending = false
}
