package widgets

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// decodeKey runs a raw terminal byte sequence through the ultraviolet event
// decoder, the same decoder bubbletea v2's input loop runs, and returns it as
// the message Update() would receive. Hand-built tea.KeyPressMsg values cannot
// catch the case this widget exists to survive: String() returns Key.Text
// whenever the terminal attached any, so a Kitty-protocol shift+enter reports
// String() == "\r".
func decodeKey(t *testing.T, seq string) tea.KeyPressMsg {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(seq))
	if n != len(seq) {
		t.Fatalf("decode(%q) consumed %d of %d bytes", seq, n, len(seq))
	}
	kp, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("decode(%q) produced %T, want uv.KeyPressEvent", seq, ev)
	}
	return tea.KeyPressMsg(kp)
}

func press(t *testing.T, ta *TextArea, seqs ...string) (submitted bool) {
	t.Helper()
	for _, seq := range seqs {
		if ta.Update(decodeKey(t, seq)) {
			submitted = true
		}
	}
	return submitted
}

// singleLine is the shape the search overlay, chatview's find bar and the auth
// prompts use.
func singleLine() *TextArea {
	ta := NewTextArea()
	ta.Focused = true
	ta.Width = 20
	return &ta
}

func TestSingleLineTypingAndSubmit(t *testing.T) {
	ta := singleLine()
	if press(t, ta, "h", "e", "l", "l", "o", " ", "y", "o", "u") {
		t.Fatal("typing reported a submit")
	}
	if ta.Value != "hello you" {
		t.Fatalf("Value = %q, want %q", ta.Value, "hello you")
	}
	if !press(t, ta, "\r") {
		t.Error("Enter did not report a submit")
	}
	if ta.Value != "hello you" {
		t.Errorf("Enter changed Value to %q", ta.Value)
	}
}

// A single-line input has no second line to move to, so the multi-line motions
// are inert there — the search overlay keeps its own up/down list navigation.
func TestSingleLineVerticalMotionIsInert(t *testing.T) {
	ta := singleLine()
	press(t, ta, "a", "b", "c")
	ta.Cursor = 1
	press(t, ta, "\x1b[A", "\x1b[B") // up, down
	if ta.Cursor != 1 || ta.Value != "abc" {
		t.Errorf("vertical motion changed state: Value=%q Cursor=%d", ta.Value, ta.Cursor)
	}
}

// A multi-line paste into a single-line input is flattened: a "\n" it can
// neither render nor navigate is worse than a space.
func TestSingleLinePasteFlattensNewlines(t *testing.T) {
	ta := singleLine()
	ta.Update(tea.PasteMsg{Content: "one\ntwo\r\nthree"})
	if ta.Value != "one two three" {
		t.Errorf("Value = %q, want %q", ta.Value, "one two three")
	}
}

// The multi-line shape (the composer) keeps the breaks.
func TestMultiLinePasteKeepsNewlines(t *testing.T) {
	ta := NewTextArea()
	ta.Focused = true
	ta.Width = 20
	ta.Height = 4
	ta.MultiLine = true
	ta.Update(tea.PasteMsg{Content: "one\ntwo"})
	if ta.Value != "one\ntwo" {
		t.Errorf("Value = %q, want %q", ta.Value, "one\ntwo")
	}
}

// Enter never inserts a line break; only InsertNewline does. Every component
// embedding a TextArea binds Enter to its own submit action, so a widget that
// swallowed Enter as text would break all of them at once.
func TestEnterNeverInsertsText(t *testing.T) {
	area := NewTextArea()
	area.Focused = true
	area.Height = 4
	ta := &area
	for _, seq := range []string{
		"\r",            // legacy enter
		"\x1b[13u",      // kitty enter
		"\x1b[13;2u",    // kitty shift+enter
		"\x1b[13;2;13u", // ...with the associated "\r" that String() reports
		"\x1b[27;2;13~", // modifyOtherKeys shift+enter
		"\n",            // ctrl+j
		"\x1b[106;5u",   // kitty ctrl+j
		"\x1b\r",        // alt+enter
		"\t",            // tab
	} {
		ta.Value, ta.Cursor = "", 0
		press(t, ta, seq)
		if ta.Value != "" {
			t.Errorf("%q inserted %q", seq, ta.Value)
		}
	}
	ta.InsertNewline()
	if ta.Value != "\n" {
		t.Errorf("InsertNewline produced %q, want %q", ta.Value, "\n")
	}
}

// A modified key press is never text, even when the terminal attached some:
// the Kitty protocol sends the macOS Option-composed character along with the
// modifier bit (Option+1 -> "¡"), and String() reports the character.
func TestModifiedKeysAreNotText(t *testing.T) {
	ta := singleLine()
	for _, seq := range []string{
		"\x1b[49;3;161u", // alt+1 with text "¡"
		"\x1b[99;3;231u", // alt+c with text "ç"
		"\x1b1",          // legacy alt+1
		"\x0f",           // ctrl+o
		"\x14",           // ctrl+t
	} {
		ta.Value, ta.Cursor = "", 0
		press(t, ta, seq)
		if ta.Value != "" {
			t.Errorf("%q inserted %q", seq, ta.Value)
		}
	}

	// A *shifted* key is still text, though — that is the whole distinction.
	ta.Value, ta.Cursor = "", 0
	press(t, ta, "\x1b[47:63;2u") // kitty shift+/ -> "?"
	if ta.Value != "?" {
		t.Errorf("shift+/ inserted %q, want %q", ta.Value, "?")
	}
}

