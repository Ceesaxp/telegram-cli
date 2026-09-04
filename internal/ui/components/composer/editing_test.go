package composer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	uv "github.com/charmbracelet/ultraviolet"
)

// These tests drive the *real* terminal input decoder rather than hand-built
// tea.KeyPressMsg values, for the reason spelled out in
// internal/app/keys_test.go: tea.KeyPressMsg.String() returns Key.Text
// whenever the terminal attached any, so a synthetic message cannot tell you
// whether a binding survives a real key press. The composer's newline chord
// is exactly such a case — a Kitty-protocol shift+enter arrives as
// CSI 13;2;13u, whose String() is "\r" and whose Keystroke() is
// "shift+enter".
//
// bubbletea v2's input loop is a thin adapter over uv.EventDecoder
// (charm.land/bubbletea/v2/input.go), so decoding here reproduces exactly what
// Update() sees.

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

// send feeds one raw terminal byte sequence to the composer.
func send(t *testing.T, m Model, seq string) (Model, tea.Msg) {
	t.Helper()
	m, cmd := m.Update(decodeKey(t, seq))
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

// typeSeq feeds a run of sequences, discarding the messages.
func typeSeq(t *testing.T, m Model, seqs ...string) Model {
	t.Helper()
	for _, seq := range seqs {
		m, _ = send(t, m, seq)
	}
	return m
}

// chars feeds each byte of s as its own key press, the way a user typing it
// would arrive.
func chars(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = typeSeq(t, m, string(r))
	}
	return m
}

func viComposer(t *testing.T) Model {
	t.Helper()
	m := newFocused()
	m.SetEditingMode(ModeVi)
	return m
}

// ---------------------------------------------------------------------------
// Decoder findings
// ---------------------------------------------------------------------------

