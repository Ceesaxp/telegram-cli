package composer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
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

// Emacs mode never draws vi's character-cursor: its caret is always a gap,
// because that is always where the next keystroke lands. The gap is drawn
// as an underline on the character it precedes — a position, not a cell, and
// a terminal has only cells.
func TestEmacsModeNeverDrawsTheNormalModeCaret(t *testing.T) {
	m := sized(t, 60)
	m.SetEditingMode(ModeEmacs)
	m.SetFocused(true)
	m.textarea.InsertString("12345678")
	m.textarea.MoveLineStart()

	if caretColumn(m.View()) < 0 {
		t.Errorf("emacs mode lost its caret:\n%s", ansi.Strip(m.View()))
	}
	if strings.Contains(m.View(), "\x1b[7m") {
		t.Errorf("emacs mode drew a character-cursor:\n%s",
			strings.ReplaceAll(m.View(), "\x1b", "ESC"))
	}

	// At the end of the line there is nothing to underline, so the block is
	// the caret — the same fallback every mode takes.
	m.textarea.MoveLineEnd()
	if !strings.Contains(ansi.Strip(m.View()), cursorBlock) {
		t.Errorf("emacs mode lost its block at the end of the line:\n%s",
			ansi.Strip(m.View()))
	}
}

// TestTheCaretNeverWidensTheLine.
//
// A caret is a position between characters and a terminal has only cells,
// so drawing the position as an inserted block made the rest of the line
// step one cell right: "123" with the caret before the 3 came out as four
// cells, and the 3 appeared to move the moment you pressed i. It read as
// though i had typed a space.
func TestTheCaretNeverWidensTheLine(t *testing.T) {
	for _, mode := range []EditingMode{ModeEmacs, ModeVi} {
		m := sized(t, 60)
		m.SetEditingMode(mode)
		m.SetFocused(true)
		m.textarea.InsertString("123")

		// At the end there is nothing to draw on, so the caret is a block
		// and costs the cell no character was using.
		m.textarea.MoveLineEnd()
		atEnd := cell.Width(ansi.Strip(m.draftLine(60)))

		// Anywhere else it draws ON a character and costs nothing.
		for cursor := range 3 {
			m.textarea.Cursor = cursor
			line := ansi.Strip(m.draftLine(60))
			if got := cell.Width(line); got != atEnd-1 {
				t.Errorf("%v with the caret at %d: line is %d cells, want %d — %q",
					mode, cursor, got, atEnd-1, line)
			}
			if strings.Contains(line, cursorBlock) {
				t.Errorf("%v with the caret at %d: a block was wedged into %q",
					mode, cursor, line)
			}
			if !strings.Contains(line, "123") {
				t.Errorf("%v with the caret at %d: the text was disturbed: %q",
					mode, cursor, line)
			}
		}
	}
}

// TestTheTwoModesStayApart. Both draw on the character now, so the shape is
// the only thing left telling a reader which mode they are in besides the
// badge — reverse for normal, an underline for insert, which is the pair vim
// asks the terminal for.
func TestTheTwoModesStayApart(t *testing.T) {
	draw := func(mode EditingMode, normal bool) string {
		m := sized(t, 60)
		m.SetEditingMode(mode)
		m.SetFocused(true)
		m.textarea.InsertString("123")
		m.textarea.Cursor = 1
		if normal {
			m.vi = viNormal
		}
		return m.draftLine(60)
	}

	insert, normal := draw(ModeVi, false), draw(ModeVi, true)
	if insert == normal {
		t.Fatal("insert and normal draw the caret identically")
	}
	if !strings.Contains(normal, "\x1b[7m") {
		t.Errorf("normal mode is not reverse video: %q",
			strings.ReplaceAll(normal, "\x1b", "ESC"))
	}
	if !strings.Contains(insert, "\x1b[4") {
		t.Errorf("insert mode is not underlined: %q",
			strings.ReplaceAll(insert, "\x1b", "ESC"))
	}
}
