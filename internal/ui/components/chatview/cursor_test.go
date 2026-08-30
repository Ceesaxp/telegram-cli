package chatview

import (
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// cursorTestModel is twenty short messages in a window a few messages tall,
// so the visible band is a small slice of the history and one message
// changing height is enough to move the bottom of the window.
func cursorTestModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	m.SetSize(60, 12)
	for i := int64(1); i <= 20; i++ {
		m.store.Messages.Append(testChatID, textMessage(i, 100, "line"))
	}
	return m
}

// grow replaces a message's content with n lines of text, standing in for a
// photo thumbnail landing and turning a one-line placeholder into art.
func grow(m Model, id int64, n int) {
	for _, msg := range m.store.Messages.Get(testChatID) {
		if msg.ID == id {
			msg.Content = &telegram.MessageText{
				Text: &telegram.FormattedText{
					Text: strings.TrimRight(strings.Repeat("art\n", n), "\n"),
				},
			}
		}
	}
	m.cache.invalidate(id)
}

// cursorVisible reports whether the cursor's message currently has a line
// inside the body window.
func cursorVisible(m Model) bool {
	first, last, ok := m.visibleMessages()
	if !ok {
		return false
	}
	for i, msg := range m.store.Messages.Get(testChatID) {
		if msg.ID == m.cursorID {
			return i >= first && i <= last
		}
	}
	return false
}

func bottomVisibleID(m Model) int64 {
	_, last, ok := m.visibleMessages()
	if !ok {
		return 0
	}
	return m.store.Messages.Get(testChatID)[last].ID
}

func topVisibleID(m Model) int64 {
	first, _, ok := m.visibleMessages()
	if !ok {
		return 0
	}
	return m.store.Messages.Get(testChatID)[first].ID
}

// TestCursorHoldsItsMessageWhenSomethingElseGrows is the bug the explicit
// cursor exists to fix.
//
// The old rule was "the message containing the last visible line". A photo
// below the fold finishing its download turns a one-line placeholder into
// several lines of art, which slides the window and hands the action keys a
// message the user never looked at. Nothing the user did changed the
// target — a background download did.
func TestCursorHoldsItsMessageWhenSomethingElseGrows(t *testing.T) {
	m := cursorTestModel(t)
	m.scrollOffset = 5
	m.syncCursor()

	held := m.cursorID
	if held == 0 || held != bottomVisibleID(m) {
		t.Fatalf("expected the cursor to anchor on the bottom-visible message, got %d", held)
	}

	// The newest message is below the fold. Growing it slides the window
	// towards older messages under a fixed scroll offset.
	msgs := m.store.Messages.Get(testChatID)
	grow(m, msgs[len(msgs)-1].ID, 2)

	if bottomVisibleID(m) == held {
		t.Fatalf("test is vacuous: the bottom of the window did not move")
	}
	if !cursorVisible(m) {
		t.Fatalf("test is vacuous: message %d left the window entirely", held)
	}
	if got := m.cursorMessage(); got == nil || got.ID != held {
		t.Fatalf("cursor drifted from %d to %v when another message grew", held, got)
	}
}

// TestCursorIsClampedBackIntoTheWindow pins the other half of the rule: an
// identity that has scrolled out of sight is not a usable target, so it
// gives way to the nearest message that is on screen. Nearest, not a fixed
// end — that is what makes the two scroll directions feel the same.
func TestCursorIsClampedBackIntoTheWindow(t *testing.T) {
	// A long history, entered half way up: both directions then have room
	// to carry the cursor out of the window without running into the tail,
	// where a different rule takes over.
	m := newTestModel()
	m.SetSize(60, 12)
	for i := int64(1); i <= 100; i++ {
		m.store.Messages.Append(testChatID, textMessage(i, 100, "line"))
	}
	m.scrollOffset = m.maxScrollOffset() / 2
	m.syncCursor()
	if m.cursorID == 0 {
		t.Fatalf("expected an anchored cursor")
	}

	// Towards older messages: scroll until the cursor is carried off the
	// bottom. The step count is discovered rather than assumed, so the
	// test does not quietly stop exercising the clamp when message heights
	// change.
	scrollUntilCursorMoves(t, &m, 3)
	if got, want := m.cursorID, bottomVisibleID(m); got != want {
		t.Fatalf("expected the cursor clamped to the bottom of the window (%d), got %d", want, got)
	}

	// Back towards newer messages: it falls off the top and clamps to that
	// end rather than jumping across the window.
	scrollUntilCursorMoves(t, &m, -3)
	if got, want := m.cursorID, topVisibleID(m); got != want {
		t.Fatalf("expected the cursor clamped to the top of the window (%d), got %d", want, got)
	}
}

