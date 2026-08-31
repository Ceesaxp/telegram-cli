package chatview

// Message-wise cursor motion.
//
// j/k and the arrows scroll LINES, as they always have and as the design
// record specifies: a thread is a buffer and the buffer motions move through
// it. That left no way to move the cursor itself — it was a side effect of
// scrolling, anchored back into the visible window by syncCursor, so
// "reply to the message above this one" meant scrolling until the right one
// happened to be at the edge.
//
// } and { are the vi analogue and the reason they are the right keys: a
// message IS a paragraph, } and { are how vi moves between paragraphs, and
// both are free here (n/N cycle search hits, [ and ] switch folders).

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
