package chatview

import (
	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// Message-wise cursor motion.
//
// j/k and the arrows move the CURSOR, one message per press (decision I-4).
// They used to scroll lines, which left no way to move the cursor at all —
// it was a side effect of scrolling, anchored back into the visible window
// by syncCursor, so "reply to the message above this one" meant scrolling
// until the right one happened to be at the edge. Nine action keys target
// the cursored message; the primary motion has to be the one that moves it.
//
// } and { were this motion for a release, chosen because a message is a
// paragraph and that is vi's paragraph motion. They are retired rather than
// kept: with j/k message-wise they would be a second spelling of one thing.
// The buffer motion they leave behind is ctrl+e / ctrl+y, which is vi's own
// answer and which nothing else here wanted.

// moveCursor moves the cursor delta messages and brings it into view.
//
// It reports whether anything moved, so the caller can leave a press at
// either end of the history inert rather than redrawing for nothing.
func (m *Model) moveCursor(delta int) bool {
	msgs := m.store.Messages.Get(m.chatID)
	if len(msgs) == 0 {
		return false
	}

	// Where the cursor is now, resolved through the same rules a read would
	// use — including the tail rule, so } from the bottom of a live chat
	// starts at the newest message rather than at a stale identity.
	from := len(msgs) - 1
	if cur := m.cursorMessage(); cur != nil {
		for i, msg := range msgs {
			if msg.ID == cur.ID {
				from = i
				break
			}
		}
	}

	to := min(max(from+delta, 0), len(msgs)-1)
	if to == from {
		return false
	}

	// Pinned BEFORE the scroll: revealMessage can land on scrollOffset 0,
	// and the tail rule would then hand back the newest message instead of
	// the one the user just moved to.
	m.cursorPinned = true
	m.setCursor(msgs[to].ID)
	m.revealMessage(to)
	return true
}

// revealMessage scrolls the minimum needed to put a message fully on screen,
// and nothing when it already is.
//
// Minimally, rather than centring it the way scrollToMessage does for a
// search hit or a jump. A jump is a teleport and centring orients the reader
// afterwards; stepping between adjacent messages is reading, and re-centring
// the history under every press would move the text the reader is looking at
// on every keystroke.
func (m *Model) revealMessage(idx int) {
	counts := m.lineCounts()
	if idx < 0 || idx >= len(counts) {
		return
	}
	total := totalRenderedLines(counts)
	bodyH := m.bodyHeight()
	if bodyH < 1 {
		return
	}

	start := 0
	for i := range idx {
		start += counts[i]
	}
	end := start + counts[idx]

	winEnd := total - m.scrollOffset
	winStart := winEnd - bodyH

	switch {
	case counts[idx] >= bodyH:
		// Taller than the pane: show its head. Its foot is the arbitrary
		// end to leave off screen, since the sender and time are at the top
		// and they are what identifies it.
		winStart = start
	case start < winStart:
		winStart = start
	case end > winEnd:
		winStart = end - bodyH
	default:
		return // already whole on screen
	}

	m.scrollOffset = min(max(total-bodyH-winStart, 0), m.maxScrollOffset())
}

// unpinCursor hands the cursor back to the scroll position.
//
// Called by the gestures that mean "stop holding my place": jumping to the
// bottom of the history, and opening a chat. From that point the cursor is
// derived again, and at the bottom it follows arrivals — which is what a
// live chat needs, and what stickiness would break.
func (m *Model) unpinCursor() { m.cursorPinned = false }

// ClickAt moves the cursor to the message drawn on a panel-local row, and
// reports whether it moved anything.
//
// A mouse user had no way to choose what r, y or + would act on: a click
// focused the panel and left the cursor wherever the keyboard had put it
// (decision I-11). The row is resolved through the same line index View
// draws from, so what the click selects is what is under the pointer.
//
// A click on the header, on a day or unread divider, or on the blank space
// above a short history moves nothing. Those are not messages, and moving
// the cursor to the nearest one instead would be a guess the reader did not
// make.
func (m *Model) ClickAt(row int) bool {
	msgs := m.store.Messages.Get(m.chatID)
	if len(msgs) == 0 {
		return false
	}

	// The header, and the search or loading line under it when one is up.
	top := 1
	if m.statusLineVisible() {
		top++
	}
	bodyRow := row - top
	bodyH := m.bodyHeight()
	if bodyRow < 0 || bodyRow >= bodyH {
		return false
	}

	counts := m.lineCounts()
	total := totalRenderedLines(counts)
	end := min(max(total-m.scrollOffset, 0), total)
	start := max(end-bodyH, 0)

	// A history shorter than the pane is drawn at the BOTTOM, padded with
	// blanks above it — so the first content row is not row zero.
	pad := bodyH - (end - start)
	if bodyRow < pad {
		return false
	}
	line := start + (bodyRow - pad)

	pos := 0
	for i, n := range counts {
		if line >= pos+n {
			pos += n
			continue
		}
		// Inside message i. Its leading lines are dividers, which belong
		// to the calendar rather than to the message.
		var prev *telegram.Message
		if i > 0 {
			prev = msgs[i-1]
		}
		if line-pos < m.leadingDividers(msgs[i], prev) {
			return false
		}
		// Pinned like any explicit motion: the reader placed it, so it
		// stays put until G hands it back to the newest message.
		m.cursorPinned = true
		m.setCursor(msgs[i].ID)
		return true
	}
	return false
}

// leadingDividers is how many rows at the head of a message's block are
// dividers rather than the message. It mirrors the two that
// gridMessageLines emits, and exists so a click can tell them apart.
func (m Model) leadingDividers(msg, prev *telegram.Message) int {
	n := 0
	if prev == nil || !render.SameDay(prev.Date, msg.Date) {
		n++
	}
	if m.unreadFromID != 0 && msg.ID == m.unreadFromID {
		n++
	}
	return n
}
