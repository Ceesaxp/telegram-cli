package statusbar

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// Model is the status bar component.
type Model struct {
	store        *store.Store
	theme        *theme.Theme
	width        int
	connected    bool
	userName     string
	typing       map[int64][]int64 // chatID -> userIDs typing
	unreadCount  int32
	unmutedCount int32
	activeChatId int64

	// hints is the keybind hint strip, supplied by the caller via
	// SetHints; "" renders no strip at all.
	//
	// There is deliberately NO built-in default. This component cannot
	// know the keymap — the bindings are configurable and resolved in
	// internal/app — so any constant here would be a second, silently
	// diverging copy of the key table, which is the exact drift
	// internal/app/keymap.go generates helpSections() to avoid. The app
	// sets this from its own resolvedKeys (Model.statusHints) in New, so
	// a rebound key can never leave the bar advertising the old one.
	hints string
}

// New creates a new status bar model.
func New(s *store.Store, th *theme.Theme) Model {
	return Model{
		store:     s,
		theme:     th,
		connected: false,
		typing:    make(map[int64][]int64),
	}
}

// SetSize sets the component width.
func (m *Model) SetSize(width int) {
	m.width = width
}

// SetUserName sets the current user's display name.
func (m *Model) SetUserName(name string) {
	m.userName = name
}

// SetConnected sets the connection indicator directly.
func (m *Model) SetConnected(connected bool) {
	m.connected = connected
}

// SetHints sets the keybind hint strip, which callers build from their
// own resolved keys. An empty string means "no hints" — the bar then
// shows only the connection state, the user name and the unread badge.
// Callers that want a hint strip must supply one; this component has no
// default of its own (see the hints field).
//
// The text is a single line of already-formatted "key:action" pairs; the
// status bar styles it, budgets it against the available width (it is
// the first block dropped when the bar does not fit — see View) and
// never parses it.
func (m *Model) SetHints(text string) {
	m.hints = text
}

// SetActiveChatId sets the currently viewed chat.
func (m *Model) SetActiveChatId(chatID int64) {
	m.activeChatId = chatID
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case telegram.ConnectionStateMsg:
		m.connected = msg.State == telegram.ConnectionStateReady

	case telegram.ChatActionMsg:
		if msg.UserId != 0 {
			users := m.typing[msg.ChatId]
			// Add user if typing, remove if stopped.
			switch msg.Action.(type) {
			case *telegram.ChatActionTyping:
				found := false
				for _, uid := range users {
					if uid == msg.UserId {
						found = true
						break
					}
				}
				if !found {
					m.typing[msg.ChatId] = append(users, msg.UserId)
				}
			case *telegram.ChatActionCancel:
				filtered := users[:0]
				for _, uid := range users {
					if uid != msg.UserId {
						filtered = append(filtered, uid)
					}
				}
				m.typing[msg.ChatId] = filtered
			}
		}

	case telegram.UnreadCountMsg:
		m.unreadCount = msg.UnreadCount
		m.unmutedCount = msg.UnreadUnmutedCount
	}

	return m, nil
}

// TypingIndicator returns a typing indicator string for the active chat.
func (m Model) TypingIndicator() string {
	users, ok := m.typing[m.activeChatId]
	if !ok || len(users) == 0 {
		return ""
	}

	var names []string
	for _, uid := range users {
		names = append(names, m.store.Users.DisplayName(uid))
	}

	if len(names) == 1 {
		return fmt.Sprintf("%s is typing...", names[0])
	}
	return fmt.Sprintf("%s are typing...", strings.Join(names, ", "))
}

