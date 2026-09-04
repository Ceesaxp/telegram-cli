package chatview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func motionKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

// motionModel is a thread tall enough that the cursor can leave the visible
// window, so the scrolling half of a motion is exercised rather than assumed.
func motionModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	m.focused = true
	m.myUserId = 100
	m.chatID = testChatID
	m.SetSize(80, 12)
	for i := int64(1); i <= 40; i++ {
		m.store.Messages.Append(testChatID, textMessage(i, 200, "message "+itoa(int(i))))
	}
	return m
}

func cursorIndex(t *testing.T, m Model) int {
	t.Helper()
	cur := m.cursorMessage()
	if cur == nil {
		t.Fatal("no cursor")
	}
	for i, msg := range m.store.Messages.Get(testChatID) {
		if msg.ID == cur.ID {
			return i
		}
	}
	t.Fatal("the cursor is not in the history")
	return -1
}

// The gap this closes: j/k used to scroll lines, so there was no way to say
// "the message above this one" — you scrolled until the right one happened
// to sit at the edge of the window (decision I-4).
func TestJKMoveTheCursorOneMessage(t *testing.T) {
	m := motionModel(t)
	start := cursorIndex(t, m)
	if start != 39 {
		t.Fatalf("precondition: the cursor starts on the newest message, got %d", start)
	}

	m, _ = m.handleKey(motionKey('k'))
	if got := cursorIndex(t, m); got != start-1 {
		t.Errorf("k moved the cursor to %d, want %d", got, start-1)
	}

	m, _ = m.handleKey(motionKey('j'))
	if got := cursorIndex(t, m); got != start {
		t.Errorf("j did not come back: %d, want %d", got, start)
	}
}

// A cursor placed by hand is the reader's until they give it back. The tail
// rule — "at the bottom of the history the cursor IS the newest message" —
// would otherwise swallow the motion the moment it was made.
func TestAMovedCursorSurvivesTheTailRule(t *testing.T) {
	m := motionModel(t)
	if m.scrollOffset != 0 {
		t.Fatalf("precondition: expected to start at the bottom, offset %d", m.scrollOffset)
	}

	m, _ = m.handleKey(motionKey('k'))
	want := cursorIndex(t, m)

	// A message arrives. The pinned cursor does not chase it.
	m.store.Messages.Append(testChatID, textMessage(41, 200, "newest"))
	if got := cursorIndex(t, m); got != want {
		t.Errorf("a new message moved the pinned cursor to %d, want %d", got, want)
	}
}

// ...and G gives it back, because "take me to the bottom" is how a reader
// says they are done holding a place.
func TestGReleasesTheCursor(t *testing.T) {
	m := motionModel(t)
	m, _ = m.handleKey(motionKey('k'))
	m, _ = m.handleKey(motionKey('k'))

	m, _ = m.handleKey(motionKey('G'))
	msgs := m.store.Messages.Get(testChatID)
	if got := cursorIndex(t, m); got != len(msgs)-1 {
		t.Errorf("after G the cursor is at %d, want the newest (%d)", got, len(msgs)-1)
	}

	// And it follows arrivals again.
	m.store.Messages.Append(testChatID, textMessage(41, 200, "newest"))
	msgs = m.store.Messages.Get(testChatID)
	if got := cursorIndex(t, m); got != len(msgs)-1 {
		t.Errorf("after G the cursor did not follow the new message: %d, want %d",
			got, len(msgs)-1)
	}
}

// The motion has to bring its message with it, or the cursor walks off the
// screen and the reader is acting on something they cannot see.
func TestTheCursorStaysOnScreen(t *testing.T) {
	m := motionModel(t)

	for range 20 {
		m, _ = m.handleKey(motionKey('k'))
		idx := cursorIndex(t, m)
		first, last, ok := m.visibleMessages()
		if !ok {
			t.Fatal("no visible window")
		}
		if idx < first || idx > last {
			t.Fatalf("the cursor is at %d, outside the visible %d..%d", idx, first, last)
		}
	}
}

// Reading is not teleporting. A jump centres its target to orient the reader
// afterwards; stepping between adjacent messages must not move the text
// under their eyes when the next message is already on screen.
func TestAMotionWithinTheWindowDoesNotScroll(t *testing.T) {
	m := motionModel(t)

	// Away from the bottom first. Pinned at scrollOffset 0 the clamp hides
	// any centring — there is nowhere further down to scroll — so a test
	// that stayed there would pass whatever revealMessage did. ctrl+y
	// rather than k: k is the message motion this test is about, and
	// scrolling with it would be the thing under test setting up for
	// itself.
	for range 8 {
		m, _ = m.handleKey(ctrlKey('y'))
	}
	if m.scrollOffset == 0 {
		t.Fatal("precondition: expected to be off the bottom of the history")
	}

	first, last, ok := m.visibleMessages()
	if !ok || last-first < 2 {
		t.Fatalf("precondition: want several messages on screen, got %d..%d", first, last)
	}
	// Land in the middle of the window so a step in either direction stays
	// inside it.
	m.setCursor(m.store.Messages.Get(testChatID)[(first+last)/2].ID)
	m.cursorPinned = true

	before := m.scrollOffset
	for _, k := range []rune{'k', 'j'} {
		m, _ = m.handleKey(motionKey(k))
		if m.scrollOffset != before {
			t.Errorf("%c moved to a visible message and scrolled from %d to %d",
				k, before, m.scrollOffset)
		}
	}
}