func TestEditPrimitives(t *testing.T) {
	area := NewTextArea()
	area.Height = 4
	ta := &area
	ta.Value = "alpha beta\ngamma delta"

	// LineBounds is line-local.
	ta.Cursor = 14
	if start, end := ta.LineBounds(); start != 11 || end != 22 {
		t.Errorf("LineBounds = (%d, %d), want (11, 22)", start, end)
	}

	// Word motion treats whitespace, including the break, as the boundary.
	ta.Cursor = 0
	ta.MoveWordForward()
	if ta.Cursor != 6 {
		t.Errorf("MoveWordForward -> %d, want 6", ta.Cursor)
	}
	ta.MoveWordForward()
	if ta.Cursor != 11 {
		t.Errorf("MoveWordForward across the break -> %d, want 11", ta.Cursor)
	}
	ta.MoveWordBack()
	if ta.Cursor != 6 {
		t.Errorf("MoveWordBack -> %d, want 6", ta.Cursor)
	}

	// DeleteLine takes the break with it.
	ta.Cursor = 3
	ta.DeleteLine()
	if ta.Value != "gamma delta" || ta.Cursor != 0 {
		t.Errorf("DeleteLine -> %q cursor %d, want %q cursor 0",
			ta.Value, ta.Cursor, "gamma delta")
	}

	// DeleteLine on the only line empties the buffer.
	ta.DeleteLine()
	if ta.Value != "" || ta.Cursor != 0 {
		t.Errorf("DeleteLine on the last line -> %q cursor %d", ta.Value, ta.Cursor)
	}
}

// Every primitive clamps, so a component that assigned Value directly and left
// a stale Cursor behind cannot corrupt the buffer.
func TestPrimitivesClampStaleCursor(t *testing.T) {
	area := NewTextArea()
	area.Height = 4
	ta := &area
	ta.Value = "ab"
	ta.Cursor = 99

	ta.InsertString("!")
	if ta.Value != "ab!" {
		t.Errorf("InsertString with a stale cursor -> %q", ta.Value)
	}
	ta.Cursor = -5
	ta.DeleteChar()
	if ta.Value != "b!" {
		t.Errorf("DeleteChar with a negative cursor -> %q", ta.Value)
	}
	ta.Cursor = 99
	ta.DeleteRange(-3, 99)
	if ta.Value != "" {
		t.Errorf("DeleteRange clamp -> %q", ta.Value)
	}
}

// The multi-line view scrolls vertically so the cursor's line stays on screen,
// instead of rendering rows the composer has no room for.
func TestMultiLineViewScrollsToCursor(t *testing.T) {
	ta := NewTextArea()
	ta.Focused = true
	ta.Width = 20
	ta.Height = 2
	ta.MultiLine = true
	ta.Value = "one\ntwo\nthree\nfour"
	ta.Cursor = ta.Len() // on "four"

	view := ta.View()
	if !strings.Contains(view, "three") || !strings.Contains(view, "four") {
		t.Errorf("cursor's line not in the window:\n%s", view)
	}
	if strings.Contains(view, "one") {
		t.Errorf("scrolled-off line still rendered:\n%s", view)
	}

	// Move to the top and the window follows.
	ta.Cursor = 0
	view = ta.View()
	if !strings.Contains(view, "one") || strings.Contains(view, "four") {
		t.Errorf("window did not follow the cursor up:\n%s", view)
	}
}

// The single-line view keeps the cursor inside the style's content width, so a
// bordered input never hard-wraps its own border onto a second row.
func TestSingleLineViewWindow(t *testing.T) {
	ta := singleLine()
	ta.Width = 10
	ta.Value = strings.Repeat("x", 40)
	ta.Cursor = ta.Len()
	if lines := strings.Count(ta.View(), "\n"); lines != 0 {
		t.Errorf("single-line view wrapped onto %d extra rows:\n%s", lines, ta.View())
	}
}

// MultiLine is a declared property, not one inferred from Height: Height is a
// layout number that is zero before the first WindowSizeMsg and can be
// squeezed to 1 on a short terminal. Inferring the shape from it meant a
// multi-line editor silently became a single-line widget in exactly those
// moments — and flattened the newlines out of anything pasted into it.
func TestMultiLineIsIndependentOfHeight(t *testing.T) {
	for _, height := range []int{0, 1, 2, 8} {
		ta := NewTextArea()
		ta.Focused = true
		ta.Width = 20
		ta.MultiLine = true
		ta.Height = height

		ta.Update(tea.PasteMsg{Content: "one\ntwo"})
		if ta.Value != "one\ntwo" {
			t.Errorf("height %d flattened the paste to %q", height, ta.Value)
		}
		// A zero or negative window still renders the cursor's line rather
		// than nothing at all.
		ta.Cursor = ta.Len()
		if view := ta.View(); !strings.Contains(view, "two") {
			t.Errorf("height %d rendered no cursor line:\n%s", height, view)
		}
	}

	// The converse: a single-line widget stays single-line however tall the
	// layout makes it. Nothing does this today, but the flag is what decides
	// — not the number.
	ta := NewTextArea()
	ta.Focused = true
	ta.Width = 20
	ta.Height = 8
	ta.Update(tea.PasteMsg{Content: "one\ntwo"})
	if ta.Value != "one two" {
		t.Errorf("single-line widget kept newlines at height 8: %q", ta.Value)
	}
}
