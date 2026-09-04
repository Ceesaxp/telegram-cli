package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// key returns a printable-character key press, matching how bubbletea
// reports letter keys.
func key(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

// specialKey returns a non-printable key press (enter, esc, arrows).
func specialKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func press(m Model, msgs ...tea.KeyPressMsg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		m, cmd = m.Update(msg)
	}
	return m, cmd
}

func result(t *testing.T, cmd tea.Cmd) DialogResultMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("dialog produced no result command")
	}
	msg, ok := cmd().(DialogResultMsg)
	if !ok {
		t.Fatalf("dialog produced %#v, want DialogResultMsg", cmd())
	}
	return msg
}

// TestConfirmDefaultsToCancel pins the safety posture deliberately:
// Enter accepts the HIGHLIGHTED button, and a confirm dialog starts on
// Cancel, so a reflex Enter can never perform the destructive action the
// dialog is guarding (deleting a message, quitting with a draft).
func TestConfirmDefaultsToCancel(t *testing.T) {
	m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?")
	m, cmd := press(m, specialKey(tea.KeyEnter))

	got := result(t, cmd)
	if got.Confirmed {
		t.Fatal("a fresh confirm dialog + enter confirmed; it must default to Cancel")
	}
	if got.ID != "delete" {
		t.Fatalf("result ID = %q, want %q", got.ID, "delete")
	}
	if m.IsVisible() {
		t.Fatal("enter left the dialog visible")
	}
}

// TestMovementKeysMoveTheHighlight covers the keys that actually move
// the highlight. j/k are NOT among them — see
// TestConfirmIgnoresJAndK.
func TestMovementKeysMoveTheHighlight(t *testing.T) {
	cases := map[string]tea.KeyPressMsg{
		"tab":   specialKey(tea.KeyTab),
		"left":  specialKey(tea.KeyLeft),
		"right": specialKey(tea.KeyRight),
	}

	for name, k := range cases {
		t.Run(name, func(t *testing.T) {
			m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?")
			if m.buttonIdx != 0 {
				t.Fatalf("setup: buttonIdx = %d, want 0 (Cancel)", m.buttonIdx)
			}

			m, _ = press(m, k)
			if m.buttonIdx != 1 {
				t.Fatalf("%q left buttonIdx = %d, want 1 (Confirm)", name, m.buttonIdx)
			}

			// Enter now accepts the highlighted Confirm.
			_, cmd := press(m, specialKey(tea.KeyEnter))
			if !result(t, cmd).Confirmed {
				t.Fatalf("enter after %q did not confirm", name)
			}
		})
	}
}

// TestMovementWrapsBothWays: with two buttons every movement key toggles,
// and the direction keys are honest about their direction.
func TestMovementWrapsBothWays(t *testing.T) {
	m := NewConfirm(theme.DarkRoles(false), "quit", "Quit", "Discard the draft?")

	m, _ = press(m, specialKey(tea.KeyLeft))
	if m.buttonIdx != 1 {
		t.Fatalf("left from Cancel: buttonIdx = %d, want 1 (wraps to Confirm)", m.buttonIdx)
	}
	m, _ = press(m, specialKey(tea.KeyLeft))
	if m.buttonIdx != 0 {
		t.Fatalf("left from Confirm: buttonIdx = %d, want 0 (Cancel)", m.buttonIdx)
	}
	m, _ = press(m, specialKey(tea.KeyRight), specialKey(tea.KeyRight))
	if m.buttonIdx != 0 {
		t.Fatalf("right twice: buttonIdx = %d, want back at 0", m.buttonIdx)
	}
}

// TestConfirmIgnoresJAndK is a safety regression test, not a style one.
// j/k were briefly wired as movement here; that made the destructive
// path EASIER, because j/k are what a user's fingers are already on when
// a confirm appears mid-scroll — "d, reflexive j, enter" would have
// deleted a message for everyone. A confirm dialog must not move its
// highlight for any key but the horizontal ones.
func TestConfirmIgnoresJAndK(t *testing.T) {
	for _, r := range []rune{'j', 'k'} {
		m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?")
		m, _ = press(m, key(r))
		if m.buttonIdx != 0 {
			t.Fatalf("%q moved the confirm highlight to %d; it must stay on Cancel", string(r), m.buttonIdx)
		}

		// And the reflex that follows must still be harmless.
		_, cmd := press(m, specialKey(tea.KeyEnter))
		if result(t, cmd).Confirmed {
			t.Fatalf("%q then enter confirmed a destructive dialog", string(r))
		}
	}
}

