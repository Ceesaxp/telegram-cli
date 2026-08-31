package app

import (
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
)

// composeModel is a chat with one message to act on, in the given keymap.
func composeModel(t *testing.T, editing composer.EditingMode) Model {
	t.Helper()
	m := openChatModel(t, PanelChatView)
	m.composer.SetEditingMode(editing)
	m.store.Messages.Append(testChatID, &telegram.Message{
		ID: 7, ChatID: testChatID, Date: 1700000000,
		SenderID: &telegram.MessageSenderUser{UserID: 200},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "hello"}},
	})
	return m
}

// leaveViaEscape leaves the composer the way a vi user does: one escape to
// normal mode, a second to give the panel back.
func leaveViaEscape(t *testing.T, m Model) Model {
	t.Helper()
	m = update(t, m, "i")
	m = update(t, m, "\x1b")
	m = update(t, m, "\x1b")
	if !m.composer.IsViNormalMode() {
		t.Fatal("precondition: the composer is not in vi normal mode")
	}
	return m
}

// Entering the composer means you intend to type.
//
// vi state was initialised once and then remembered, and a vi user always
// leaves the composer through normal mode — so the composer was ALWAYS in
// normal mode by the time it was next entered. r, e and i each landed there,
// and the first keystrokes became commands: pressing r and typing "abc"
// produced "bc", because the "a" was vi's append. ctrl+j did not insert a
// newline for the same reason. ctrl+o worked, because it is handled before
// the modal dispatch — which is why the external editor looked like the only
// way to write anything.
func TestEnteringTheComposerStartsInInsertMode(t *testing.T) {
	// Telegram allows editing only your own messages, so the edit case needs
	// one — pressing e on somebody else's is correctly refused.
	ownMessage := func(t *testing.T, m Model) Model {
		t.Helper()
		for _, msg := range m.store.Messages.Get(testChatID) {
			msg.SenderID = &telegram.MessageSenderUser{UserID: m.myUserId}
		}
		return m
	}

	tests := []struct {
		name string
		key  string
		run  func(*testing.T, Model) Model
	}{
		{"i", "i", nil},
		{"reply", "r", nil},
		{"edit", "e", ownMessage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := composeModel(t, composer.ModeVi)
			if tt.run != nil {
				m = tt.run(t, m)
			}
			m = leaveViaEscape(t, m)

			m, cmd := updateCmd(t, m, tt.key)
			if cmd != nil {
				m = send(t, m, cmd())
			}
			if m.focus != PanelComposer {
				t.Fatalf("%q did not focus the composer (focus = %v)", tt.key, m.focus)
			}
			if m.composer.IsViNormalMode() {
				t.Fatalf("%q left the composer in vi normal mode", tt.key)
			}

			for _, r := range "abc" {
				m = update(t, m, string(r))
			}
			// Ends with, not equals: edit pre-loads the message being
			// edited and puts the cursor after it, which is the point of
			// edit. What matters is that all three characters landed as
			// text rather than being eaten as vi commands.
			if got := m.composer.Draft(); !strings.HasSuffix(got, "abc") {
				t.Errorf("typing abc after %q gave %q", tt.key, got)
			}
		})
	}
}

// And ctrl+j inserts a newline once there, which it cannot do in normal mode
// — deliberately, so the hint line and the behaviour agree.
func TestCtrlJInsertsANewlineAfterEnteringTheComposer(t *testing.T) {
	m := leaveViaEscape(t, composeModel(t, composer.ModeVi))
	m = update(t, m, "i")

	for _, r := range "ab" {
		m = update(t, m, string(r))
	}
	m = update(t, m, "\n") // ctrl+j
	for _, r := range "cd" {
		m = update(t, m, string(r))
	}

	if got := m.composer.Draft(); got != "ab\ncd" {
		t.Errorf("draft = %q, want a newline between ab and cd", got)
	}
}

// Escape still reaches normal mode from inside — the reset is on the way IN,
// not a removal of the mode.
func TestEscapeStillReachesNormalMode(t *testing.T) {
	m := composeModel(t, composer.ModeVi)
	m = update(t, m, "i")
	if m.composer.IsViNormalMode() {
		t.Fatal("the composer opened in normal mode")
	}

	m = update(t, m, "\x1b")
	if !m.composer.IsViNormalMode() {
		t.Error("escape did not reach vi normal mode")
	}
}

// Emacs mode has no modal state to reset, and must be unaffected.
func TestEmacsComposerIsUnaffected(t *testing.T) {
	m := composeModel(t, composer.ModeEmacs)
	m = update(t, m, "i")

	for _, r := range "abc" {
		m = update(t, m, string(r))
	}
	if got := m.composer.Draft(); got != "abc" {
		t.Errorf("draft = %q, want abc", got)
	}
	if m.composer.IsViNormalMode() {
		t.Error("an emacs composer reports vi normal mode")
	}
}
