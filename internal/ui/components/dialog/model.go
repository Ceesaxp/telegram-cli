package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// DialogResultMsg is emitted when a dialog is closed.
//
// Value names the button that was chosen, so a dialog with more than two
// answers can be told apart by the host: the delete confirm asks "for me" or
// "for everyone", and the caller has to know which. Confirmed remains the
// yes/no reading of the same answer — true for any button that acts, false
// for the one that backs out and for Escape — so the callers that only ever
// asked one question did not have to learn a second vocabulary.
type DialogResultMsg struct {
	ID        string
	Confirmed bool
	Value     string
}

// Button is one answer a dialog offers.
//
// Accel is the single key that chooses it outright, drawn inside the label
// so it is discoverable before it is needed (decision I-7). It is not a held
// key — that is why y/n are safe here and j/k are not. The reason j/k were
// kept off the button row is that they are what a reader's fingers are
// already on when a confirm appears mid-scroll; an accelerator letter nobody
// is holding cannot be pressed by reflex.
//
// Affirmative is what Confirmed reports for this button. Every button that
// does the thing is affirmative; the one that backs out is not.
type Button struct {
	Label       string
	Accel       string
	Value       string
	Affirmative bool
}

// Model is a modal dialog component.
type Model struct {
	roles     theme.Roles
	visible   bool
	id        string
	title     string
	message   string
	buttonIdx int
	buttons   []Button
	width     int
	height    int
}

// NewChoice creates a dialog with an arbitrary set of answers.
//
// The first button is the highlighted one, and it must be the one that backs
// out: Enter accepts whatever is highlighted, so a reflex Enter has to be
// harmless. NewConfirm and NewAlert are the two shapes that existed before
// this and are now wrappers, so there is one implementation of the button
// row, the accelerators and the hint line rather than one per shape.
func NewChoice(r theme.Roles, id, title, message string, buttons []Button) Model {
	return Model{
		roles:   r,
		visible: true,
		id:      id,
		title:   title,
		message: message,
		buttons: buttons,
	}
}

// NewConfirm creates a two-button confirmation dialog, answerable with y and
// n as well as with the arrows (decision I-7).
func NewConfirm(r theme.Roles, id, title, message string) Model {
	return NewChoice(r, id, title, message, []Button{
		{Label: "Cancel", Accel: "n", Value: "cancel"},
		{Label: "Confirm", Accel: "y", Value: "confirm", Affirmative: true},
	})
}

// NewAlert creates an alert dialog: one button, nothing to choose between.
func NewAlert(r theme.Roles, id, title, message string) Model {
	return NewChoice(r, id, title, message, []Button{
		{Label: "OK", Value: "ok", Affirmative: true},
	})
}

// IsVisible returns whether the dialog is visible.
func (m Model) IsVisible() bool {
	return m.visible
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// An accelerator answers outright: y and n on a confirm, n/m/e on
		// the delete choice. Checked before the movement keys so a button
		// whose accelerator collided with one could not be shadowed by it.
		if i, ok := m.acceleratorIndex(msg.String()); ok {
			m.buttonIdx = i
			return m.choose()
		}

		switch msg.String() {
		case "esc":
			m.visible = false
			return m, func() tea.Msg {
				return DialogResultMsg{ID: m.id, Confirmed: false}
			}

		// Movement is HORIZONTAL only. j/k were briefly wired here and
		// are deliberately gone: the button row is a row, "up"/"down"
		// have no meaning across it, and j/k are precisely the keys a
		// user is already holding when a confirm appears mid-scroll —
		// d, a reflexive j, Enter, and the message is deleted for
		// everyone.
		//
		// tab stays honored at this level even though the app's own
		// focus cycling normally consumes it before the dialog sees it;
		// the hint therefore does not advertise it.
		case "tab", "right":
			m.moveButton(1)

		case "left":
			m.moveButton(-1)

		case "enter":
			// Enter accepts the HIGHLIGHTED button, whatever it is — it
			// is never a shortcut for a particular button. A confirm
			// starts on Cancel, because a reflex Enter must not delete a
			// message or discard a draft. The View below makes the
			// highlighted button unmistakable and spells the keys out,
			// so that default is visible before Enter is pressed.
			return m.choose()
		}
	}

	return m, nil
}

// choose closes the dialog on the highlighted button and reports it.
func (m Model) choose() (Model, tea.Cmd) {
	m.visible = false
	var chosen Button
	if m.buttonIdx >= 0 && m.buttonIdx < len(m.buttons) {
		chosen = m.buttons[m.buttonIdx]
	}
	return m, func() tea.Msg {
		return DialogResultMsg{
			ID:        m.id,
			Confirmed: chosen.Affirmative,
			Value:     chosen.Value,
		}
	}
}

// acceleratorIndex finds the button a key answers for, if any. An empty
// accelerator never matches, so a button without one (an alert's OK) cannot
// be chosen by a key event that reports no text.
func (m Model) acceleratorIndex(key string) (int, bool) {
	if key == "" {
		return 0, false
	}
	for i, b := range m.buttons {
		if b.Accel != "" && strings.EqualFold(b.Accel, key) {
			return i, true
		}
	}
	return 0, false
}