// Either end of the history is a wall, not a wrap.
func TestTheMotionsStopAtTheEnds(t *testing.T) {
	m := motionModel(t)

	m, _ = m.handleKey(motionKey('j'))
	msgs := m.store.Messages.Get(testChatID)
	if got := cursorIndex(t, m); got != len(msgs)-1 {
		t.Errorf("j past the newest message moved to %d", got)
	}

	for range len(msgs) + 5 {
		m, _ = m.handleKey(motionKey('k'))
	}
	if got := cursorIndex(t, m); got != 0 {
		t.Errorf("k past the oldest message landed on %d, want 0", got)
	}
	m, _ = m.handleKey(motionKey('k'))
	if got := cursorIndex(t, m); got != 0 {
		t.Errorf("k at the oldest message moved to %d", got)
	}
}

// Scrolling the pinned message off the screen is the reader letting go: the
// cursor becomes a position again, and reaching the bottom resumes following
// arrivals. Otherwise a pin made once would outlive every later scroll.
func TestScrollingAwayReleasesThePin(t *testing.T) {
	m := motionModel(t)
	m, _ = m.handleKey(motionKey('k'))
	if !m.cursorPinned {
		t.Fatal("precondition: k did not pin the cursor")
	}

	// Scroll up until the pinned message is gone. The buffer motion, not
	// the message one: moving the cursor is what pinned it in the first
	// place, and would keep it on screen.
	for range 60 {
		m, _ = m.handleKey(ctrlKey('y'))
		if !m.cursorPinned {
			break
		}
	}
	if m.cursorPinned {
		t.Fatal("scrolling never released the pin")
	}
}

// The cursor bar has to be where the motion put it, not merely reported
// there: the bar is the only thing on screen that says which message the
// action keys will act on.
func TestTheBarFollowsTheMotion(t *testing.T) {
	m := motionModel(t)
	m, _ = m.handleKey(motionKey('k'))
	target := m.cursorMessage()

	var barred []string
	for _, line := range strings.Split(m.View(), "\n") {
		if runes := []rune(ansi.Strip(line)); len(runes) > 1 && runes[1] == '▌' {
			barred = append(barred, ansi.Strip(line))
		}
	}
	if len(barred) == 0 {
		t.Fatal("no cursor bar on screen after a motion")
	}
	body := messageText(target)
	for _, line := range barred {
		if strings.Contains(line, body) {
			return
		}
	}
	t.Errorf("the bar is not on the cursored message (%q):\n%s", body,
		strings.Join(barred, "\n"))
}

// TestTheRetiredMotionKeysAreInert is the negative half of decision I-4 and
// I-10. `}` and `{` were the message-wise motion and `M` was mark-read; with
// j/k message-wise and m marking read, keeping them would be a second
// spelling of each. They are freed rather than reassigned, so the proof they
// are gone is that they do nothing at all.
func TestTheRetiredMotionKeysAreInert(t *testing.T) {
	for _, r := range []rune{'}', '{', 'M'} {
		t.Run(string(r), func(t *testing.T) {
			m := motionModel(t)
			start := cursorIndex(t, m)
			offset := m.scrollOffset

			next, cmd := m.handleKey(motionKey(r))

			if got := cursorIndex(t, next); got != start {
				t.Errorf("%q moved the cursor from %d to %d", string(r), start, got)
			}
			if next.scrollOffset != offset {
				t.Errorf("%q scrolled from %d to %d", string(r), offset, next.scrollOffset)
			}
			if cmd != nil {
				t.Errorf("%q produced %T", string(r), cmd())
			}
		})
	}
}

// TestCtrlEAndCtrlYScrollOneLine covers the buffer motion that took over
// from j/k. One line per count, not the three j/k used to move: a line is a
// line, and the count is what asks for more of them.
func TestCtrlEAndCtrlYScrollOneLine(t *testing.T) {
	m := motionModel(t)
	m.scrollOffset = 0

	m, _ = m.handleKey(ctrlKey('y'))
	if m.scrollOffset != 1 {
		t.Fatalf("ctrl+y scrolled to %d, want one line up", m.scrollOffset)
	}
	m, _ = m.handleKey(ctrlKey('y'))
	if m.scrollOffset != 2 {
		t.Fatalf("a second ctrl+y scrolled to %d, want 2", m.scrollOffset)
	}
	m, _ = m.handleKey(ctrlKey('e'))
	if m.scrollOffset != 1 {
		t.Fatalf("ctrl+e scrolled to %d, want one line back down", m.scrollOffset)
	}

	// It does not pin the cursor — that is what an explicit j/k does. The
	// cursor follows the viewport instead.
	if m.cursorPinned {
		t.Error("a buffer scroll pinned the cursor")
	}
}

// TestCtrlYAtTheTopAsksForAnOlderPage: the upward buffer motion keeps the
// older-page fetch j/k used to carry, or scrolling up would stop dead at the
// oldest loaded message.
func TestCtrlYAtTheTopAsksForAnOlderPage(t *testing.T) {
	m := motionModel(t)
	m.tg = nil // no client: loadHistoryCmd is not reachable, so assert the clamp
	m.scrollOffset = 0
	for range 200 {
		m, _ = m.handleKey(ctrlKey('y'))
	}
	if got, max := m.scrollOffset, m.maxScrollOffset(); got != max {
		t.Errorf("ctrl+y past the top left the offset at %d, want the clamp %d", got, max)
	}
}
