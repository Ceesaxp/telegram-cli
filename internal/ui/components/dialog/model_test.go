package dialog

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
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
	m := NewConfirm(theme.DarkTheme(), "delete", "Delete Message", "Are you sure?")
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

// TestMovementKeysMoveTheHighlight covers the keys the help card
// advertises. j/k were advertised but did nothing at all.
func TestMovementKeysMoveTheHighlight(t *testing.T) {
	cases := map[string]tea.KeyPressMsg{
		"j":     key('j'),
		"k":     key('k'),
		"tab":   specialKey(tea.KeyTab),
		"left":  specialKey(tea.KeyLeft),
		"right": specialKey(tea.KeyRight),
	}

	for name, k := range cases {
		t.Run(name, func(t *testing.T) {
			m := NewConfirm(theme.DarkTheme(), "delete", "Delete Message", "Are you sure?")
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
	m := NewConfirm(theme.DarkTheme(), "quit", "Quit", "Discard the draft?")

	m, _ = press(m, specialKey(tea.KeyLeft))
	if m.buttonIdx != 1 {
		t.Fatalf("left from Cancel: buttonIdx = %d, want 1 (wraps to Confirm)", m.buttonIdx)
	}
	m, _ = press(m, specialKey(tea.KeyLeft))
	if m.buttonIdx != 0 {
		t.Fatalf("left from Confirm: buttonIdx = %d, want 0 (Cancel)", m.buttonIdx)
	}
	m, _ = press(m, key('j'), key('j'))
	if m.buttonIdx != 0 {
		t.Fatalf("j twice: buttonIdx = %d, want back at 0", m.buttonIdx)
	}
}

// TestPromptTreatsMovementLettersAsText: a prompt dialog's input owns
// every printable — the attach-file dialog takes a path, and paths
// contain j's and k's.
func TestPromptTreatsMovementLettersAsText(t *testing.T) {
	m := NewPrompt(theme.DarkTheme(), "attach-file", "Attach File", "Path to file:")

	m, _ = press(m, key('j'), key('k'), key('.'), key('t'), key('x'), key('t'))
	if m.input != "jk.txt" {
		t.Fatalf("prompt input = %q, want %q — j/k must be text in a prompt", m.input, "jk.txt")
	}
	if m.buttonIdx != 0 {
		t.Fatalf("j/k moved the prompt's button highlight to %d, want 0", m.buttonIdx)
	}

	// Arrows and tab are not text, so they still choose a button.
	m, _ = press(m, specialKey(tea.KeyTab))
	if m.buttonIdx != 1 {
		t.Fatalf("tab in a prompt: buttonIdx = %d, want 1", m.buttonIdx)
	}
	_, cmd := press(m, specialKey(tea.KeyEnter))
	got := result(t, cmd)
	if !got.Confirmed || got.Input != "jk.txt" {
		t.Fatalf("prompt result = %+v, want confirmed with input %q", got, "jk.txt")
	}
}

func TestEscCancels(t *testing.T) {
	m := NewConfirm(theme.DarkTheme(), "delete", "Delete Message", "Are you sure?")
	m, _ = press(m, key('j')) // highlight Confirm
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
	m := NewConfirm(theme.DarkTheme(), "delete", "Delete Message", "Are you sure?")

	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "[ Cancel ]") {
		t.Fatalf("View() does not mark Cancel as highlighted:\n%s", plain)
	}
	if strings.Contains(plain, "[ Confirm ]") {
		t.Fatalf("View() marks Confirm as highlighted while buttonIdx is 0:\n%s", plain)
	}

	m, _ = press(m, key('j'))
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "[ Confirm ]") {
		t.Fatalf("after j, View() does not mark Confirm as highlighted:\n%s", plain)
	}
	if strings.Contains(plain, "[ Cancel ]") {
		t.Fatalf("after j, View() still marks Cancel as highlighted:\n%s", plain)
	}
}

// TestViewShowsTheMovementHint: "enter accepts the highlighted button"
// must be discoverable from the dialog itself, not by losing something
// to it.
func TestViewShowsTheMovementHint(t *testing.T) {
	m := NewConfirm(theme.DarkTheme(), "delete", "Delete Message", "Are you sure?")
	plain := ansi.Strip(m.View())
	for _, want := range []string{"tab: choose", "enter: accept"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("View() is missing %q:\n%s", want, plain)
		}
	}

	// A single-button alert has nothing to choose between, so it says so.
	a := NewAlert(theme.DarkTheme(), "oops", "Error", "Something broke")
	plain = ansi.Strip(a.View())
	if strings.Contains(plain, "choose") {
		t.Fatalf("an alert with one button advertises a choice:\n%s", plain)
	}
	if !strings.Contains(plain, "enter: dismiss") {
		t.Fatalf("alert View() is missing its dismiss hint:\n%s", plain)
	}
}

// TestViewIsARectangle: every line of the rendered box must be the same
// width, or lipgloss.Place (which the app centers it with) paints a
// ragged overlay.
func TestViewIsARectangle(t *testing.T) {
	dialogs := map[string]Model{
		"confirm": NewConfirm(theme.DarkTheme(), "delete", "Delete Message", "Are you sure?"),
		"alert":   NewAlert(theme.DarkTheme(), "oops", "Error", "Something broke"),
		"prompt":  NewPrompt(theme.DarkTheme(), "attach-file", "Attach File", "Path to file:"),
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
	m := NewConfirm(theme.DarkTheme(), "delete", "Delete Message", "Are you sure?")
	m, _ = press(m, specialKey(tea.KeyEscape))

	m2, cmd := press(m, specialKey(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("a closed dialog still emitted a result")
	}
	if m2.View() != "" {
		t.Fatalf("a closed dialog rendered %q", m2.View())
	}
}