// scrollUntilCursorMoves scrolls in steps of n lines until the anchored
// cursor is pushed out of the window, failing if it never is.
func scrollUntilCursorMoves(t *testing.T, m *Model, n int) {
	t.Helper()
	held := m.cursorID
	for range 40 {
		m.ScrollByLines(n)
		if m.scrollOffset == 0 {
			// Tail mode owns the cursor there, so anything that happened
			// is not the clamp this is trying to observe.
			t.Fatalf("reached the bottom of the history before the clamp fired")
		}
		if m.cursorID != held {
			return
		}
	}
	t.Fatalf("scrolling by %d never carried message %d out of the window", n, held)
}

// TestCursorFollowsTheTail pins that stickiness stops at the bottom of the
// history. Sitting in a live chat, r has to reply to the message that just
// arrived; a cursor holding its position there would send replies to
// whatever the reader happened to be looking at a minute ago.
func TestCursorFollowsTheTail(t *testing.T) {
	m := cursorTestModel(t)
	m.syncCursor()
	if got := m.cursorMessage(); got == nil || got.ID != 20 {
		t.Fatalf("expected the cursor on the newest message, got %v", got)
	}

	m.store.Messages.Append(testChatID, textMessage(21, 100, "just in"))
	if got := m.cursorMessage(); got == nil || got.ID != 21 {
		t.Fatalf("expected the cursor to follow the new arrival, got %v", got)
	}

	// Scrolling up leaves tail mode, and from there the cursor holds.
	m.ScrollByLines(6)
	held := m.cursorID
	if held == 0 || held == 21 {
		t.Fatalf("expected an anchored cursor away from the tail, got %d", held)
	}
	m.store.Messages.Append(testChatID, textMessage(22, 100, "and another"))
	if got := m.cursorMessage(); got == nil || got.ID != held {
		t.Fatalf("expected the cursor to hold message %d away from the tail, got %v", held, got)
	}
}

// TestJumpSetsTheCursor: a jump is the one movement that names the message
// it is about, so it claims the cursor outright. Landing on a search hit and
// pressing r must reply to the hit, not to the edge of the window it landed
// in.
func TestJumpSetsTheCursor(t *testing.T) {
	m := cursorTestModel(t)
	if !m.scrollToMessage(7) {
		t.Fatalf("expected message 7 to be found")
	}
	if got := m.cursorMessage(); got == nil || got.ID != 7 {
		t.Fatalf("expected the jump target to become the cursor, got %v", got)
	}
	if bottomVisibleID(m) == 7 {
		t.Fatalf("test is vacuous: the jump left message 7 at the bottom of the window")
	}
}

// TestActionKeysActOnTheCursor closes the loop: the action keys read the
// same identity the renderer marks, not a second, separately-derived
// position.
func TestActionKeysActOnTheCursor(t *testing.T) {
	m := cursorTestModel(t)
	m.focused = true
	m.scrollOffset = 5
	m.syncCursor()
	held := m.cursorID

	m2, cmd := m.handleKey(key('r'))
	if got := dispatchedAction(t, cmd); got.MessageId != held {
		t.Fatalf("reply targeted %d, cursor is on %d", got.MessageId, held)
	}
	if m2.cursorID != held {
		t.Fatalf("expected a non-scroll key to leave the cursor alone, got %d", m2.cursorID)
	}
}

// TestCursorIsReleasedOnChatOpen: the identity is per-chat, and a stale one
// must not follow the user into a chat where that ID means another message.
func TestCursorIsReleasedOnChatOpen(t *testing.T) {
	m := cursorTestModel(t)
	m.scrollOffset = 5
	m.syncCursor()
	if m.cursorID == 0 {
		t.Fatalf("expected an anchored cursor")
	}

	m.OpenChatAt(testChatID, "other", 0)
	if m.cursorID != 0 {
		t.Fatalf("expected the cursor to be released on chat open, got %d", m.cursorID)
	}
}