// View renders the dialog.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	title := theme.OverlayTitle(m.roles).Render(m.title)
	message := theme.OverlayBody(m.roles).Render(m.message)

	rows := []string{title, "", message}
	buttons, hint := m.renderButtons(), m.renderHint()
	rows = append(rows, "", buttons, "", hint)

	// Center the button row and the hint under the widest row. The box
	// itself has no fixed width (DialogBox sizes to its content), so
	// centering needs that width computed here — the previous
	// Align(Center) with no Width was a no-op, leaving both left-aligned
	// under a message that is usually wider.
	width := 0
	for _, r := range rows {
		if w := cell.MaxWidth(r); w > width {
			width = w
		}
	}
	center := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	rows[len(rows)-3] = center.Render(buttons)
	rows[len(rows)-1] = center.Render(hint)

	return theme.OverlayFrame(m.roles).Padding(1, 2).Render(strings.Join(rows, "\n"))
}

// renderButtons paints the button row with the highlighted button marked
// TWICE: by DialogButtonActive (a reversed-color block) and by literal
// bracket glyphs around its label. The brackets are the part that
// survives a monochrome terminal, a NO_COLOR theme or a screen reader —
// and since Enter accepts whatever is highlighted, "which button is
// highlighted" is the single most consequential thing this dialog shows.
// Both states pad to the same width so the row does not jitter as the
// selection moves.
//
// The accelerator is drawn into the label under the same rule and for the
// same reason: parentheses survive everything colour does not.
func (m Model) renderButtons() string {
	var row string
	for i, b := range m.buttons {
		style := theme.OverlayBody(m.roles)
		label := accelLabel(b)
		text := "  " + label + "  "
		if i == m.buttonIdx {
			style = theme.OverlaySelected(m.roles)
			text = "[ " + label + " ]"
		}
		if i > 0 {
			row += "  "
		}
		row += style.Render(text)
	}
	return row
}

// accelLabel marks a button's accelerator inside its label: the first
// occurrence of the letter is parenthesised ("For me" with accelerator m
// becomes "For (m)e"), and a label that does not contain its accelerator at
// all carries it on the end instead ("Confirm (y)").
//
// The letter is chosen for the keyboard first and the label second — y and n
// answer a confirm because that is what a person types at a yes/no question
// — so the label has to accommodate a letter it may not contain, rather than
// the accelerator being whatever the label happens to start with.
func accelLabel(b Button) string {
	if b.Accel == "" {
		return b.Label
	}
	if i := strings.Index(strings.ToLower(b.Label), strings.ToLower(b.Accel)); i >= 0 {
		n := len(b.Accel)
		return b.Label[:i] + "(" + b.Label[i:i+n] + ")" + b.Label[i+n:]
	}
	return b.Label + " (" + b.Accel + ")"
}

// renderHint spells out the keys the dialog actually honors, inside the
// dialog. Without it, "enter accepts the highlighted button" is a rule a
// user can only discover by losing something to it.
//
// It is built FROM the button set rather than written out beside it, so a
// dialog with three answers advertises three accelerators and cannot come to
// describe a set of buttons it no longer has.
//
// It names ONLY keys that reach this component in the running app. tab is
// honored by Update but omitted because the app's focus cycling consumes it
// first, and j/k are not movement keys at all — a hint that lists keys the
// dialog does not actually get is the same drift this wave removed from the
// status bar and the README.
func (m Model) renderHint() string {
	if len(m.buttons) < 2 {
		// One answer: nothing to choose between.
		return theme.OverlayMuted(m.roles).Render("enter or esc: dismiss")
	}

	parts := make([]string, 0, 4)
	if accels := m.accelerators(); accels != "" {
		parts = append(parts, accels+": answer")
	}
	parts = append(parts, "←/→: choose", "enter: accept", "esc: cancel")
	return theme.OverlayMuted(m.roles).Render(strings.Join(parts, " · "))
}

// Accelerators is the answer keys in button order: "n/y" for a confirm,
// "n/m/e" for the delete choice, "" for a dialog whose buttons carry none.
//
// Exported because the frame's hint bar has to name the same letters this
// dialog's own line does, and the button set is the only thing that knows
// what they are. The bar used to say "y/n" for every dialog, which was
// wrong on the delete choice — advertising an inert y on the one surface
// where a wrong key press is destructive.
func (m Model) Accelerators() string { return m.accelerators() }

// accelerators is the answer keys in button order: "n/y" for a confirm,
// "n/m/e" for the delete choice.
func (m Model) accelerators() string {
	var keys []string
	for _, b := range m.buttons {
		if b.Accel != "" {
			keys = append(keys, b.Accel)
		}
	}
	return strings.Join(keys, "/")
}

// moveButton moves the highlight by delta, wrapping.
func (m *Model) moveButton(delta int) {
	n := len(m.buttons)
	if n == 0 {
		return
	}
	m.buttonIdx = ((m.buttonIdx+delta)%n + n) % n
}