// TestDecoderNewlineAndEditingChords records what the decoder actually
// produces for every key this package binds. It is the empirical basis for
// isNewlineChord and handleKey, and the reason both match on Keystroke().
//
// The two rows that matter most:
//
//   - 0x0A and 0x0D decode *distinctly* ("ctrl+j" vs "enter"), so ctrl+j is a
//     newline chord that works in every terminal, legacy encodings included.
//   - Kitty may attach text to shift+enter, and then String() is "\r" while
//     Keystroke() is still "shift+enter". Matching on String() would insert a
//     carriage return instead of a line break.
func TestDecoderNewlineAndEditingChords(t *testing.T) {
	cases := []struct {
		name          string
		seq           string
		wantString    string
		wantKeystroke string
	}{
		// Submit vs newline, legacy control bytes. Distinct — this is the
		// finding ctrl+j rests on.
		{"legacy 0x0D", "\r", "enter", "enter"},
		{"legacy 0x0A", "\n", "ctrl+j", "ctrl+j"},

		// Kitty CSI-u.
		{"kitty enter", "\x1b[13u", "enter", "enter"},
		{"kitty shift+enter", "\x1b[13;2u", "shift+enter", "shift+enter"},
		{"kitty ctrl+enter", "\x1b[13;5u", "ctrl+enter", "ctrl+enter"},
		{"kitty ctrl+j", "\x1b[106;5u", "ctrl+j", "ctrl+j"},
		// ...with associated text: String() is the carriage return, only
		// Keystroke() names the chord.
		{"kitty shift+enter + text", "\x1b[13;2;13u", "\r", "shift+enter"},

		// XTerm modifyOtherKeys.
		{"modifyOtherKeys shift+enter", "\x1b[27;2;13~", "shift+enter", "shift+enter"},

		// alt+enter decodes cleanly in both encodings; it is still not the
		// primary chord (see isNewlineChord).
		{"kitty alt+enter", "\x1b[13;3u", "alt+enter", "alt+enter"},
		{"legacy alt+enter", "\x1b\r", "alt+enter", "alt+enter"},

		// External editor.
		{"ctrl+o legacy", "\x0f", "ctrl+o", "ctrl+o"},
		{"ctrl+o kitty", "\x1b[111;5u", "ctrl+o", "ctrl+o"},

		// Emacs chords: unambiguous in both encodings, no associated text.
		{"ctrl+a", "\x01", "ctrl+a", "ctrl+a"},
		{"ctrl+b", "\x02", "ctrl+b", "ctrl+b"},
		{"ctrl+d", "\x04", "ctrl+d", "ctrl+d"},
		{"ctrl+e", "\x05", "ctrl+e", "ctrl+e"},
		{"ctrl+f", "\x06", "ctrl+f", "ctrl+f"},
		{"ctrl+k", "\x0b", "ctrl+k", "ctrl+k"},
		{"ctrl+u", "\x15", "ctrl+u", "ctrl+u"},
		{"ctrl+w", "\x17", "ctrl+w", "ctrl+w"},
		{"ctrl+a kitty", "\x1b[97;5u", "ctrl+a", "ctrl+a"},
		{"ctrl+w kitty", "\x1b[119;5u", "ctrl+w", "ctrl+w"},

		// Vi normal-mode commands are unmodified printables, where the
		// spellings diverge the other way: Keystroke() names the physical
		// chord, String() the character. Hence handleViNormal uses String().
		{"plain $", "$", "$", "$"},
		{"kitty shift+4 (=$)", "\x1b[52:36;2u", "$", "shift+4"},
		{"plain D", "D", "D", "shift+d"},
		{"kitty shift+d", "\x1b[100:68;2u", "D", "shift+d"},

		{"esc", "\x1b", "esc", "esc"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := decodeKey(t, tc.seq)
			if got := msg.String(); got != tc.wantString {
				t.Errorf("String() = %q, want %q", got, tc.wantString)
			}
			if got := msg.Keystroke(); got != tc.wantKeystroke {
				t.Errorf("Keystroke() = %q, want %q", got, tc.wantKeystroke)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Task 1 — newline insertion
// ---------------------------------------------------------------------------

func TestNewlineChordsInsertNewline(t *testing.T) {
	cases := []struct{ name, seq string }{
		{"ctrl+j (legacy 0x0A)", "\n"},
		{"ctrl+j (kitty)", "\x1b[106;5u"},
		{"shift+enter (kitty)", "\x1b[13;2u"},
		{"shift+enter (kitty, with text)", "\x1b[13;2;13u"},
		{"shift+enter (modifyOtherKeys)", "\x1b[27;2;13~"},
		{"ctrl+enter (kitty)", "\x1b[13;5u"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := chars(t, newFocused(), "ab")
			m, msg := send(t, m, tc.seq)
			if msg != nil {
				t.Fatalf("newline chord produced %T, want no message", msg)
			}
			m = chars(t, m, "cd")
			if m.textarea.Value != "ab\ncd" {
				t.Errorf("Value = %q, want %q", m.textarea.Value, "ab\ncd")
			}
		})
	}
}

// Enter still submits, in every encoding — the newline chords must not have
// stolen it.
func TestEnterStillSubmits(t *testing.T) {
	for _, seq := range []string{"\r", "\x1b[13u"} {
		t.Run(seq, func(t *testing.T) {
			m := chars(t, newFocused(), "hi")
			_, msg := send(t, m, seq)
			submitted, ok := msg.(MessageSubmittedMsg)
			if !ok {
				t.Fatalf("got %T, want MessageSubmittedMsg", msg)
			}
			if submitted.Text != "hi" {
				t.Errorf("Text = %q, want %q", submitted.Text, "hi")
			}
		})
	}
}

// A multi-line draft is submitted with its line breaks intact.
func TestSubmitCarriesNewlines(t *testing.T) {
	m := chars(t, newFocused(), "one")
	m = typeSeq(t, m, "\n")
	m = chars(t, m, "two")
	_, msg := send(t, m, "\r")
	submitted, ok := msg.(MessageSubmittedMsg)
	if !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}
	if submitted.Text != "one\ntwo" {
		t.Errorf("Text = %q, want %q", submitted.Text, "one\ntwo")
	}
}

// The multi-line editor has to actually be an editor: up/down move between
// lines keeping the column, home/end are line-local, and backspace at the
// start of a line joins it to the one above.
func TestMultiLineCursorMovement(t *testing.T) {
	m := newFocused()
	m.textarea.Value = "hello\nworld"
	m.textarea.Cursor = 8 // "wo|rld"

	m.textarea.MoveUp()
	if m.textarea.Cursor != 2 { // "he|llo", same column
		t.Errorf("after MoveUp Cursor = %d, want 2", m.textarea.Cursor)
	}
	m.textarea.MoveDown()
	if m.textarea.Cursor != 8 {
		t.Errorf("after MoveDown Cursor = %d, want 8", m.textarea.Cursor)
	}
	m.textarea.MoveLineStart()
	if m.textarea.Cursor != 6 {
		t.Errorf("after MoveLineStart Cursor = %d, want 6", m.textarea.Cursor)
	}
	m.textarea.MoveLineEnd()
	if m.textarea.Cursor != 11 {
		t.Errorf("after MoveLineEnd Cursor = %d, want 11", m.textarea.Cursor)
	}

	// MoveUp on the first line and MoveDown on the last are no-ops.
	m.textarea.Cursor = 2
	m.textarea.MoveUp()
	if m.textarea.Cursor != 2 {
		t.Errorf("MoveUp on the first line moved to %d", m.textarea.Cursor)
	}
	m.textarea.Cursor = 11
	m.textarea.MoveDown()
	if m.textarea.Cursor != 11 {
		t.Errorf("MoveDown on the last line moved to %d", m.textarea.Cursor)
	}

	// A shorter previous line clamps the column instead of overshooting.
	m.textarea.Value = "ab\nlongline"
	m.textarea.Cursor = 10 // "longlin|e"
	m.textarea.MoveUp()
	if m.textarea.Cursor != 2 { // end of "ab"
		t.Errorf("MoveUp column clamp put the cursor at %d, want 2", m.textarea.Cursor)
	}

	// Backspace across the line boundary joins the lines.
	m.textarea.Value = "ab\ncd"
	m.textarea.Cursor = 3
	m.textarea.Backspace()
	if m.textarea.Value != "abcd" || m.textarea.Cursor != 2 {
		t.Errorf("Backspace joined to %q cursor %d, want %q cursor 2",
			m.textarea.Value, m.textarea.Cursor, "abcd")
	}
}

// Arrow keys must move between lines through the normal key path too.
func TestArrowKeysMoveBetweenLines(t *testing.T) {
	m := chars(t, newFocused(), "one")
	m = typeSeq(t, m, "\n")
	m = chars(t, m, "two")
	m = typeSeq(t, m, "\x1b[A") // up
	m = chars(t, m, "!")
	if m.textarea.Value != "one!\ntwo" {
		t.Errorf("Value = %q, want %q", m.textarea.Value, "one!\ntwo")
	}
}

// The rendered composer shows both lines of a two-line draft.
//
// The inline row is one row and shows the cursor's line, with the prompt
// turning into a chevron so a multi-line draft cannot look like a one-line
// one that lost its tail. Seeing all of it is what the expanded form is for,
// so that is where this guarantee now lives.
func TestMultiLineRenders(t *testing.T) {
	m := newFocused()
	m.SetSize(60, 5)
	m.textarea.Value = "first line\nsecond line"
	m.textarea.Cursor = m.textarea.Len()

	if view := m.View(); !strings.Contains(view, "second line") {
		t.Errorf("cursor's line missing from the inline composer:\n%s", view)
	}

	m.SetExpanded(true)
	view := m.View()
	if !strings.Contains(view, "first line") || !strings.Contains(view, "second line") {
		t.Errorf("multi-line draft missing from the expanded view:\n%s", view)
	}
}

// The composer has to name the newline chord — with Enter bound to send, "how
// do I write a second line" is the first question it must answer.
//
// The inline row is one row and spends it on the draft, so the chord list
// moved to the expanded form's footer. Divergence 4: that footer advertises
// the bindings that already exist, because the handoff's proposed four are
// all claimed by emacs line editing.
func TestHintMentionsNewlineChord(t *testing.T) {
	m := newFocused()
	m.SetExpanded(true)
	if view := m.View(); !strings.Contains(view, "Ctrl+J") {
		t.Errorf("expanded footer does not mention the newline chord:\n%s", view)
	}
}

// And it advertises the inline/expanded toggle, which is otherwise the one
// binding with nowhere to be discovered.
func TestExpandedFooterAdvertisesTheToggle(t *testing.T) {
	m := newFocused()
	m.SetExpanded(true)
	if view := m.View(); !strings.Contains(view, "Ctrl+P") {
		t.Errorf("expanded footer does not mention Ctrl+P:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Task 2 — emacs mode
// ---------------------------------------------------------------------------

func TestEmacsIsTheDefault(t *testing.T) {
	if got := New(theme.DarkRoles(false)).EditingMode(); got != ModeEmacs {
		t.Errorf("default EditingMode = %v, want ModeEmacs", got)
	}
}

func TestEditingModeFor(t *testing.T) {
	cases := map[string]EditingMode{
		"vi": ModeVi, "VI": ModeVi, " vim ": ModeVi, "nvim": ModeVi,
		"emacs": ModeEmacs, "": ModeEmacs, "nonsense": ModeEmacs,
	}
	for name, want := range cases {
		if got := EditingModeFor(name); got != want {
			t.Errorf("EditingModeFor(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestEmacsChords(t *testing.T) {
	cases := []struct {
		name       string
		value      string
		cursor     int
		seq        string
		wantValue  string
		wantCursor int
	}{
		{"ctrl+a line start", "hello world", 6, "\x01", "hello world", 0},
		{"ctrl+e line end", "hello world", 3, "\x05", "hello world", 11},
		{"ctrl+b back one", "hello", 3, "\x02", "hello", 2},
		{"ctrl+f forward one", "hello", 3, "\x06", "hello", 4},
		{"ctrl+b at start", "hello", 0, "\x02", "hello", 0},
		{"ctrl+f at end", "hello", 5, "\x06", "hello", 5},
		{"ctrl+k kill to end", "hello world", 6, "\x0b", "hello ", 6},
		{"ctrl+u kill to start", "hello world", 6, "\x15", "world", 0},
		{"ctrl+w kill word back", "hello world", 11, "\x17", "hello ", 6},
		{"ctrl+w through spaces", "hello world   ", 14, "\x17", "hello ", 6},
		{"ctrl+d delete forward", "hello", 1, "\x04", "hllo", 1},
		{"ctrl+d at end", "hello", 5, "\x04", "hello", 5},

		// Line-local, not buffer-local, once the draft is multi-line.
		{"ctrl+a is line start", "one\ntwo", 6, "\x01", "one\ntwo", 4},
		{"ctrl+e is line end", "one\ntwo", 5, "\x05", "one\ntwo", 7},
		{"ctrl+k stops at the break", "one\ntwo", 1, "\x0b", "o\ntwo", 1},
		{"ctrl+u stops at the break", "one\ntwo", 6, "\x15", "one\no", 4},
		{"ctrl+w stops at the break", "one\ntwo", 7, "\x17", "one\n", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newFocused()
			m.textarea.Value = tc.value
			m.textarea.Cursor = tc.cursor
			m, msg := send(t, m, tc.seq)
			if msg != nil {
				t.Fatalf("%s produced %T, want no message", tc.name, msg)
			}
			if m.textarea.Value != tc.wantValue {
				t.Errorf("Value = %q, want %q", m.textarea.Value, tc.wantValue)
			}
			if m.textarea.Cursor != tc.wantCursor {
				t.Errorf("Cursor = %d, want %d", m.textarea.Cursor, tc.wantCursor)
			}
		})
	}
}

// The emacs chords stay live in vi's *insert* mode too: they are the readline
// keys every shell has, and vi insert mode has always kept them.
func TestEmacsChordsWorkInViInsertMode(t *testing.T) {
	m := viComposer(t)
	m.textarea.Value = "hello world"
	m.textarea.Cursor = 11
	m, _ = send(t, m, "\x17") // ctrl+w
	if m.textarea.Value != "hello " {
		t.Errorf("Value = %q, want %q", m.textarea.Value, "hello ")
	}
}

// ---------------------------------------------------------------------------
// Task 2 — vi mode
// ---------------------------------------------------------------------------

func TestViEscEntersNormalMode(t *testing.T) {
	m := chars(t, viComposer(t), "hello")
	if m.IsViNormalMode() {
		t.Fatal("a fresh vi composer starts in normal mode, want insert")
	}

	m, msg := send(t, m, "\x1b")
	if msg != nil {
		t.Fatalf("first Esc produced %T, want no message", msg)
	}
	if !m.IsViNormalMode() {
		t.Fatal("first Esc did not enter normal mode")
	}
	// Text is untouched by the mode switch.
	if m.textarea.Value != "hello" {
		t.Errorf("Value = %q, want %q", m.textarea.Value, "hello")
	}
	// Entering normal mode moves the cursor onto the last character rather
	// than leaving it in the gap after it — vi's normal-mode convention.
	// See viClampCursor.
	if m.textarea.Cursor != 4 {
		t.Errorf("Cursor = %d after Esc, want 4 (on the last char)", m.textarea.Cursor)
	}

	// And normal-mode keys no longer type.
	m = chars(t, m, "i")
	if m.textarea.Value != "hello" {
		t.Errorf("\"i\" leaked into the buffer: %q", m.textarea.Value)
	}
	if m.IsViNormalMode() {
		t.Error("\"i\" did not return to insert mode")
	}
	// "i" inserts *before* the character the cursor sits on, so this lands
	// between the second l and the o.
	m = chars(t, m, "X")
	if m.textarea.Value != "hellXo" {
		t.Errorf("Value = %q, want %q", m.textarea.Value, "hellXo")
	}
}

// The counterpart to the above: "a" appends after the character the cursor
// sits on, so from the end of a line it appends to the line.
func TestViAppendAtLineEnd(t *testing.T) {
	m := chars(t, viComposer(t), "hello")
	m, _ = send(t, m, "\x1b")
	m = chars(t, m, "a")
	m = chars(t, m, "X")
	if m.textarea.Value != "helloX" {
		t.Errorf("Value = %q, want %q", m.textarea.Value, "helloX")
	}
}

// The Escape/cancel invariants are preserved, they just cost one extra Escape
// in vi mode: the first leaves insert mode, the second runs the existing
// cancel path — attachment discard and all.
func TestViEscEscDiscardsAttachment(t *testing.T) {
	m := viComposer(t)
	m.SetAttachment("/tmp/paste-1.png", true)

	m, msg := send(t, m, "\x1b")
	if msg != nil {
		t.Fatalf("first Esc produced %T, want no message", msg)
	}
	if m.Attachment() != "/tmp/paste-1.png" {
		t.Fatalf("first Esc discarded the attachment (%q), want it kept", m.Attachment())
	}
	if !m.IsViNormalMode() {
		t.Fatal("first Esc did not enter normal mode")
	}

	m, msg = send(t, m, "\x1b")
	discarded, ok := msg.(AttachmentDiscardedMsg)
	if !ok {
		t.Fatalf("second Esc produced %T, want AttachmentDiscardedMsg", msg)
	}
	if discarded.Path != "/tmp/paste-1.png" {
		t.Errorf("Path = %q, want /tmp/paste-1.png", discarded.Path)
	}
	if m.Attachment() != "" {
		t.Errorf("Attachment = %q, want it cleared", m.Attachment())
	}
	// Cancelling does not drop the user back into insert mode.
	if !m.IsViNormalMode() {
		t.Error("cancel left normal mode")
	}
}

func TestViEscEscExitsReplyMode(t *testing.T) {
	m := viComposer(t)
	m.EnterReplyMode(7, "hello")

	m, _ = send(t, m, "\x1b")
	if !m.IsComposing() {
		t.Fatal("first Esc already cleared reply mode, want it kept")
	}
	m, msg := send(t, m, "\x1b")
	if msg != nil {
		t.Fatalf("second Esc produced %T, want no message", msg)
	}
	if m.IsComposing() {
		t.Error("still composing after Esc Esc, want reply mode cleared")
	}
}

// app.go consults IsComposing before forwarding Escape: it has to report true
// while vi is in insert mode, or the first Escape moves the panel focus
// instead of reaching handleEsc. In normal mode with nothing pending it must
// go back to reporting false, so the second Escape leaves the panel as before.
func TestViIsComposingGatesEscape(t *testing.T) {
	m := viComposer(t)
	if !m.IsComposing() {
		t.Error("IsComposing = false in vi insert mode; app.go would steal Esc")
	}
	m, _ = send(t, m, "\x1b")
	if m.IsComposing() {
		t.Error("IsComposing = true in vi normal mode with nothing pending")
	}

	// Emacs mode is unchanged.
	if newFocused().IsComposing() {
		t.Error("IsComposing = true for an idle emacs composer")
	}
}

// Enter sends from normal mode too — submit is never modal.
func TestViEnterSubmitsFromNormalMode(t *testing.T) {
	m := chars(t, viComposer(t), "hi")
	m, _ = send(t, m, "\x1b")
	_, msg := send(t, m, "\r")
	submitted, ok := msg.(MessageSubmittedMsg)
	if !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}
	if submitted.Text != "hi" {
		t.Errorf("Text = %q, want %q", submitted.Text, "hi")
	}
}

// After a send the composer is ready to be typed into again.
func TestViSubmitReturnsToInsertMode(t *testing.T) {
	m := chars(t, viComposer(t), "hi")
	m, _ = send(t, m, "\x1b")
	m, _ = send(t, m, "\r")
	if m.IsViNormalMode() {
		t.Error("still in normal mode after a send, want insert")
	}
}

// TestViNormalModeCommands is the command table. Each case starts in insert
// mode, presses Escape, then runs the listed normal-mode keys.
func TestViNormalModeCommands(t *testing.T) {
	cases := []struct {
		name       string
		value      string
		cursor     int
		keys       string
		wantValue  string
		wantCursor int
		wantInsert bool
	}{
		// Motions.
		{"h moves left", "hello", 3, "h", "hello", 2, false},
		{"l moves right", "hello", 3, "l", "hello", 4, false},
		{"h stops at the start", "hello", 0, "h", "hello", 0, false},
		{"j moves down", "hello\nworld", 2, "j", "hello\nworld", 8, false},
		{"k moves up", "hello\nworld", 8, "k", "hello\nworld", 2, false},
		{"w to the next word", "one two three", 0, "w", "one two three", 4, false},
		{"w twice", "one two three", 0, "ww", "one two three", 8, false},
		{"b to the previous word", "one two three", 8, "b", "one two three", 4, false},
		{"0 line start", "one two", 5, "0", "one two", 0, false},
		{"$ line end", "one two", 2, "$", "one two", 6, false},
		{"0 is line-local", "one\ntwo", 6, "0", "one\ntwo", 4, false},
		{"$ is line-local", "one\ntwo", 4, "$", "one\ntwo", 6, false},

		// Deletions.
		{"x deletes under the cursor", "hello", 1, "x", "hllo", 1, false},
		{"l stops on the last char", "hello", 4, "l", "hello", 4, false},
		{"$x deletes the last char", "hello\nworld", 0, "$x", "hell\nworld", 3, false},
		{"x on an empty line", "one\n\ntwo", 4, "x", "one\n\ntwo", 4, false},
		{"x is line-local at the break", "ab\ncd", 1, "x", "a\ncd", 0, false},
		{"xx deletes two", "hello", 0, "xx", "llo", 0, false},
		{"D deletes to line end", "hello world", 5, "D", "hello", 4, false},
		{"D keeps the line break", "one\ntwo", 1, "D", "o\ntwo", 0, false},
		{"dd deletes the line", "one\ntwo\nthree", 5, "dd", "one\nthree", 4, false},
		{"dd on the last line", "one\ntwo", 5, "dd", "one", 0, false},
		{"dd on the only line", "only", 2, "dd", "", 0, false},
		{"d then a non-d aborts", "one\ntwo", 5, "dl", "one\ntwo", 5, false},

		// Entering insert mode.
		{"i inserts here", "hello", 2, "i", "hello", 2, true},
		{"a inserts after", "hello", 2, "a", "hello", 3, true},
		{"A inserts at line end", "one\ntwo", 5, "A", "one\ntwo", 7, true},
		{"o opens a line below", "one\ntwo", 1, "o", "one\n\ntwo", 4, true},
		{"O opens a line above", "one\ntwo", 5, "O", "one\n\ntwo", 4, true},
		{"o on the last line", "one", 1, "o", "one\n", 4, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := viComposer(t)
			m.textarea.Value = tc.value
			m.textarea.Cursor = tc.cursor
			m, _ = send(t, m, "\x1b")
			m = chars(t, m, tc.keys)

			if m.textarea.Value != tc.wantValue {
				t.Errorf("Value = %q, want %q", m.textarea.Value, tc.wantValue)
			}
			if m.textarea.Cursor != tc.wantCursor {
				t.Errorf("Cursor = %d, want %d", m.textarea.Cursor, tc.wantCursor)
			}
			if inInsert := !m.IsViNormalMode(); inInsert != tc.wantInsert {
				t.Errorf("insert mode = %v, want %v", inInsert, tc.wantInsert)
			}
		})
	}
}

// o/O reuse the Task-1 newline machinery, so what they open is a real line
// break that survives a send.
func TestViOpenLineProducesRealNewline(t *testing.T) {
	m := chars(t, viComposer(t), "one")
	m, _ = send(t, m, "\x1b")
	m = chars(t, m, "o")
	m = chars(t, m, "two")
	_, msg := send(t, m, "\r")
	submitted, ok := msg.(MessageSubmittedMsg)
	if !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}
	if submitted.Text != "one\ntwo" {
		t.Errorf("Text = %q, want %q", submitted.Text, "one\ntwo")
	}
}

// Normal mode inserts nothing, so every newline chord is inert there — which
// is what the hint line promises once it stops advertising ctrl+j. They all
// come back the moment insert mode does.
func TestViNormalModeIgnoresNewlineChords(t *testing.T) {
	for _, seq := range []string{
		"\n",            // ctrl+j
		"\x1b[106;5u",   // kitty ctrl+j
		"\x1b[13;2u",    // shift+enter
		"\x1b[13;2;13u", // shift+enter with associated text
		"\x1b[13;5u",    // ctrl+enter
	} {
		t.Run(seq, func(t *testing.T) {
			m := chars(t, viComposer(t), "hi")
			m, _ = send(t, m, "\x1b")
			m, _ = send(t, m, seq)
			if m.textarea.Value != "hi" {
				t.Errorf("%q inserted in normal mode: %q", seq, m.textarea.Value)
			}
			if !m.IsViNormalMode() {
				t.Errorf("%q left normal mode", seq)
			}

			// Back in insert mode the chord works again.
			m = chars(t, m, "A")
			m, _ = send(t, m, seq)
			if m.textarea.Value != "hi\n" {
				t.Errorf("%q did not insert in insert mode: %q", seq, m.textarea.Value)
			}
		})
	}
}

// And the hint line agrees with the behavior: ctrl+j is advertised exactly
// where it works.
func TestViHintAdvertisesNewlineOnlyWhereItWorks(t *testing.T) {
	m := viComposer(t)
	m.SetExpanded(true)
	if !strings.Contains(m.View(), "Ctrl+J") {
		t.Error("insert mode does not advertise the newline chord")
	}
	m, _ = send(t, m, "\x1b")
	if strings.Contains(m.View(), "Ctrl+J") {
		t.Error("normal mode advertises a newline chord that does nothing there")
	}
}

// The mode indicator is always on screen — not knowing which mode you are in
// is what makes modal editing hostile.
//
// It is the badge now, at the far left of the composer row, and it covers
// emacs too: "-- INSERT --" only ever appeared in vi mode, so an emacs user
// had nothing on screen saying whether the next letter would be typed or
// would navigate. That is the same question in both keymaps.
func TestViHintShowsMode(t *testing.T) {
	m := viComposer(t)
	if view := m.View(); !strings.Contains(view, "INSERT") {
		t.Errorf("insert-mode badge missing:\n%s", view)
	}
	m, _ = send(t, m, "\x1b")
	if view := m.View(); !strings.Contains(view, "NORMAL") {
		t.Errorf("normal-mode badge missing:\n%s", view)
	}
	// It survives a notice, which takes the rest of the row.
	m.SetNotice("⚠ something happened")
	view := m.View()
	if !strings.Contains(view, "NORMAL") || !strings.Contains(view, "something happened") {
		t.Errorf("notice hid the mode badge:\n%s", view)
	}
	// A focused emacs composer inserts, and says so.
	if view := newFocused().View(); !strings.Contains(view, "INSERT") {
		t.Errorf("emacs composer shows no mode badge:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Task 3 — external editor
// ---------------------------------------------------------------------------

// stubEditor writes an executable shell script that acts as $EDITOR and
// returns its path. Everything about it is POSIX sh, so it is skipped where
// there is no shell to run it with.
func stubEditor(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub editor needs a POSIX shell")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}
	path := filepath.Join(t.TempDir(), "stub-editor")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("writing stub editor: %v", err)
	}
	return path
}

// runEditor performs the full ctrl+o round trip without the Bubble Tea
// runtime: prepareEditor spools the draft, the stub edits the file in place,
// and editorResult reads it back exactly as tea.ExecProcess's callback would.
func runEditor(t *testing.T, m Model, runErr *error) (Model, string) {
	t.Helper()
	sess, err := m.prepareEditor()
	if err != nil {
		t.Fatalf("prepareEditor: %v", err)
	}
	*runErr = sess.cmd.Run()
	msg := editorResult(sess.path)(*runErr)
	m, _ = m.Update(msg)
	return m, sess.path
}

func TestExternalEditorRoundTrip(t *testing.T) {
	t.Setenv("VISUAL", stubEditor(t, `printf ' edited\nsecond line\n' >> "$1"`))
	t.Setenv("EDITOR", "")

	m := newFocused()
	m.textarea.Value = "draft"
	m.textarea.Cursor = 5
	m.EnterReplyMode(7, "the message being replied to")
	m.SetAttachment("/tmp/paste-1.png", true)

	var runErr error
	m, path := runEditor(t, m, &runErr)
	if runErr != nil {
		t.Fatalf("stub editor failed: %v", runErr)
	}

	if want := "draft edited\nsecond line"; m.textarea.Value != want {
		t.Errorf("Value = %q, want %q", m.textarea.Value, want)
	}
	// The cursor lands at the end of what came back.
	if m.textarea.Cursor != m.textarea.Len() {
		t.Errorf("Cursor = %d, want %d", m.textarea.Cursor, m.textarea.Len())
	}
	// Everything that is not text survives the round trip.
	if m.Attachment() != "/tmp/paste-1.png" {
		t.Errorf("Attachment = %q, want it preserved", m.Attachment())
	}
	if !m.IsComposing() || m.mode != ModeReply || m.replyToID != 7 {
		t.Errorf("reply state lost: mode=%v replyToID=%d", m.mode, m.replyToID)
	}
	if m.ChatId() != 42 {
		t.Errorf("ChatId = %d, want 42", m.ChatId())
	}
	// The temp file is gone.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s survived the edit (err=%v)", path, err)
	}

	// And the multi-line result sends as-is.
	_, msg := send(t, m, "\r")
	submitted, ok := msg.(MessageSubmittedMsg)
	if !ok {
		t.Fatalf("got %T, want MessageSubmittedMsg", msg)
	}
	if want := "draft edited\nsecond line"; submitted.Text != want {
		t.Errorf("Text = %q, want %q", submitted.Text, want)
	}
	if submitted.ReplyToId != 7 {
		t.Errorf("ReplyToId = %d, want 7", submitted.ReplyToId)
	}
}

// $VISUAL wins over $EDITOR, and an editor carrying flags runs with them.
func TestExternalEditorPrefersVisualAndKeepsFlags(t *testing.T) {
	t.Setenv("VISUAL", stubEditor(t, `for f; do :; done; [ "$1" = --flag ] && printf 'V' >> "$f"`)+" --flag")
	t.Setenv("EDITOR", stubEditor(t, `printf 'E' >> "$1"`))

	m := newFocused()
	m.textarea.Value = "x"

	var runErr error
	m, _ = runEditor(t, m, &runErr)
	if runErr != nil {
		t.Fatalf("stub editor failed: %v", runErr)
	}
	if m.textarea.Value != "xV" {
		t.Errorf("Value = %q, want %q ($VISUAL with its flag should have run)",
			m.textarea.Value, "xV")
	}
}

// $EDITOR is the fallback when $VISUAL is unset.
func TestExternalEditorFallsBackToEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", stubEditor(t, `printf 'E' >> "$1"`))

	m := newFocused()
	m.textarea.Value = "x"

	var runErr error
	m, _ = runEditor(t, m, &runErr)
	if runErr != nil {
		t.Fatalf("stub editor failed: %v", runErr)
	}
	if m.textarea.Value != "xE" {
		t.Errorf("Value = %q, want %q", m.textarea.Value, "xE")
	}
}

// A non-zero exit is how a vi user aborts (:cq). The draft must survive it.
func TestExternalEditorNonZeroExitKeepsDraft(t *testing.T) {
	t.Setenv("VISUAL", stubEditor(t, `printf 'clobbered' > "$1"; exit 1`))
	t.Setenv("EDITOR", "")

	m := newFocused()
	m.textarea.Value = "original draft"

	var runErr error
	m, path := runEditor(t, m, &runErr)
	if runErr == nil {
		t.Fatal("stub editor exited 0, want a failure")
	}
	if m.textarea.Value != "original draft" {
		t.Errorf("Value = %q, want the original draft kept", m.textarea.Value)
	}
	if view := m.View(); !strings.Contains(view, "draft kept") {
		t.Errorf("failure notice missing from view:\n%s", view)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temp file %s survived a failed edit", path)
	}
}

// With no editor configured, ctrl+o says so and changes nothing.
func TestExternalEditorWithoutEditorSet(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	m := newFocused()
	m.textarea.Value = "draft"

	m, msg := send(t, m, "\x0f") // ctrl+o
	if msg != nil {
		t.Fatalf("ctrl+o produced %T, want no message", msg)
	}
	if m.textarea.Value != "draft" {
		t.Errorf("Value = %q, want it untouched", m.textarea.Value)
	}
	if view := m.View(); !strings.Contains(view, "no $EDITOR set") {
		t.Errorf("missing-editor notice absent from view:\n%s", view)
	}
}

// ctrl+o must not type an "o", in either editing mode.
func TestCtrlOIsNotTypedAsText(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	for _, seq := range []string{"\x0f", "\x1b[111;5u"} {
		m, _ := send(t, newFocused(), seq)
		if m.textarea.Value != "" {
			t.Errorf("%q inserted %q", seq, m.textarea.Value)
		}
	}
}

// The editor result is applied even if focus moved on: it arrives after the
// program resumed from suspension, and dropping it would lose the edits.
func TestEditorResultAppliedWhileUnfocused(t *testing.T) {
	m := newFocused()
	m.textarea.Value = "draft"
	m.SetFocused(false)
	m, _ = m.Update(editorFinishedMsg{text: "from the editor\n", ok: true})
	if m.textarea.Value != "from the editor" {
		t.Errorf("Value = %q, want %q", m.textarea.Value, "from the editor")
	}
}

// Editors append a trailing newline; a chat message should not carry one.
// CRLF is normalised for the same reason.
func TestEditorResultTrimsTrailingNewline(t *testing.T) {
	cases := map[string]string{
		"one\n":           "one",
		"one\n\n\n":       "one",
		"one\ntwo\n":      "one\ntwo",
		"one\r\ntwo\r\n":  "one\ntwo",
		"":                "",
		"\n":              "",
		"keep\n\ninner\n": "keep\n\ninner",
	}
	for in, want := range cases {
		m := newFocused()
		m, _ = m.Update(editorFinishedMsg{text: in, ok: true})
		if m.textarea.Value != want {
			t.Errorf("editor text %q -> %q, want %q", in, m.textarea.Value, want)
		}
	}
}

// TestViDeleteCharNeverJoinsLines is the regression this rule exists for. The
// insert-mode cursor convention parks the cursor in the gap after the last
// character of a line, which is where the line break lives; an unguarded x
// there deleted the break and welded the two lines together.
func TestViDeleteCharNeverJoinsLines(t *testing.T) {
	cases := []struct {
		name      string
		value     string
		cursor    int
		keys      string
		wantValue string
	}{
		{"$ then x", "hello\nworld", 0, "$x", "hell\nworld"},
		{"x from a stale end-of-line cursor", "hello\nworld", 5, "x", "hell\nworld"},
		{"x on the last line", "hello\nworld", 11, "x", "hello\nworl"},
		{"x repeatedly at the line end", "ab\ncd", 2, "xxxx", "\ncd"},
		{"x on an empty line", "a\n\nb", 2, "x", "a\n\nb"},
		{"x on an empty buffer", "", 0, "x", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := viComposer(t)
			m.textarea.Value = tc.value
			m.textarea.Cursor = tc.cursor
			m, _ = send(t, m, "\x1b")
			m = chars(t, m, tc.keys)
			if m.textarea.Value != tc.wantValue {
				t.Errorf("Value = %q, want %q", m.textarea.Value, tc.wantValue)
			}
			if strings.Count(m.textarea.Value, "\n") != strings.Count(tc.wantValue, "\n") {
				t.Errorf("line count changed: %q", m.textarea.Value)
			}
		})
	}
}

// Every normal-mode command leaves the cursor on a character (or in the gap
// of a genuinely empty line), never past the end of a line. That single
// invariant is what x, i and a rely on.
func TestViNormalModeCursorNeverPastLineEnd(t *testing.T) {
	for _, keys := range []string{
		"", "$", "l", "llllllllll", "j", "k", "w", "ww", "b", "0",
		"x", "D", "dd", "$x", "$D", "jx", "j$", "k$",
	} {
		m := viComposer(t)
		m.textarea.Value = "hello\nworld\nab"
		m.textarea.Cursor = 3
		m, _ = send(t, m, "\x1b")
		m = chars(t, m, keys)

		start, end := m.textarea.LineBounds()
		if end > start && m.textarea.Cursor >= end {
			t.Errorf("after %q: cursor %d is past line end %d (line %d..%d) in %q",
				keys, m.textarea.Cursor, end, start, end, m.textarea.Value)
		}
		if m.textarea.Cursor < 0 || m.textarea.Cursor > m.textarea.Len() {
			t.Errorf("after %q: cursor %d out of bounds for %q",
				keys, m.textarea.Cursor, m.textarea.Value)
		}
	}
}

// A composer that has not been sized yet — or that has been squeezed onto a
// terminal with no room to spare — is still a multi-line editor. Before the
// Height floor and the explicit MultiLine flag, both cases silently turned it
// into a single-line widget, which flattens the newlines out of a paste.
func TestComposerStaysMultiLineAtEverySize(t *testing.T) {
	cases := []struct {
		name string
		size func(*Model)
	}{
		{"before the first WindowSizeMsg", func(m *Model) {}},
		{"height 0", func(m *Model) { m.SetSize(60, 0) }},
		{"height 1", func(m *Model) { m.SetSize(60, 1) }},
		{"height 2", func(m *Model) { m.SetSize(60, 2) }},
		{"height 5", func(m *Model) { m.SetSize(60, 5) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(theme.DarkRoles(false))
			tc.size(&m)
			m.SetFocused(true)
			m.SetChatId(42)

			m, _ = m.Update(tea.PasteMsg{Content: "one\ntwo"})
			if m.textarea.Value != "one\ntwo" {
				t.Errorf("pasted draft flattened to %q", m.textarea.Value)
			}
			// The scroll window never collapses to nothing, so however few
			// rows there are, the cursor's line is one of them.
			if m.textarea.Height < 1 {
				t.Errorf("textarea.Height = %d, want at least 1", m.textarea.Height)
			}
			if view := m.View(); !strings.Contains(view, "two") {
				t.Errorf("cursor's line missing from view:\n%s", view)
			}
			// The ctrl+j chord agrees with the paste.
			m, _ = send(t, m, "\n")
			if m.textarea.Value != "one\ntwo\n" {
				t.Errorf("Value = %q, want %q", m.textarea.Value, "one\ntwo\n")
			}
		})
	}
}

// $EDITOR is a shell command line, so a path with spaces in it works when the
// user quotes it — the way git has always required — and a bare unquoted path
// works too, via the "the whole variable names an executable" fast path.
func TestExternalEditorHandlesSpacesInPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub editor needs a POSIX shell")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	// A stub living in a directory whose name contains a space, the shape
	// strings.Fields could never handle: "/Applications/Sublime Text/subl".
	dir := filepath.Join(t.TempDir(), "Sublime Text")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stub := filepath.Join(dir, "subl")
	// The file is the *last* argument: any flags in $EDITOR come first.
	body := "#!/bin/sh\nfor f; do :; done\nprintf 'S' >> \"$f\"\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("writing stub editor: %v", err)
	}

	cases := []struct {
		name   string
		editor string
	}{
		{"bare path with spaces", stub},
		{"quoted path with a flag", `"` + stub + `" -w`},
		{"quoted path, no flag", `"` + stub + `"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VISUAL", tc.editor)
			t.Setenv("EDITOR", "")

			m := newFocused()
			m.textarea.Value = "x"

			var runErr error
			m, _ = runEditor(t, m, &runErr)
			if runErr != nil {
				t.Fatalf("stub editor failed: %v", runErr)
			}
			if m.textarea.Value != "xS" {
				t.Errorf("Value = %q, want %q", m.textarea.Value, "xS")
			}
		})
	}
}

// The notice names the editor, not the shell it is launched through.
func TestExternalEditorNoticeNamesTheEditor(t *testing.T) {
	t.Setenv("VISUAL", stubEditor(t, `sleep 0`)+" --flag")
	t.Setenv("EDITOR", "")

	m := newFocused()
	m, _ = send(t, m, "\x0f") // ctrl+o
	view := m.View()
	if !strings.Contains(view, "stub-editor") {
		t.Errorf("notice does not name the editor:\n%s", view)
	}
	if strings.Contains(view, "/bin/sh") {
		t.Errorf("notice names the shell instead of the editor:\n%s", view)
	}
}

// TestAltEnterIsNotANewlineChord is decision I-1's negative test in this
// package. alt+enter was accepted as a third newline chord; Alt is gone from
// the client, and this one carried a hazard of its own — its legacy encoding
// is ESC CR, byte-for-byte what "press Escape, then press Enter" produces,
// which is exactly what a vi user types to leave insert mode and send.
func TestAltEnterIsNotANewlineChord(t *testing.T) {
	for _, seq := range []string{
		"\x1b[13;3u",    // kitty alt+enter
		"\x1b[27;3;13~", // modifyOtherKeys alt+enter
	} {
		t.Run(seq, func(t *testing.T) {
			m := chars(t, newFocused(), "ab")
			m, _ = send(t, m, seq)
			m = chars(t, m, "cd")
			if strings.Contains(m.textarea.Value, "\n") {
				t.Errorf("alt+enter inserted a newline: %q", m.textarea.Value)
			}
		})
	}
}
