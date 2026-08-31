package composer

import "testing"

// The reset belongs on the transition, not on every call.
//
// SetFocused is called on the way in, but it is also called whenever the app
// re-asserts where focus already is — a resize, a redraw after an overlay
// closes, a re-entrant focus set. If the reset fired on every call rather
// than on unfocused→focused, a vi user who pressed Escape and then did
// anything that re-asserted focus would be silently put back into insert
// mode, and their next motion key would type itself into the draft.
func TestReAssertingFocusKeepsVisMode(t *testing.T) {
	m := viDraft(t, "hello")

	m, _ = press(t, m, "esc")
	if !m.IsViNormalMode() {
		t.Fatal("precondition: escape did not reach normal mode")
	}

	m.SetFocused(true)
	if !m.IsViNormalMode() {
		t.Error("re-asserting focus dropped the composer back into insert mode")
	}

	// x is normal mode's delete, and only reaches the buffer as a command
	// if the mode really survived.
	m, _ = press(t, m, "x")
	if got := m.Draft(); got != "hell" {
		t.Errorf("draft = %q after x, want %q — x was taken as text", got, "hell")
	}
}

// Leaving and coming back is the transition, and that does reset.
func TestLeavingAndReturningStartsInInsertMode(t *testing.T) {
	m := viDraft(t, "hello")

	m, _ = press(t, m, "esc")
	m.SetFocused(false)
	m.SetFocused(true)

	if m.IsViNormalMode() {
		t.Fatal("re-entering the composer left it in vi normal mode")
	}
	// Escape parked the cursor ON the final character, and re-entering
	// resumes there rather than jumping to the end — so the x lands before
	// the o. What matters is that it landed as text at all: in normal mode
	// it would have deleted one.
	m, _ = press(t, m, "x")
	if got := m.Draft(); got != "hellxo" {
		t.Errorf("draft = %q after x, want %q — x was taken as a command", got, "hellxo")
	}
}
