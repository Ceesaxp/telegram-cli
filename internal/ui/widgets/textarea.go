package widgets

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// TextArea is the shared text input widget. It runs in two shapes, selected
// by MultiLine:
//
//   - MultiLine false: a single-line input with a horizontally scrolling view
//     window (the search overlay, chatview's find bar, the auth prompts).
//   - MultiLine true: a multi-line editor (the composer). Value may contain
//     "\n"; every motion and edit below is line-aware, and the view scrolls
//     vertically to keep the cursor's line on screen.
//
// The shape is a declared property, not something inferred from Height.
// Height is a layout number: it arrives from a WindowSizeMsg, is zero before
// the first one, and can be squeezed to 1 on a short terminal. Deriving
// "is this a multi-line editor" from it meant the composer silently became a
// single-line widget in exactly those moments — and flattened the newlines out
// of anything pasted into it.
//
// Key handling is readline/emacs and is unconditional — callers that want vi
// semantics (the composer) intercept keys before delegating here and drive the
// exported edit primitives directly.
//
// Bindings are matched on tea.KeyPressMsg.Keystroke(), never on String().
// String() returns Key.Text whenever the terminal attached any, so a
// Kitty-protocol shift+enter (CSI 13;2;13u) reports String() == "\r" while
// Keystroke() correctly reports "shift+enter" — matching on String() would
// both miss the binding and insert a stray carriage return. See the keyPress
// doc comment in internal/app/keymap.go.
type TextArea struct {
	Value       string
	Cursor      int
	Width       int
	Height      int
	Focused     bool
	Placeholder string
	Style       lipgloss.Style

	// StylePlaceholder draws the prompt shown while the field is empty and
	// unfocused. Supplied by the caller like Style — this widget used to
	// reach for a hard-coded #565F89, which is how a generic input ended up
	// holding an opinion about the application's palette.
	StylePlaceholder lipgloss.Style
	// MultiLine makes Value a multi-line buffer: pastes keep their line
	// breaks, InsertNewline is meaningful, and View renders one row per
	// line inside a vertically scrolling window Height rows tall.
	MultiLine bool
	// EchoPassword, on the single-line path, renders a bullet per rune
	// instead of Value so a 2FA password is not visible on screen. The
	// cursor and placeholder (when empty) are unchanged.
	EchoPassword bool
}

func NewTextArea() TextArea {
	return TextArea{
		Height: 1,
	}
}

func (t *TextArea) Update(msg tea.Msg) (submitted bool) {
	if !t.Focused {
		return false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return t.handleKey(msg)

	case tea.PasteMsg:
		// Bracketed paste. Multi-line pastes keep their newlines in a
		// multi-line editor; a genuinely single-line input flattens them,
		// since a "\n" it can neither render nor navigate is worse than a
		// space.
		content := msg.Content
		if !t.MultiLine {
			content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", " "), "\n", " ")
		}
		t.InsertString(content)
	}
	return false
}

// handleKey applies one key press. It returns true when the key means
// "submit" (Enter), which the owning component turns into its own action.
func (t *TextArea) handleKey(msg tea.KeyPressMsg) bool {
	switch msg.Keystroke() {
	case "enter":
		return true

	case "backspace":
		t.Backspace()
	case "delete", "ctrl+d":
		t.DeleteChar()

	case "left", "ctrl+b":
		t.MoveLeft()
	case "right", "ctrl+f":
		t.MoveRight()
	case "up":
		t.MoveUp()
	case "down":
		t.MoveDown()
	case "home", "ctrl+a":
		t.MoveLineStart()
	case "end", "ctrl+e":
		t.MoveLineEnd()

	case "ctrl+u":
		t.KillToLineStart()
	case "ctrl+k":
		t.KillToLineEnd()
	case "ctrl+w":
		t.KillWordBack()

	default:
		t.InsertString(printableText(msg))
	}
	return false
}