// View renders the status bar.
func (m Model) View() string {
	// Connection status
	connStatus := m.theme.StatusBarConnected.Render("● Connected")
	if !m.connected {
		connStatus = m.theme.StatusBar.Foreground(m.theme.Error).Render("● Disconnected")
	}

	// Typing indicator
	typingText := ""
	if indicator := m.TypingIndicator(); indicator != "" {
		typingText = m.theme.StatusBarTyping.Render(indicator)
	}

	// User name
	userName := m.theme.StatusBar.Render(m.userName)

	// Unread count. The headline number prefers the unmuted count (what
	// actually warrants attention); when muted chats contribute
	// additional unread messages, the true total is shown in parens.
	// Two renderings are kept so the layout below can fall back to the
	// shorter form (or drop it entirely) at narrow widths.
	unreadShort := ""
	unreadLong := ""
	if m.unreadCount > 0 {
		unreadShort = m.theme.StatusBar.Foreground(m.theme.Primary).
			Render(fmt.Sprintf(" [%d unread]", m.unreadCount))
		if m.unmutedCount != m.unreadCount {
			unreadLong = m.theme.StatusBar.Foreground(m.theme.Primary).
				Render(fmt.Sprintf(" [%d unread (%d total)]", m.unmutedCount, m.unreadCount))
		} else {
			unreadLong = unreadShort
		}
	}

	// Keybind hints. The text is entirely the caller's (SetHints, built
	// from the app's resolved keys so rebinds show up here); this
	// component only styles and budgets it. An unset strip must stay a
	// genuinely empty string rather than a styled one: StatusBar carries
	// PaddingLeft(1)+PaddingRight(1), so Render("") would still occupy
	// two cells and push the layout below off by that much.
	hintsText := ""
	if m.hints != "" {
		hintsText = m.theme.StatusBar.Foreground(m.theme.TextMuted).Render(m.hints)
	}

	center := typingText
	centerW := cell.MaxWidth(center)

	// Every width computed and fitted below targets innerWidth — m.width
	// minus StatusBar's own horizontal padding — never m.width itself.
	// StatusBar carries PaddingLeft(1)+PaddingRight(1); content built to
	// fill m.width exactly and then handed to THAT style's Width(m.width)
	// silently word-wraps instead of rendering as one line (see
	// cell.FitLine's doc comment for the lipgloss behavior behind
	// this). Piling MaxHeight(1) on top of that wrap, as this function
	// used to, is what produced the actual visible bug: it kept only the
	// wrapped line's first fragment, chopping the hints off mid-token
	// ("←→/1-") rather than showing the full string or a clean
	// truncation of it.
	innerWidth := m.width - m.theme.StatusBar.GetHorizontalFrameSize()
	if innerWidth < 0 {
		innerWidth = 0
	}

	fits := func(left, right string) bool {
		if innerWidth <= 0 {
			return true
		}
		return cell.MaxWidth(left)+centerW+cell.MaxWidth(right) <= innerWidth
	}

	// Assemble the bar at decreasing levels of detail — full, then with
	// the unread badge progressively shortened, then with the hints
	// block dropped entirely — using the first combination that fits the
	// available width. This mirrors the fallback order "full -> drop
	// unread -> drop hints": the hints block (caller-supplied via
	// SetHints, so of unbounded length — the app's own strip is 50+
	// cells) can exceed a narrow terminal, and unlike a fixed-width
	// truncation this keeps whichever piece still fits legible rather
	// than clipping it mid-word.
	unreadCandidates := []string{unreadLong, unreadShort, ""}
	hintsCandidates := []string{hintsText, ""}

	var left, right string
	found := false
	for _, h := range hintsCandidates {
		for _, u := range unreadCandidates {
			candidateLeft := fmt.Sprintf("%s  %s%s", connStatus, userName, u)
			if fits(candidateLeft, h) {
				left, right = candidateLeft, h
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		// Nothing fit even at minimum detail; fall through to the
		// narrowest form and let FitLine's own truncation below do the
		// rest.
		left = fmt.Sprintf("%s  %s", connStatus, userName)
		right = ""
	}

	// Calculate padding
	leftW := cell.MaxWidth(left)
	rightW := cell.MaxWidth(right)
	padding := innerWidth - leftW - centerW - rightW
	if padding < 0 {
		padding = 0
	}

	pad1 := padding / 2
	pad2 := padding - pad1

	bar := left + strings.Repeat(" ", pad1) + center + strings.Repeat(" ", pad2) + right

	// FitLine is both the final truncation safety net (in case some
	// future change to the pieces above doesn't land exactly on
	// innerWidth) and the only place in this function allowed to call
	// StatusBar.Width, since it budgets against the style's own frame
	// size first — the same shared helper list.go's rows and the folder
	// tab bar use to avoid this exact class of bug.
	return cell.FitLine(m.theme.StatusBar, bar, m.width)
}
