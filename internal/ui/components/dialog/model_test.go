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

// TestPromptDefaultsToOK is the data-loss regression: a prompt COLLECTS
// something, so the reflex after typing — Enter — has to accept it. This
// dialog used to start on Cancel, which silently discarded the path the
// user had just typed into the attach-file flow.
func TestPromptDefaultsToOK(t *testing.T) {
	m := NewPrompt(theme.DarkRoles(false), "attach-file", "Attach File", "Path to file:")
	if m.buttonIdx != len(m.buttons)-1 {
		t.Fatalf("a fresh prompt highlights button %d (%q), want the last one (OK)",
			m.buttonIdx, m.buttons[m.buttonIdx])
	}
	if !strings.Contains(ansi.Strip(m.View()), "[ OK ]") {
		t.Fatalf("a fresh prompt does not show OK as highlighted:\n%s", ansi.Strip(m.View()))
	}
}

// TestPromptTypeThenEnterCarriesTheInput is the attach-file flow
// end-to-end: type a path, press Enter, get it back.
func TestPromptTypeThenEnterCarriesTheInput(t *testing.T) {
	m := NewPrompt(theme.DarkRoles(false), "attach-file", "Attach File", "Path to file:")

	for _, r := range "/tmp/a.png" {
		m, _ = press(m, key(r))
	}
	_, cmd := press(m, specialKey(tea.KeyEnter))

	got := result(t, cmd)
	if !got.Confirmed {
		t.Fatal("type-then-enter did not confirm the prompt: the typed path would be discarded")
	}
	if got.Input != "/tmp/a.png" {
		t.Fatalf("prompt result Input = %q, want %q", got.Input, "/tmp/a.png")
	}
	if got.ID != "attach-file" {
		t.Fatalf("prompt result ID = %q, want %q", got.ID, "attach-file")
	}
}

// TestPromptTreatsMovementLettersAsText: a prompt dialog's input owns
// every printable — the attach-file dialog takes a path, and paths
// contain j's and k's.
func TestPromptTreatsMovementLettersAsText(t *testing.T) {
	m := NewPrompt(theme.DarkRoles(false), "attach-file", "Attach File", "Path to file:")

	start := m.buttonIdx
	m, _ = press(m, key('j'), key('k'), key('.'), key('t'), key('x'), key('t'))
	if m.input != "jk.txt" {
		t.Fatalf("prompt input = %q, want %q — j/k must be text in a prompt", m.input, "jk.txt")
	}
	if m.buttonIdx != start {
		t.Fatalf("j/k moved the prompt's button highlight to %d, want %d", m.buttonIdx, start)
	}

	// Arrows and tab are not text, so they still choose a button — here
	// moving OFF the default OK and onto Cancel.
	m, _ = press(m, specialKey(tea.KeyTab))
	if m.buttonIdx != 0 {
		t.Fatalf("tab in a prompt: buttonIdx = %d, want 0 (Cancel)", m.buttonIdx)
	}
	_, cmd := press(m, specialKey(tea.KeyEnter))
	if got := result(t, cmd); got.Confirmed {
		t.Fatalf("enter on the highlighted Cancel confirmed: %+v", got)
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

	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "[ Cancel ]") {
		t.Fatalf("View() does not mark Cancel as highlighted:\n%s", plain)
	}
	if strings.Contains(plain, "[ Confirm ]") {
		t.Fatalf("View() marks Confirm as highlighted while buttonIdx is 0:\n%s", plain)
	}

	m, _ = press(m, specialKey(tea.KeyRight))
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "[ Confirm ]") {
		t.Fatalf("after right, View() does not mark Confirm as highlighted:\n%s", plain)
	}
	if strings.Contains(plain, "[ Cancel ]") {
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

	// A prompt leads with Enter, because that is the reflex at the end of
	// typing and here it accepts what was typed.
	p := NewPrompt(theme.DarkRoles(false), "attach-file", "Attach File", "Path to file:")
	plain = ansi.Strip(p.View())
	if !strings.Contains(plain, "enter: accept input") {
		t.Fatalf("prompt View() does not say Enter accepts the input:\n%s", plain)
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
		"prompt":  NewPrompt(theme.DarkRoles(false), "attach-file", "Attach File", "Path to file:"),
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
		"prompt":  NewPrompt(theme.DarkRoles(false), "attach-file", "Attach File", "Path to file:"),
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