// printableText returns the literal characters a key press should insert, or
// "" when it should insert nothing.
//
// A key carrying any modifier beyond shift/lock is never text: the Kitty
// protocol attaches composed text to alt-modified keys on macOS (Option+1 ->
// "¡") and even to shift+enter ("\r"), so Key.Text alone cannot be trusted.
// Control characters are filtered out for the same reason.
func printableText(msg tea.KeyPressMsg) string {
	if msg.Mod&^(tea.ModShift|tea.ModCapsLock|tea.ModNumLock|tea.ModScrollLock) != 0 {
		return ""
	}
	text := msg.Text
	if text == "" {
		// A real terminal always attaches Text to a printable key, but the
		// app synthesizes tea.KeyPressMsg values too (app.go forwards a
		// constructed key to redirect "/" into chatview's find), so fall
		// back to the key code. Bubble Tea's special keys (up, enter, F1…)
		// are encoded above unicode.MaxRune or as C0 controls, which is
		// exactly what the guard below rejects.
		if msg.Code > unicode.MaxRune || !unicode.IsPrint(msg.Code) {
			return ""
		}
		r := msg.Code
		if msg.Mod&tea.ModShift != 0 {
			r = unicode.ToUpper(r)
		}
		return string(r)
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return text
}

func (t *TextArea) Reset() {
	t.Value = ""
	t.Cursor = 0
}

// ---------------------------------------------------------------------------
// Edit primitives
//
// All offsets are rune indices into Value. Every method clamps, so callers can
// hand over an out-of-range cursor (a component may have assigned Value
// directly) without corrupting the buffer.
// ---------------------------------------------------------------------------

func (t *TextArea) runes() []rune {
	r := []rune(t.Value)
	t.Cursor = clampInt(t.Cursor, 0, len(r))
	return r
}

// Len returns the length of Value in runes.
func (t *TextArea) Len() int { return len([]rune(t.Value)) }

// InsertString inserts s at the cursor and leaves the cursor after it.
func (t *TextArea) InsertString(s string) {
	if s == "" {
		return
	}
	runes := t.runes()
	insert := []rune(s)
	out := make([]rune, 0, len(runes)+len(insert))
	out = append(out, runes[:t.Cursor]...)
	out = append(out, insert...)
	out = append(out, runes[t.Cursor:]...)
	t.Value = string(out)
	t.Cursor += len(insert)
}

// InsertNewline inserts a hard line break. It is deliberately separate from
// InsertString/key handling: Enter means "submit" to every component that
// embeds a TextArea, so a line break can only ever arrive through an explicit
// call (the composer's ctrl+j / shift+enter chords, or vi's o/O).
func (t *TextArea) InsertNewline() {
	t.InsertString("\n")
}

// DeleteRange removes the runes in [from, to) and puts the cursor at from.
func (t *TextArea) DeleteRange(from, to int) {
	runes := t.runes()
	from = clampInt(from, 0, len(runes))
	to = clampInt(to, 0, len(runes))
	if from >= to {
		return
	}
	t.Value = string(runes[:from]) + string(runes[to:])
	t.Cursor = from
}

// Backspace deletes the rune before the cursor, joining lines when the cursor
// sits at the start of one.
func (t *TextArea) Backspace() {
	if t.runes(); t.Cursor > 0 {
		t.DeleteRange(t.Cursor-1, t.Cursor)
	}
}

// DeleteChar deletes the rune under the cursor (emacs ctrl+d, vi x).
func (t *TextArea) DeleteChar() {
	runes := t.runes()
	if t.Cursor < len(runes) {
		t.DeleteRange(t.Cursor, t.Cursor+1)
	}
}

// LineBounds returns the [start, end) rune range of the line holding the
// cursor. end excludes the terminating "\n".
func (t *TextArea) LineBounds() (start, end int) {
	runes := t.runes()
	start = t.Cursor
	for start > 0 && runes[start-1] != '\n' {
		start--
	}
	end = t.Cursor
	for end < len(runes) && runes[end] != '\n' {
		end++
	}
	return start, end
}

func (t *TextArea) MoveLeft() {
	if t.runes(); t.Cursor > 0 {
		t.Cursor--
	}
}

func (t *TextArea) MoveRight() {
	if runes := t.runes(); t.Cursor < len(runes) {
		t.Cursor++
	}
}

func (t *TextArea) MoveLineStart() {
	start, _ := t.LineBounds()
	t.Cursor = start
}

func (t *TextArea) MoveLineEnd() {
	_, end := t.LineBounds()
	t.Cursor = end
}

// MoveUp moves to the previous line, keeping the column where possible. It is
// a no-op on the first line (and therefore in single-line mode).
func (t *TextArea) MoveUp() {
	runes := t.runes()
	start, _ := t.LineBounds()
	if start == 0 {
		return
	}
	col := t.Cursor - start
	prevEnd := start - 1 // the "\n" that ended the previous line
	prevStart := prevEnd
	for prevStart > 0 && runes[prevStart-1] != '\n' {
		prevStart--
	}
	t.Cursor = prevStart + minInt(col, prevEnd-prevStart)
}

// MoveDown moves to the next line, keeping the column where possible. It is a
// no-op on the last line.
func (t *TextArea) MoveDown() {
	runes := t.runes()
	start, end := t.LineBounds()
	if end >= len(runes) {
		return
	}
	col := t.Cursor - start
	nextStart := end + 1
	nextEnd := nextStart
	for nextEnd < len(runes) && runes[nextEnd] != '\n' {
		nextEnd++
	}
	t.Cursor = nextStart + minInt(col, nextEnd-nextStart)
}

// KillToLineEnd removes the rest of the current line (emacs ctrl+k, vi D).
// On an already-empty line end it swallows the line break, like readline.
func (t *TextArea) KillToLineEnd() {
	runes := t.runes()
	_, end := t.LineBounds()
	if end == t.Cursor && end < len(runes) {
		t.DeleteRange(t.Cursor, t.Cursor+1)
		return
	}
	t.DeleteRange(t.Cursor, end)
}

// KillToLineStart removes the current line up to the cursor (emacs ctrl+u).
func (t *TextArea) KillToLineStart() {
	start, _ := t.LineBounds()
	t.DeleteRange(start, t.Cursor)
}

// DeleteLine removes the whole line under the cursor including its line break
// (vi dd) and lands the cursor at the start of what is now the current line.
func (t *TextArea) DeleteLine() {
	runes := t.runes()
	start, end := t.LineBounds()
	switch {
	case end < len(runes): // consume the trailing "\n"
		end++
	case start > 0: // last line: consume the leading "\n" instead
		start--
	}
	t.DeleteRange(start, end)
	t.MoveLineStart()
}

// WordBackStart returns the offset of the start of the word before the
// cursor: skip separators, then skip the word itself.
func (t *TextArea) WordBackStart() int {
	runes := t.runes()
	i := t.Cursor
	for i > 0 && isWordSep(runes[i-1]) {
		i--
	}
	for i > 0 && !isWordSep(runes[i-1]) {
		i--
	}
	return i
}

// WordForwardStart returns the offset of the start of the next word: skip the
// current word, then skip separators.
func (t *TextArea) WordForwardStart() int {
	runes := t.runes()
	i := t.Cursor
	for i < len(runes) && !isWordSep(runes[i]) {
		i++
	}
	for i < len(runes) && isWordSep(runes[i]) {
		i++
	}
	return i
}

// MoveWordBack moves to the start of the previous word (vi b).
func (t *TextArea) MoveWordBack() { t.Cursor = t.WordBackStart() }

// MoveWordForward moves to the start of the next word (vi w).
func (t *TextArea) MoveWordForward() { t.Cursor = t.WordForwardStart() }

// KillWordBack deletes from the start of the previous word to the cursor
// (emacs ctrl+w).
func (t *TextArea) KillWordBack() { t.DeleteRange(t.WordBackStart(), t.Cursor) }

// isWordSep treats whitespace (including the line break) as the word
// boundary. Punctuation stays part of the word, which is what a chat message
// wants: ctrl+w after a URL removes the URL.
func isWordSep(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n'
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func (t *TextArea) View() string {
	if t.Value == "" && t.Placeholder != "" && !t.Focused {
		return t.Style.Width(t.Width).Render(t.StylePlaceholder.Render(t.Placeholder))
	}

	runes := []rune(t.Value)
	cursor := clampInt(t.Cursor, 0, len(runes))

	// Multi-line mode (the composer). Value may hold hard line breaks; each
	// is rendered as its own row and the visible window scrolls vertically
	// so the cursor's line stays on screen. lipgloss still wraps any single
	// line that is wider than Width across extra rows, as before.
	if t.MultiLine {
		return t.Style.Width(t.Width).Render(t.multiLineContent(runes, cursor))
	}

	// Single-line mode: keep the rendered line within the style's actual
	// content width (Width minus horizontal padding) at all times,
	// reserving one column for the cursor glyph when focused, and
	// horizontally scrolling the visible window so the cursor stays on
	// screen. Without this, once Value (plus the appended cursor glyph)
	// reached the style's wrap width, lipgloss's internal ansi.Wrap would
	// hard-wrap the overflowing line onto a second row — including, for a
	// bordered style, the border characters themselves — which is what
	// produced the broken-border / stray-block rendering artifacts around
	// the cursor.
	avail := t.Width - t.Style.GetHorizontalPadding()
	if avail < 1 {
		avail = 1
	}
	visible := avail
	if t.Focused {
		visible--
		if visible < 1 {
			visible = 1
		}
	}

	display := runes
	if t.EchoPassword {
		masked := make([]rune, len(runes))
		for i := range runes {
			masked[i] = '•'
		}
		display = masked
	}
	start, end := textWindow(len(display), cursor, visible)
	content := string(display[start:end])
	if t.Focused {
		content = string(display[start:cursor]) + "█" + string(display[cursor:end])
	}

	return t.Style.Width(t.Width).Render(content)
}

// multiLineContent splits Value into rows, places the cursor glyph, and
// returns only the rows inside the vertical window.
func (t *TextArea) multiLineContent(runes []rune, cursor int) string {
	content := t.Value
	if t.Focused {
		content = string(runes[:cursor]) + "█" + string(runes[cursor:])
	}
	// Height is a layout number and may not have arrived yet; one row is the
	// smallest window that can still show the cursor.
	height := t.Height
	if height < 1 {
		height = 1
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= height {
		return content
	}

	// Which row holds the cursor? Count line breaks before it.
	cursorLine := 0
	for _, r := range runes[:cursor] {
		if r == '\n' {
			cursorLine++
		}
	}
	start := cursorLine - height + 1
	if start < 0 {
		start = 0
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	return strings.Join(lines[start:start+height], "\n")
}

// clampInt clamps v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// textWindow returns a [start, end) rune range of at most width runes out of
// n total runes that contains cursor. Once the text overflows width, the
// window scrolls so the cursor sits at the trailing edge — the usual
// single-line text-input behavior.
func textWindow(n, cursor, width int) (start, end int) {
	if width <= 0 {
		return cursor, cursor
	}
	if n <= width {
		return 0, n
	}
	start = cursor - width + 1
	if start < 0 {
		start = 0
	}
	end = start + width
	if end > n {
		end = n
		start = end - width
	}
	return start, end
}
