package composer

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// EditingMode selects which line-editing keymap the composer speaks. The
// default is ModeEmacs, so a Model that is never configured behaves exactly as
// it did before vi mode existed.
//
// Only the composer is modal. Every other widgets.TextArea user (the search
// overlay, chatview's find bar, the auth prompts) keeps emacs semantics
// unconditionally — a one-line search field has nothing to gain from a normal
// mode, and Escape there already means "close the overlay".
type EditingMode int

const (
	// ModeEmacs is the readline keymap: ctrl+a/e move to the ends of the
	// line, ctrl+b/f a character, ctrl+k/u kill to either end, ctrl+w kills
	// the previous word, ctrl+d deletes forward.
	ModeEmacs EditingMode = iota
	// ModeVi is the modal keymap: Escape leaves insert mode for a normal
	// mode with h/j/k/l, w/b, 0/$, x, dd, D and i/a/A/o/O.
	ModeVi
)

func (e EditingMode) String() string {
	if e == ModeVi {
		return "vi"
	}
	return "emacs"
}

// EditingModeFor maps a configured keymap name onto an EditingMode. It is the
// bridge from config.ResolveComposeEditing (which returns "emacs" or "vi")
// and, being total, treats anything it does not recognise as emacs rather
// than leaving the composer in a mode the user did not ask for.
func EditingModeFor(name string) EditingMode {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "vi", "vim", "nvim", "neovim":
		return ModeVi
	default:
		return ModeEmacs
	}
}

// SetEditingMode selects the composer's line-editing keymap. Switching to vi
// starts in insert mode, which is what a chat composer wants: typing a
// message is the common case and a normal-mode landing would swallow the
// first word.
func (m *Model) SetEditingMode(mode EditingMode) {
	m.editing = mode
	m.vi = viInsert
	m.viPending = 0
}

// EditingMode reports the composer's current line-editing keymap.
func (m Model) EditingMode() EditingMode { return m.editing }

// viState is the modal state within ModeVi. It is meaningless in ModeEmacs.
type viState int

const (
	viInsert viState = iota
	viNormal
)

// IsViNormalMode reports whether vi mode is active and currently in normal
// mode. Exposed for the status/hint rendering and for tests.
func (m Model) IsViNormalMode() bool { return m.editing == ModeVi && m.vi == viNormal }

// isNewlineChord reports whether a keystroke should insert a hard line break.
//
// What the decoder actually produces (verified against uv.EventDecoder, the
// same decoder bubbletea v2's input loop runs — see editing_test.go):
//
//	byte 0x0D                -> "enter"        (submit)
//	byte 0x0A                -> "ctrl+j"       distinct from enter, every terminal
//	CSI 13;2u / CSI 27;2;13~ -> "shift+enter"  Kitty / modifyOtherKeys only
//	CSI 13;5u                -> "ctrl+enter"   Kitty only
//	CSI 13;3u, ESC CR        -> "alt+enter"    see the caveat below
//
// ctrl+j is the primary chord because it is the only one every terminal can
// send: the legacy encoding has no way to express a modifier on Enter, so
// shift+enter and ctrl+enter simply never arrive outside a terminal speaking
// the Kitty keyboard protocol or xterm's modifyOtherKeys.
//
// alt+enter is not primary for the reason documented on config.KeyConfig: a
// terminal that does not report Option/Alt as a modifier (Ghostty's default
// on macOS, Terminal.app, iTerm2's default) makes every alt binding both
// unreachable and undetectable. It is accepted anyway where it costs nothing.
//
// None of them insert in vi's normal mode. Normal mode is not a place where
// text gets inserted — that is what o/O are for, and the hint line stops
// advertising ctrl+j there, so honouring it anyway would be a mode leak the
// user was told not to expect. It also disposes of a real hazard: the legacy
// encoding of alt+enter is ESC CR, byte-for-byte what "Escape, then Enter"
// produces, and that is the sequence a vi user types constantly to leave
// insert mode and send.
func (m Model) isNewlineChord(stroke string) bool {
	if m.IsViNormalMode() {
		return false
	}
	switch stroke {
	case "ctrl+j", "shift+enter", "ctrl+enter", "alt+enter":
		return true
	}
	return false
}

// viClampCursor implements vi's normal-mode cursor rule: the cursor sits ON a
// character, never in the gap after the last one. Insert mode uses the other
// convention (the cursor is a gap between characters), and the textarea's
// primitives are written for that, so normal mode re-establishes its own
// invariant after every motion and deletion.
//
// This is what makes x, i and a behave the way a vi user expects without each
// of them needing its own end-of-line special case: "$x" deletes the last
// character of the line rather than the line break, and "$i" inserts before
// the last character rather than after it. An empty line has no character to
// sit on, so the cursor stays in the gap and the commands that read a
// character (x) find nothing to do.
func (m *Model) viClampCursor() {
	start, end := m.textarea.LineBounds()
	if end > start && m.textarea.Cursor >= end {
		m.textarea.Cursor = end - 1
	}
}

