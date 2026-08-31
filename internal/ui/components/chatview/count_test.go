package chatview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// 9{ moves nine messages back. The whole point of the count prefix.
func TestACountRepeatsTheMessageMotion(t *testing.T) {
	m := motionModel(t)
	start := cursorIndex(t, m)

	for _, r := range "9" {
		m, _ = m.handleKey(motionKey(r))
	}
	m, _ = m.handleKey(motionKey('{'))

	if got := cursorIndex(t, m); got != start-9 {
		t.Errorf("9{ moved to %d, want %d", got, start-9)
	}
}

// Multi-digit counts accumulate left to right, as they do in vi.
func TestCountsAccumulate(t *testing.T) {
	m := motionModel(t)
	start := cursorIndex(t, m)

	for _, r := range "12" {
		m, _ = m.handleKey(motionKey(r))
	}
	m, _ = m.handleKey(motionKey('{'))

	if got := cursorIndex(t, m); got != start-12 {
		t.Errorf("12{ moved to %d, want %d", got, start-12)
	}
}

// A count is spent by the motion it precedes and does not survive into the
// next one: "9{" then "{" moves nine and then one, not nine and nine.
func TestACountIsSpentOnce(t *testing.T) {
	m := motionModel(t)
	start := cursorIndex(t, m)

	for _, r := range "9" {
		m, _ = m.handleKey(motionKey(r))
	}
	m, _ = m.handleKey(motionKey('{'))
	m, _ = m.handleKey(motionKey('{'))

	if got := cursorIndex(t, m); got != start-10 {
		t.Errorf("9{ then { moved to %d, want %d", got, start-10)
	}
}

// It repeats the line motions too, so a vi reader's 5j does what they mean
// rather than silently discarding the 5.
func TestACountRepeatsTheLineMotion(t *testing.T) {
	m := motionModel(t)
	m.scrollOffset = 0

	one := motionModel(t)
	one.scrollOffset = 0
	one, _ = one.handleKey(motionKey('k'))
	step := one.scrollOffset

	for _, r := range "4" {
		m, _ = m.handleKey(motionKey(r))
	}
	m, _ = m.handleKey(motionKey('k'))

	if m.scrollOffset != step*4 {
		t.Errorf("4k scrolled %d lines, want %d", m.scrollOffset, step*4)
	}
}

// A number typed and then abandoned must not attach itself to whatever comes
// next. Anything that is not a digit takes the count with it.
func TestANonMotionKeyClearsTheCount(t *testing.T) {
	m := motionModel(t)
	start := cursorIndex(t, m)

	for _, r := range "9" {
		m, _ = m.handleKey(motionKey(r))
	}
	m, _ = m.handleKey(motionKey('x')) // reveal spoilers: not a motion
	if m.pendingCount != 0 {
		t.Fatalf("a non-motion key left %d pending", m.pendingCount)
	}

	m, _ = m.handleKey(motionKey('{'))
	if got := cursorIndex(t, m); got != start-1 {
		t.Errorf("the abandoned count reattached: moved to %d, want %d",
			got, start-1)
	}
}

// A bare 0 is not a count. vi gives it to the start of the line, and a "0"
// that silently began one would make "01" mean 1 while "0" meant nothing
// visible at all.
func TestALeadingZeroIsNotACount(t *testing.T) {
	m := motionModel(t)
	m, _ = m.handleKey(motionKey('0'))
	if m.pendingCount != 0 {
		t.Errorf("a leading 0 started a count of %d", m.pendingCount)
	}

	// The consequence that matters is not the value — 0*10+0 is 0 either
	// way — but that the key is NOT CONSUMED, so "0" stays available for a
	// motion to bind. A version without the guard swallows it, and today
	// nothing would notice.
	if m.countDigit("0") {
		t.Error("a bare 0 was consumed as a count digit")
	}

	// But once a count is under way it is an ordinary digit.
	for _, r := range "10" {
		m, _ = m.handleKey(motionKey(r))
	}
	if !m.countDigit("0") {
		t.Error("0 was refused while a count was under way")
	}
	if m.pendingCount != 100 {
		t.Errorf("pendingCount = %d after \"100\", want 100", m.pendingCount)
	}
}

// A leaned-on digit key must not overflow its way to a negative count.
func TestACountIsBounded(t *testing.T) {
	m := motionModel(t)
	for range 30 {
		m, _ = m.handleKey(motionKey('9'))
	}
	if m.pendingCount < 0 || m.pendingCount > maxCount {
		t.Errorf("pendingCount = %d, want it inside [0, %d]", m.pendingCount, maxCount)
	}

	// And it still moves, to the far end rather than past it.
	m, _ = m.handleKey(motionKey('{'))
	if got := cursorIndex(t, m); got != 0 {
		t.Errorf("a huge count landed on %d, want the oldest message", got)
	}
}

// The count is visible while it is pending: a digit that changes nothing on
// screen cannot be told from a key the thread ignores.
func TestThePendingCountIsShown(t *testing.T) {
	m := motionModel(t)

	before := ansi.Strip(m.renderHeader())
	if strings.Contains(before, "12") {
		t.Fatalf("precondition: the header already reads 12: %q", before)
	}

	for _, r := range "12" {
		m, _ = m.handleKey(motionKey(r))
	}
	if got := ansi.Strip(m.renderHeader()); !strings.Contains(got, "12") {
		t.Errorf("the pending count is not on the header: %q", got)
	}

	m, _ = m.handleKey(motionKey('{'))
	if got := ansi.Strip(m.renderHeader()); strings.Contains(got, "12  ln") {
		t.Errorf("the spent count is still on the header: %q", got)
	}
}

// Opening another chat drops it, like every other piece of per-chat state.
func TestOpeningAChatClearsTheCount(t *testing.T) {
	m := motionModel(t)
	for _, r := range "9" {
		m, _ = m.handleKey(motionKey(r))
	}
	m.OpenChat(testChatID+1, "elsewhere")

	if m.pendingCount != 0 {
		t.Errorf("opening a chat left %d pending", m.pendingCount)
	}
}