func TestEscCancels(t *testing.T) {
	m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?")
	m, _ = press(m, specialKey(tea.KeyRight)) // highlight Confirm
	m, cmd := press(m, specialKey(tea.KeyEscape))

	if result(t, cmd).Confirmed {
		t.Fatal("esc confirmed the dialog")
	}
	if m.IsVisible() {
		t.Fatal("esc left the dialog visible")
	}
}

// TestViewMarksTheHighlightedButtonWithoutColor: Enter accepts whatever
// is highlighted, so which button that is must be readable with the
// styling stripped — a reversed-color block alone is invisible on a
// monochrome terminal.
func TestViewMarksTheHighlightedButtonWithoutColor(t *testing.T) {
	m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?")

	// The labels carry their accelerator: "Cancel" answers to n, and n is
	// inside the word, so it is parenthesised there.
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "[ Ca(n)cel ]") {
		t.Fatalf("View() does not mark Cancel as highlighted:\n%s", plain)
	}
	if strings.Contains(plain, "[ Confirm (y) ]") {
		t.Fatalf("View() marks Confirm as highlighted while buttonIdx is 0:\n%s", plain)
	}

	m, _ = press(m, specialKey(tea.KeyRight))
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "[ Confirm (y) ]") {
		t.Fatalf("after right, View() does not mark Confirm as highlighted:\n%s", plain)
	}
	if strings.Contains(plain, "[ Ca(n)cel ]") {
		t.Fatalf("after right, View() still marks Cancel as highlighted:\n%s", plain)
	}
}

// TestViewShowsTheMovementHint: "enter accepts the highlighted button"
// must be discoverable from the dialog itself, not by losing something
// to it.
func TestViewShowsTheMovementHint(t *testing.T) {
	m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?")
	plain := ansi.Strip(m.View())
	for _, want := range []string{"←/→: choose", "enter: accept", "esc: cancel"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("confirm View() is missing %q:\n%s", want, plain)
		}
	}

	// A single-button alert has nothing to choose between, so it says so.
	a := NewAlert(theme.DarkRoles(false), "oops", "Error", "Something broke")
	plain = ansi.Strip(a.View())
	if strings.Contains(plain, "choose") {
		t.Fatalf("an alert with one button advertises a choice:\n%s", plain)
	}
	if !strings.Contains(plain, "enter or esc: dismiss") {
		t.Fatalf("alert View() is missing its dismiss hint:\n%s", plain)
	}
}

// TestHintNamesOnlyKeysTheDialogGets: the hint must not advertise a key
// the dialog never receives (the app's focus cycling eats tab) or a key
// that is not movement at all (j/k). A hint that lists keys the
// component does not honor is the same documentation drift this wave
// removed from the status bar and the README.
func TestHintNamesOnlyKeysTheDialogGets(t *testing.T) {
	dialogs := map[string]Model{
		"confirm": NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?"),
		"alert":   NewAlert(theme.DarkRoles(false), "oops", "Error", "Something broke"),
	}
	for name, m := range dialogs {
		plain := ansi.Strip(m.renderHint())
		for _, forbidden := range []string{"tab", "j/k", "j:", "k:"} {
			if strings.Contains(plain, forbidden) {
				t.Errorf("%s hint advertises %q, which the dialog does not get: %q", name, forbidden, plain)
			}
		}
	}
}

// TestViewIsARectangle: every line of the rendered box must be the same
// width, or lipgloss.Place (which the app centers it with) paints a
// ragged overlay.
func TestViewIsARectangle(t *testing.T) {
	dialogs := map[string]Model{
		"confirm": NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?"),
		"alert":   NewAlert(theme.DarkRoles(false), "oops", "Error", "Something broke"),
	}
	for name, m := range dialogs {
		t.Run(name, func(t *testing.T) {
			view := m.View()
			want := lipgloss.Width(view)
			for i, line := range strings.Split(view, "\n") {
				if got := ansi.StringWidth(line); got != want {
					t.Fatalf("line %d is %d cells, want %d: %q", i, got, want, ansi.Strip(line))
				}
			}
		})
	}
}

// TestHiddenDialogIgnoresKeys guards the early return in Update.
func TestHiddenDialogIgnoresKeys(t *testing.T) {
	m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Are you sure?")
	m, _ = press(m, specialKey(tea.KeyEscape))

	m2, cmd := press(m, specialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("a closed dialog still emitted a result")
	}
	if m2.View() != "" {
		t.Fatalf("a closed dialog rendered %q", m2.View())
	}
}

// --- decision I-7: a confirm says what it confirms, and answers to y/n ----

