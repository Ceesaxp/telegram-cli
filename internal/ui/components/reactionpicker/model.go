// Package reactionpicker is the one-row chooser that `+` opens over the
// cursored message.
//
// A row rather than a palette command. Reacting is a thing you do TO a
// message, and the message is already under the cursor — routing it through
// `:` would mean naming the message a second time, in a surface that has no
// idea which one you meant.
//
// A fixed set rather than the account's own. Telegram serves a global list
// and lets a chat narrow it, so the honest set for a chat is two requests
// away, and a chooser that has to load is a chooser you press and then wait
// at. These twelve are Telegram's own defaults, in its own order; a chat
// that has narrowed its set refuses the ones it does not allow, and the
// refusal says so.
package reactionpicker

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/keys"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// Reactions is the set offered, in Telegram's own order of popularity.
//
// Twelve, because the row has to fit a narrow thread and because a chooser
// long enough to need scrolling has stopped being quicker than typing.
var Reactions = []string{"👍", "👎", "❤", "🔥", "🎉", "🤩", "😁", "🤔", "😢", "🙏", "👏", "🤯"}

// ChosenMsg is a reaction the user picked. Emoji is empty when they chose
// the one they had already left, which Telegram models as removing it.
type ChosenMsg struct {
	ChatId    int64
	MessageId int64
	Emoji     string
}

// CancelledMsg is the picker closing without a choice.
type CancelledMsg struct{}

// Model is the picker.
type Model struct {
	roles theme.Roles
	width int

	visible   bool
	chatID    int64
	messageID int64
	index     int

	// mine is the reaction already on this message from this account, so
	// choosing it again can mean "take it off" rather than "put it on
	// twice" — which is what Telegram does with a second identical send,
	// and what every other client's picker does on the same press.
	mine string
}

func New(r theme.Roles) Model { return Model{roles: r} }

func (m *Model) SetRoles(r theme.Roles) { m.roles = r }
func (m *Model) SetWidth(w int)         { m.width = w }
func (m Model) IsVisible() bool         { return m.visible }

// Open points the picker at a message. mine is the reaction this account has
// already left on it, or "".
func (m *Model) Open(chatID, messageID int64, mine string) {
	m.visible = true
	m.chatID = chatID
	m.messageID = messageID
	m.mine = mine
	m.index = 0

	// Start on the one you already left, so the press that opens the picker
	// and the press that confirms it take it back off.
	for i, r := range Reactions {
		if r == mine {
			m.index = i
			break
		}
	}
}

func (m *Model) Close() { m.visible = false }

// Update handles a keypress while the picker is open.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	press, ok := msg.(tea.KeyPressMsg)
	if !ok || !m.visible {
		return m, nil
	}
	kp := keys.NewPress(press)

	switch {
	// Esc, and ctrl+c because a row that owns the keyboard must not be
	// able to trap someone. NOT q (decision I-8): q is quit, and an
	// overlay that answered to it made cancelling and quitting one
	// keystroke apart. The row is a picker over the message under the
	// cursor — every printable it does not claim is swallowed, so q here
	// is simply inert.
	case kp.Matches("esc", "ctrl+c"):
		m.visible = false
		return m, func() tea.Msg { return CancelledMsg{} }

	case kp.Matches("left", "h"):
		m.index = (m.index - 1 + len(Reactions)) % len(Reactions)
		return m, nil

	case kp.Matches("right", "l"):
		m.index = (m.index + 1) % len(Reactions)
		return m, nil

	case kp.Matches("enter", "space", " "):
		return m, m.choose(Reactions[m.index])
	}

	// The emoji themselves are not typeable on most keyboards, so the row
	// is also numbered: 1-9 then 0, which is as far as ten digits go.
	if digit := digitIndex(press.String()); digit >= 0 && digit < len(Reactions) {
		m.index = digit
		return m, m.choose(Reactions[digit])
	}
	return m, nil
}

// choose closes the picker and reports the choice, as a removal when it is
// the one already there.
func (m *Model) choose(emoji string) tea.Cmd {
	m.visible = false
	chosen := ChosenMsg{ChatId: m.chatID, MessageId: m.messageID, Emoji: emoji}
	if emoji == m.mine {
		chosen.Emoji = ""
	}
	return func() tea.Msg { return chosen }
}

// digitIndex maps the number keys onto positions in the row: 1-9 then 0.
// Anything else is -1.
func digitIndex(key string) int {
	if len(key) != 1 {
		return -1
	}
	switch {
	case key[0] == '0':
		return 9
	case key[0] >= '1' && key[0] <= '9':
		return int(key[0] - '1')
	}
	return -1
}

// View draws the row.
func (m Model) View() string {
	if !m.visible {
		return ""
	}
	r := m.roles

	frame := lipgloss.NewStyle().Foreground(r.Ghost)
	selected := lipgloss.NewStyle().Foreground(r.Cyan).Bold(true)
	mine := lipgloss.NewStyle().Foreground(r.Green)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(r.Dim).Render(" react "))

	for i, emoji := range Reactions {
		style := frame
		switch {
		case i == m.index:
			style = selected
		case emoji == m.mine:
			style = mine
		}
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(style.Render("[" + emoji + "]"))
	}

	hint := " enter pick · esc cancel "
	if m.mine != "" {
		hint = " enter takes yours off · esc cancel "
	}
	b.WriteString(lipgloss.NewStyle().Foreground(r.Faint).Render(hint))

	return cell.Fit(b.String(), m.width)
}

// Width is what the row reserves, budgeted with cell.Reserve because every
// cell of it is an emoji — see the reaction chips in internal/render for why
// the tables and the terminal disagree about those.
func Width() int {
	total := len(" react ")
	for _, emoji := range Reactions {
		total += cell.Reserve("["+emoji+"] ") + 1
	}
	return total
}
