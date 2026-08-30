package dialog

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
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
//
// It starts highlighted on OK, unlike NewConfirm — buttonIdx 1, not 0.
// A prompt COLLECTS something (the attach-file path); accepting what the
// user just typed is not destructive, and Enter is the universal reflex
// at the end of typing. Defaulting it to Cancel meant "type /tmp/a.png,
// press Enter" silently threw the path away and the only way to attach
// was type -> arrow -> Enter, which nothing on screen suggested.
//
// The Cancel-by-default rule that NewConfirm keeps is a guard for
// DESTRUCTIVE actions (delete for everyone, quit with an unsent draft),
// where the cost of a reflex Enter is asymmetric. That asymmetry does
// not exist here: the worst case of a reflex Enter on a prompt is an
// empty path, which the app already ignores.
func NewPrompt(th *theme.Theme, id, title, message string) Model {
	return Model{
		theme:     th,
		visible:   true,
		kind:      KindPrompt,
		id:        id,
		title:     title,
		message:   message,
		buttons:   []string{"Cancel", "OK"},
		buttonIdx: 1,
	}
}

// IsVisible returns whether the dialog is visible.
func (m Model) IsVisible() bool {
	return m.visible
}

// Kind reports which sort of dialog this is.
//
// The distinction that matters to callers is [KindPrompt] versus the rest: a
// prompt collects text, so printable keys are typed into it, while a confirm
// or an alert treats the same keys as navigation. The app's interaction-mode
// resolver needs exactly that difference to say whether the next letter will
// type or navigate.
func (m Model) Kind() Kind { return m.kind }

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
		// everyone. In a prompt they would be worse than meaningless,
		// since that dialog's input owns every printable (a file path
		// may well contain a j) and they fall through to the text
		// branch below.
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
			// is never a shortcut for a particular button. What differs
			// per kind is where the highlight STARTS (see the New*
			// constructors): a confirm starts on Cancel, because a
			// reflex Enter must not delete a message or discard a draft;
			// a prompt starts on OK, because a reflex Enter after typing
			// must not throw away what was typed. The View below makes
			// the highlighted button unmistakable and spells the keys
			// out, so either default is visible before Enter is pressed.
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
		if w := cell.MaxWidth(r); w > width {
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
//
// It names ONLY keys that reach this component in the running app. tab
// is honored by Update but omitted here because the app's focus cycling
// consumes it first, and j/k are not movement keys at all — a hint that
// lists keys the dialog does not actually get is the same drift this
// wave removed from the status bar and the README.
func (m Model) renderHint() string {
	hint := "←/→: choose · enter: accept · esc: cancel"
	switch {
	case len(m.buttons) <= 1:
		// An alert has nothing to choose between.
		hint = "enter or esc: dismiss"
	case m.kind == KindPrompt:
		// Enter is the reflex at the end of typing, and here it accepts
		// what was typed — lead with that rather than with movement.
		hint = "enter: accept input · ←/→: choose · esc: cancel"
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