// TestYAndNAnswerAConfirm: y and n are safe here for the reason j/k are not.
// They are not keys anybody is holding when the dialog appears, so they
// cannot be pressed by reflex — and typing y at a yes/no question is what a
// person does.
func TestYAndNAnswerAConfirm(t *testing.T) {
	t.Run("y confirms", func(t *testing.T) {
		m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Delete this message?")
		m, cmd := press(m, key('y'))
		if !result(t, cmd).Confirmed {
			t.Error("y did not confirm")
		}
		if m.IsVisible() {
			t.Error("y left the dialog visible")
		}
	})

	t.Run("n cancels", func(t *testing.T) {
		m := NewConfirm(theme.DarkRoles(false), "delete", "Delete Message", "Delete this message?")
		m, cmd := press(m, key('n'))
		if result(t, cmd).Confirmed {
			t.Error("n confirmed")
		}
		if m.IsVisible() {
			t.Error("n left the dialog visible")
		}
	})
}

// TestChoiceCarriesTheAnswer: a dialog with more than two answers cannot be
// read off Confirmed alone, which is why the result carries the value.
func TestChoiceCarriesTheAnswer(t *testing.T) {
	choice := func() Model {
		return NewChoice(theme.DarkRoles(false), "delete", "Delete Message",
			"Delete this message?", []Button{
				{Label: "Cancel", Accel: "n", Value: "cancel"},
				{Label: "For me", Accel: "m", Value: "me", Affirmative: true},
				{Label: "For everyone", Accel: "e", Value: "everyone", Affirmative: true},
			})
	}

	cases := []struct {
		key           rune
		wantValue     string
		wantConfirmed bool
	}{
		{'n', "cancel", false},
		{'m', "me", true},
		{'e', "everyone", true},
	}
	for _, tc := range cases {
		t.Run(string(tc.key), func(t *testing.T) {
			_, cmd := press(choice(), key(tc.key))
			got := result(t, cmd)
			if got.Value != tc.wantValue {
				t.Errorf("Value = %q, want %q", got.Value, tc.wantValue)
			}
			if got.Confirmed != tc.wantConfirmed {
				t.Errorf("Confirmed = %v, want %v", got.Confirmed, tc.wantConfirmed)
			}
		})
	}

	// Enter still accepts the highlighted button, and a choice still opens
	// on the one that backs out.
	_, cmd := press(choice(), specialKey(tea.KeyEnter))
	if got := result(t, cmd); got.Confirmed || got.Value != "cancel" {
		t.Errorf("enter on a fresh choice gave %+v, want the cancel answer", got)
	}
}

// TestEveryAcceleratorIsDrawnInItsLabel: an accelerator nobody can see is a
// key that has to be discovered by guessing.
func TestEveryAcceleratorIsDrawnInItsLabel(t *testing.T) {
	m := NewChoice(theme.DarkRoles(false), "delete", "Delete Message",
		"Delete this message?", []Button{
			{Label: "Cancel", Accel: "n", Value: "cancel"},
			{Label: "For me", Accel: "m", Value: "me", Affirmative: true},
			{Label: "For everyone", Accel: "e", Value: "everyone", Affirmative: true},
		})

	plain := ansi.Strip(m.View())
	for _, want := range []string{"Ca(n)cel", "For (m)e", "For (e)veryone"} {
		if !strings.Contains(plain, want) {
			t.Errorf("View() is missing %q:\n%s", want, plain)
		}
	}
}

// TestTheHintIsBuiltFromTheButtons: the hint line named a fixed set of keys
// beside a button row it did not read, which is exactly how a surface comes
// to describe a keymap it does not have.
func TestTheHintIsBuiltFromTheButtons(t *testing.T) {
	m := NewChoice(theme.DarkRoles(false), "delete", "Delete Message",
		"Delete this message?", []Button{
			{Label: "Cancel", Accel: "n", Value: "cancel"},
			{Label: "For me", Accel: "m", Value: "me", Affirmative: true},
			{Label: "For everyone", Accel: "e", Value: "everyone", Affirmative: true},
		})

	if plain := ansi.Strip(m.renderHint()); !strings.Contains(plain, "n/m/e: answer") {
		t.Errorf("hint = %q, want it to name all three answers", plain)
	}

	c := NewConfirm(theme.DarkRoles(false), "quit", "Quit", "Discard the draft?")
	if plain := ansi.Strip(c.renderHint()); !strings.Contains(plain, "n/y: answer") {
		t.Errorf("confirm hint = %q, want it to name y and n", plain)
	}
}
