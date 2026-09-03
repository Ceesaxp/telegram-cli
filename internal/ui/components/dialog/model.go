package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// DialogResultMsg is emitted when a dialog is closed.
type DialogResultMsg struct {
	ID        string
	Confirmed bool
}

// Kind represents the type of dialog.
//
// Two of them, since the picker took the third. A prompt collected text into
// a centred box with an OK button, which is the idiom TUI 2.0 replaced
// everywhere else; internal/ui/components/attach is what asks for a path
// now. What is left is a yes/no and an acknowledgement — genuinely
// two-button and one-button questions, which is what a modal is for.
type Kind int

const (
	KindConfirm Kind = iota
	KindAlert
)

// Model is a modal dialog component.
type Model struct {
	roles     theme.Roles
	visible   bool
	kind      Kind
	id        string
	title     string
	message   string
	buttonIdx int
	buttons   []string
	width     int
	height    int
}

// NewConfirm creates a confirmation dialog.
func NewConfirm(r theme.Roles, id, title, message string) Model {
	return Model{
		roles:   r,
		visible: true,
		kind:    KindConfirm,
		id:      id,
		title:   title,
		message: message,
		buttons: []string{"Cancel", "Confirm"},
	}
}

// NewAlert creates an alert dialog.
func NewAlert(r theme.Roles, id, title, message string) Model {
	return Model{
		roles:   r,
		visible: true,
		kind:    KindAlert,
		id:      id,
		title:   title,
		message: message,
		buttons: []string{"OK"},
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
			m.visible = false
			confirmed := m.buttonIdx == len(m.buttons)-1
			return m, func() tea.Msg {
				return DialogResultMsg{ID: m.id, Confirmed: confirmed}
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
func (m Model) renderButtons() string {
	var row string
	for i, label := range m.buttons {
		style := theme.OverlayBody(m.roles)
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

// renderHint spells out the keys the dialog actually honors, inside the
// dialog. Without it, "enter accepts the highlighted button" is a rule a
// user can only discover by losing something to it.
//
// It names ONLY keys that reach this component in the running app. tab
// is honored by Update but omitted here because the app's focus cycling
// consumes it first, and j/k are not movement keys at all — a hint that
// lists keys the dialog does not actually get is the same drift this
// wave removed from the status bar and the README.
func (m Model) renderHint() string {
	if m.kind == KindAlert {
		// An alert has nothing to choose between.
		return theme.OverlayMuted(m.roles).Render("enter or esc: dismiss")
	}
	return theme.OverlayMuted(m.roles).
		Render("←/→: choose · enter: accept · esc: cancel")
}

// moveButton moves the highlight by delta, wrapping.
func (m *Model) moveButton(delta int) {
	n := len(m.buttons)
	if n == 0 {
		return
	}
	m.buttonIdx = ((m.buttonIdx+delta)%n + n) % n
}
