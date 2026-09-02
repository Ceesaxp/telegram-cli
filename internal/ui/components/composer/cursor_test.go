package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Pressing i must do something you can see.
//
// Before this, focusing an empty composer changed the badge's colour and
// nothing else: promptContent's placeholder branch drew no cursor at all.
// Every motion key then moved an invisible caret, which is indistinguishable
// from a motion key that does nothing — and is why the line editing was
// reported as absent when ctrl+a, ctrl+e, 0 and $ had all worked from the
// day they were written.
func TestAnEmptyComposerShowsItsCursorWhenFocused(t *testing.T) {
	m := sized(t, 60)

	if strings.Contains(ansi.Strip(m.View()), cursorBlock) {
		t.Error("an unfocused composer draws a cursor")
	}

	m.SetFocused(true)
	if !strings.Contains(ansi.Strip(m.View()), cursorBlock) {
		t.Errorf("a focused empty composer draws no cursor:\n%s", ansi.Strip(m.View()))
	}
}

// And it has to keep showing it once there is a draft, at the position the
// motions move to: a caret that only appears on an empty line is worse than
// none, because it teaches the wrong thing about where typing will land.
func TestTheCursorFollowsTheMotions(t *testing.T) {
	m := sized(t, 60)
	m.SetFocused(true)
	m.textarea.InsertString("hello world")

	at := func() int { return caretColumn(m.View()) }

	end := at()
	if end < 0 {
		t.Fatal("no cursor after typing")
	}

	m.textarea.MoveLineStart()
	start := at()
	if start < 0 {
		t.Fatal("the cursor vanished at the line start")
	}
	if start >= end {
		t.Errorf("ctrl+a did not move the cursor left: column %d, was %d", start, end)
	}

	m.textarea.MoveLineEnd()
	if got := at(); got != end {
		t.Errorf("ctrl+e did not return the cursor: column %d, want %d", got, end)
	}
}

// The placeholder still says what to do. A bare cursor on an empty line
// tells a new reader nothing about what this row is for.
func TestTheEmptyComposerStillSaysWhatItIsFor(t *testing.T) {
	m := sized(t, 60)
	m.SetFocused(true)

	if view := ansi.Strip(m.View()); !strings.Contains(view, "type a message") {
		t.Errorf("the cursor replaced the placeholder instead of preceding it:\n%s", view)
	}
}

// caretColumn is where the caret is drawn, in display cells from the start
// of the row, or -1 when there is none.
//
// By POSITION rather than by glyph. The caret is a block only where there
// is no character under it; everywhere else it is the character itself,
// styled — so a test that looks for the block finds nothing mid-line and
// concludes the cursor has vanished, which is what these two did.
func caretColumn(view string) int {
	if i := strings.Index(ansi.Strip(view), cursorBlock); i >= 0 {
		return i
	}
	// A styled character: find where its escape sequence opens and measure
	// the visible text before it. Underline is matched on the prefix
	// because lipgloss writes it as "4;4" — the attribute and its style —
	// rather than as a bare 4.
	for _, open := range []string{"\x1b[7m", "\x1b[4"} {
		if i := strings.Index(view, open); i >= 0 {
			return len(ansi.Strip(view[:i]))
		}
	}
	return -1
}