// handleViNormal runs one vi normal-mode command.
//
// Normal-mode commands are unmodified printables, so String() is the right
// spelling here and Keystroke() is not: the Kitty protocol reports "$" as
// Keystroke "shift+4" and "D" as "shift+d", while String() gives the
// character the layout actually produced in both cases. This is the mirror
// image of the chord matching in handleKey, and the same rule app.go's
// keyPress.matches follows.
func (m Model) handleViNormal(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	m.notice = ""

	// Anything carrying ctrl/alt is a chord, not a normal-mode command, and
	// the chords that matter were already handled in handleKey.
	if msg.Mod&^(tea.ModShift|tea.ModCapsLock|tea.ModNumLock|tea.ModScrollLock) != 0 {
		return m, nil
	}
	cmd := msg.String()

	// Pending operator. Only "d" takes one, and only "dd" completes it —
	// any other key aborts, exactly as vi does.
	if m.viPending == 'd' {
		m.viPending = 0
		if cmd == "d" {
			m.textarea.DeleteLine()
			m.viClampCursor()
		}
		return m, nil
	}

	switch cmd {
	// Motions.
	case "h", "left":
		m.textarea.MoveLeft()
	case "l", "right":
		m.textarea.MoveRight()
	case "j", "down":
		m.textarea.MoveDown()
	case "k", "up":
		m.textarea.MoveUp()
	case "w":
		m.textarea.MoveWordForward()
	case "b":
		m.textarea.MoveWordBack()
	case "0", "home":
		m.textarea.MoveLineStart()
	case "$", "end":
		m.textarea.MoveLineEnd()

	// Deletions.
	case "x":
		// Only ever deletes a character of this line. Without the guard a
		// cursor at the line end — which is where a stale insert-mode
		// position leaves it — would delete the line break and silently
		// join the two lines.
		if _, end := m.textarea.LineBounds(); m.textarea.Cursor < end {
			m.textarea.DeleteChar()
		}
	case "d":
		m.viPending = 'd'
	case "D":
		// Delete to end of line. Unlike emacs ctrl+k this never swallows
		// the line break, so D on an empty line does nothing.
		_, end := m.textarea.LineBounds()
		m.textarea.DeleteRange(m.textarea.Cursor, end)

	// Entering insert mode. These deliberately leave the cursor in the
	// insert-mode convention (the gap), which is why the clamp below is
	// skipped once m.vi has flipped.
	case "i":
		m.vi = viInsert
	case "a":
		m.textarea.MoveRight()
		m.vi = viInsert
	case "A":
		m.textarea.MoveLineEnd()
		m.vi = viInsert
	case "o":
		m.textarea.MoveLineEnd()
		m.textarea.InsertNewline()
		m.vi = viInsert
	case "O":
		m.textarea.MoveLineStart()
		m.textarea.InsertNewline()
		// InsertNewline leaves the cursor after the break, i.e. at the
		// start of the line that was just pushed down; step back onto the
		// blank line it opened above.
		m.textarea.MoveLeft()
		m.vi = viInsert
	}

	if m.vi == viNormal {
		m.viClampCursor()
	}
	return m, nil
}

// viIndicator returns the modal-state banner for the hint line, empty in
// emacs mode.
// expandedHint is the expanded composer's footer: the chords that do
// something here, in the order they matter.
//
// Enter and the way back out come first because the footer is cut from the
// right on a narrow pane, and "how do I leave this view" is the one binding
// with nowhere else to be discovered — it is in no help section, because it
// only exists while this form is open.
//
// The mode is NOT repeated here. The badge at the head of the same row says
// it, in both keymaps rather than only in vi.
//
// Divergence 4: these are the bindings that already exist. The handoff's
// proposed footer claimed ctrl+a, ctrl+d and ctrl+e, all of which emacs line
// editing owns, against its own promise that both keymaps keep working here.
func (m Model) expandedHint() string {
	parts := []string{"Enter: send", "Ctrl+P: inline"}
	// ctrl+j is inert in normal mode (see isNewlineChord), so it is not
	// advertised there; o/O are how a normal-mode user opens a line.
	if !m.IsViNormalMode() {
		parts = append(parts, "Ctrl+J: newline")
	}
	parts = append(parts, "Ctrl+T: attach", "Ctrl+O: $EDITOR", "Ctrl+V: paste")
	return strings.Join(parts, " · ")
}
