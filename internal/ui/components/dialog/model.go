package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// DialogResultMsg is emitted when a dialog is closed.
type DialogResultMsg struct {
	ID        string
	Confirmed bool
	Input     string
}

// Kind represents the type of dialog.
type Kind int

const (
	KindConfirm Kind = iota
	KindPrompt
	KindAlert
)

// Model is a modal dialog component.
type Model struct {
	theme     *theme.Theme
	visible   bool
	kind      Kind
	id        string
	title     string
	message   string
	input     string
	cursor    int
	buttonIdx int
	buttons   []string
	width     int
	height    int
}

// NewConfirm creates a confirmation dialog.
func NewConfirm(th *theme.Theme, id, title, message string) Model {
	return Model{
		theme:   th,
		visible: true,
		kind:    KindConfirm,
		id:      id,
		title:   title,
		message: message,
		buttons: []string{"Cancel", "Confirm"},
	}
}

// NewAlert creates an alert dialog.
func NewAlert(th *theme.Theme, id, title, message string) Model {
	return Model{
		theme:   th,
		visible: true,
		kind:    KindAlert,
		id:      id,
		title:   title,
		message: message,
		buttons: []string{"OK"},
	}
}

// NewPrompt creates a prompt dialog with text input.
func NewPrompt(th *theme.Theme, id, title, message string) Model {
	return Model{
		theme:   th,
		visible: true,
		kind:    KindPrompt,
		id:      id,
		title:   title,
		message: message,
		buttons: []string{"Cancel", "OK"},
	}
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
		switch msg.String() {
		case "esc":
			m.visible = false
			return m, func() tea.Msg {
				return DialogResultMsg{ID: m.id, Confirmed: false}
			}

		case "tab", "right":
			m.moveButton(1)

		case "left":
			m.moveButton(-1)

		case "j", "k":
			// The help card advertises j/k as "Move", so they have to
			// work. In a PROMPT dialog they stay text: that dialog's
			// input owns every printable (a file path may well contain
			// a j), exactly as the default branch below assumes.
			if m.kind == KindPrompt {
				m.input += msg.String()
				break
			}
			if msg.String() == "j" {
				m.moveButton(1)
			} else {
				m.moveButton(-1)
			}

		case "enter":
			// Enter accepts the HIGHLIGHTED button, whatever it is — it
			// is not a shortcut for "Confirm". A confirm dialog starts
			// on Cancel on purpose: these dialogs guard destructive or
			// lossy actions (deleting a message, quitting with a draft
			// in the composer), and a reflex Enter must not be what
			// performs one. The View below makes the highlighted button
			// unmistakable and spells the movement keys out, so the
			// behavior is discoverable rather than surprising.
			m.visible = false
			confirmed := m.buttonIdx == len(m.buttons)-1
			return m, func() tea.Msg {
				return DialogResultMsg{
					ID:        m.id,
					Confirmed: confirmed,
					Input:     m.input,
				}
			}

		case "backspace":
			if m.kind == KindPrompt && len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}

		default:
			if m.kind == KindPrompt && len(msg.String()) == 1 {
				m.input += msg.String()
			}
		}
	}

	return m, nil
}

// View renders the dialog.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	title := m.theme.DialogTitle.Render(m.title)
	message := lipgloss.NewStyle().Foreground(m.theme.Text).Render(m.message)

	rows := []string{title, "", message}
	if m.kind == KindPrompt {
		inputStyle := m.theme.AuthInput.Width(30)
		rows = append(rows, "", inputStyle.Render(m.input+"▏"))
	}
	buttons, hint := m.renderButtons(), m.renderHint()
	rows = append(rows, "", buttons, "", hint)

	// Center the button row and the hint under the widest row. The box
	// itself has no fixed width (DialogBox sizes to its content), so
	// centering needs that width computed here — the previous
	// Align(Center) with no Width was a no-op, leaving both left-aligned
	// under a message that is usually wider.
	width := 0
	for _, r := range rows {
		if w := lipgloss.Width(r); w > width {
			width = w
		}
	}
	center := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	rows[len(rows)-3] = center.Render(buttons)
	rows[len(rows)-1] = center.Render(hint)

	return m.theme.DialogBox.Render(strings.Join(rows, "\n"))
}

// renderButtons paints the button row with the highlighted button marked
// TWICE: by DialogButtonActive (a reversed-color block) and by literal
// bracket glyphs around its label. The brackets are the part that
// survives a monochrome terminal, a NO_COLOR theme or a screen reader —
// and since Enter accepts whatever is highlighted, "which button is
// highlighted" is the single most consequential thing this dialog shows.
// Both states pad to the same width so the row does not jitter as the
// selection moves.
func (m Model) renderButtons() string {
	var row string
	for i, label := range m.buttons {
		style := m.theme.DialogButton
		text := "  " + label + "  "
		if i == m.buttonIdx {
			style = m.theme.DialogButtonActive
			text = "[ " + label + " ]"
		}
		if i > 0 {
			row += "  "
		}
		row += style.Render(text)
	}
	return row
}

// renderHint spells out the keys the dialog actually honors, inside the
// dialog. Without it, "enter accepts the highlighted button" is a rule a
// user can only discover by losing something to it.
func (m Model) renderHint() string {
	hint := "←/→ or tab: choose · enter: accept"
	if len(m.buttons) <= 1 {
		hint = "enter: dismiss"
	}
	return lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render(hint)
}

// moveButton moves the highlight by delta, wrapping.
func (m *Model) moveButton(delta int) {
	n := len(m.buttons)
	if n == 0 {
		return
	}
	m.buttonIdx = ((m.buttonIdx+delta)%n + n) % n
}
