package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// viDraft is a composer in vi mode with a draft already typed, ready for
// Escape.
func viDraft(t *testing.T, text string) Model {
	t.Helper()
	m := sized(t, 60)
	m.SetEditingMode(ModeVi)
	m.SetFocused(true)
	m.textarea.InsertString(text)
	return m
}

// vi has two cursors and this drew both the same way.
//
// In INSERT the caret is a GAP between characters, so a block belongs before
// the character at the cursor: that is where typing lands. In NORMAL the
// cursor sits ON a character — which is why Escape appears to step back one,
// why x deletes "the character under the cursor", and why i inserts before
// it. Drawing normal mode's cursor as a gap made "type 12345678, press
// Escape" look as though the caret had jumped between the 7 and the 8. It was
// on the 8, drawn as though it were before it.
func TestNormalModeDrawsTheCursorOnTheCharacter(t *testing.T) {
	m := viDraft(t, "12345678")

	insert := m.View()
	if !strings.Contains(ansi.Strip(insert), cursorBlock) {
		t.Fatalf("insert mode draws no block cursor:\n%s", ansi.Strip(insert))
	}

	m, _ = press(t, m, "esc")
	if !m.IsViNormalMode() {
		t.Fatal("precondition: escape did not reach normal mode")
	}

	normal := m.View()
	if strings.Contains(ansi.Strip(normal), cursorBlock) {
		t.Errorf("normal mode still wedges a block between characters:\n%s",
			ansi.Strip(normal))
	}
	// The text is unchanged — the caret is a highlight on the 8, not an
	// extra glyph, so nothing was inserted or lost.
	if got := ansi.Strip(normal); !strings.Contains(got, "12345678") {
		t.Errorf("the draft changed when the caret did:\n%s", got)
	}
	// And the 8 is the character wearing it.
	if !strings.Contains(normal, "\x1b[7m8") {
		t.Errorf("the caret is not on the last character:\n%s",
			strings.ReplaceAll(normal, "\x1b", "ESC"))
	}
}

// Motions move the highlight, and the character under it is the one the
// commands act on.
func TestTheNormalModeCaretFollowsTheMotions(t *testing.T) {
	m := viDraft(t, "12345678")
	m, _ = press(t, m, "esc")

	m, _ = press(t, m, "0")
	if !strings.Contains(m.View(), "\x1b[7m1") {
		t.Errorf("0 did not put the caret on the first character:\n%s",
			strings.ReplaceAll(m.View(), "\x1b", "ESC"))
	}

	m, _ = press(t, m, "$")
	if !strings.Contains(m.View(), "\x1b[7m8") {
		t.Errorf("$ did not put the caret on the last character:\n%s",
			strings.ReplaceAll(m.View(), "\x1b", "ESC"))
	}
}

// An empty line has no character to sit on, so both modes fall back to the
// block — a caret that vanished on an empty draft would be worse than one
// drawn in the other convention.
func TestAnEmptyLineKeepsTheBlockCaret(t *testing.T) {
	m := sized(t, 60)
	m.SetEditingMode(ModeVi)
	m.SetFocused(true)
	m.textarea.InsertString("abc\n")
	m, _ = press(t, m, "esc")

	if !strings.Contains(ansi.Strip(m.View()), cursorBlock) {
		t.Errorf("an empty line lost its caret:\n%s", ansi.Strip(m.View()))
	}
}

// Emacs mode never has a character-cursor: its caret is always a gap, because
// that is always where the next keystroke lands.
func TestEmacsModeAlwaysDrawsTheBlock(t *testing.T) {
	m := sized(t, 60)
	m.SetEditingMode(ModeEmacs)
	m.SetFocused(true)
	m.textarea.InsertString("12345678")
	m.textarea.MoveLineStart()

	if !strings.Contains(ansi.Strip(m.View()), cursorBlock) {
		t.Errorf("emacs mode lost its block caret:\n%s", ansi.Strip(m.View()))
	}
	if strings.Contains(m.View(), "\x1b[7m") {
		t.Errorf("emacs mode drew a character-cursor:\n%s",
			strings.ReplaceAll(m.View(), "\x1b", "ESC"))
	}
}
