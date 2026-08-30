package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/composer"
	"github.com/imtaqin/telegram-cli/internal/ui/components/hintbar"
	"github.com/imtaqin/telegram-cli/internal/ui/components/topbar"
	"github.com/imtaqin/telegram-cli/internal/ui/frame"
)

// renderMainScreen draws the TUI 2.0 frame: a one-row top bar, a borderless
// body of one-cell-ruled columns, and a one-row hint bar.
//
// The panels still render their own current content. The frame fits each
// line to its region, so a panel that is not yet exact-width is padded or
// clipped instead of tearing the screen — see internal/ui/frame for why that
// inversion is what makes the redesign safe to land in pieces.
func (m Model) renderMainScreen() string {
	l := m.layout

	// Left column: contacts borrows the chat list's region when open.
	//
	// m.chatList.View() is intentionally skipped while contacts is showing:
	// its dirty flag just accumulates and gets cleared the next time the
	// chat list actually renders. Self-healing, verified harmless — do not
	// "fix" by rendering it unseen just to clear it.
	leftView := m.chatList.View()
	if m.contacts.IsVisible() {
		leftView = m.contacts.View()
	}

	threadLines := frame.Lines(m.chatView.View())
	// The composer sits at the foot of the thread column, so its rows come
	// out of the thread's budget rather than the body's.
	for len(threadLines) < l.ThreadHeight {
		threadLines = append(threadLines, "")
	}
	if len(threadLines) > l.ThreadHeight {
		threadLines = threadLines[:l.ThreadHeight]
	}
	threadLines = append(threadLines, frame.Lines(m.composer.View())...)

	body := frame.Screen{
		Layout:  l,
		Roles:   m.roles,
		TopBar:  m.topBar.View(),
		HintBar: m.hintBar.View(),
		ChatList: frame.Column{
			Width: l.ChatListWidth,
			Lines: frame.Lines(leftView),
		},
		Thread: frame.Column{
			Width: l.ThreadWidth,
			Lines: threadLines,
		},
	}

	// In single-panel mode one column owns the width, and which one depends
	// on focus. The frame reads ChatList for it, so put the right content
	// there rather than teaching the frame about focus.
	if l.SinglePanel {
		switch m.focus {
		case PanelChatList, PanelContacts:
			body.ChatList.Lines = frame.Lines(leftView)
		default:
			body.ChatList.Lines = threadLines
		}
		body.ChatList.Width = l.Width
	}

	return frame.Render(body)
}

// bodyRow converts an absolute screen row into a body row, or -1 when the
// row is chrome. The top bar shifts every body row down by one, so any
// hit-test written against raw Y would be off by exactly that much — which
// is the kind of bug that only shows up as "clicks land one row high".
func (m Model) bodyRow(y int) int {
	row := y
	if m.layout.TopBar {
		row--
	}
	if row < 0 || row >= m.layout.BodyHeight {
		return -1
	}
	return row
}

// notify shows a transient message.
//
// It goes to the hint bar, which the design gives to a transient error or
// progress notice (docs/tui-2.0.md). It also still goes to the composer,
// because the composer keeps its own notice line in the expanded form and
// several tests read it there — but the hint bar is what the user sees in
// the one-row frame, since the composer's inline form has no spare row.
//
// Every notice in the app routes through here rather than calling either
// component directly, so a new message cannot land in only one of the two.
func (m *Model) notify(text string) {
	m.composer.SetNotice(text)

	kind := "info"
	if strings.HasPrefix(text, "⚠") {
		kind = "error"
	}
	m.hintBar.SetNotice(text, kind)
}

// refreshChrome updates the two chrome rows from current state. Called
// whenever something they display changes, rather than rebuilt inside View —
// View must stay a pure function of the model.
func (m *Model) refreshChrome() {
	m.topBar.SetWidth(m.width)
	m.topBar.SetFolders(m.topBarFolders())
	m.topBar.SetClock(time.Now().Format("15:04"))

	// DEFERRED (decision 7): neither value has a source in the current
	// connection state. These literals exist so the layout and shrink order
	// are settled; they must be wired or removed before release, and TODO.md
	// tracks that as a release blocker. Presenting a hard-coded transport
	// version as live status would be a lie in the UI.
	m.topBar.SetPlaceholders("mtproto 2.0", "devices 1")

	m.hintBar.SetWidth(m.width)
	m.hintBar.SetHints(m.hintsForMode())
	m.hintBar.SetRight(m.hintBarCounters())

	// The badge and the hint bar read the same resolver, in the same place,
	// on the same tick. Decision 3 requires the badge to describe key
	// routing rather than alter it, and two surfaces describing it from one
	// call is what makes disagreeing between them impossible.
	m.composer.SetMode(composerMode(m.Mode()))
}

// composerMode projects the app's interaction mode onto the composer's own
// badge enum.
//
// Two enums rather than a shared type: the app imports the composer and not
// the other way round, and a third package existing to hold three constants
// would cost more than this switch. TestComposerModeIsExhaustive walks every
// InteractionMode so a fourth one cannot land here as a silent NORMAL.
func composerMode(mode InteractionMode) composer.AppMode {
	switch mode {
	case ModeInsert:
		return composer.AppInsert
	case ModeCommand:
		return composer.AppCommand
	default:
		return composer.AppNormal
	}
}

// topBarFolders projects the chat list's folder model into the top bar's
// display type. Selection and key handling stay in chatlist; only the
// drawing moved.
func (m Model) topBarFolders() []topbar.Folder {
	names := m.chatList.FolderNames()
	active := m.chatList.ActiveFolderIndex()

	out := make([]topbar.Folder, 0, len(names))
	for i, n := range names {
		out = append(out, topbar.Folder{Name: n, Active: i == active})
	}
	return out
}

// hintsForMode returns the hint set for the current interaction mode.
//
// The set is ORDERED and the bar keeps the longest prefix that fits, so the
// order here is a priority ranking, not a cosmetic choice: whatever is last
// is what disappears first on a narrow terminal.
func (m Model) hintsForMode() []hintbar.Hint {
	switch m.Mode() {
	case ModeInsert:
		return []hintbar.Hint{
			{Key: "enter", Label: "send"},
			{Key: "esc", Label: "leave"},
			{Key: "ctrl+j", Label: "newline"},
			{Key: "ctrl+t", Label: "attach"},
			{Key: "ctrl+o", Label: "editor"},
		}
	case ModeCommand:
		return []hintbar.Hint{
			{Key: "enter", Label: "run"},
			{Key: "tab", Label: "complete"},
			{Key: "esc", Label: "cancel"},
		}
	default:
		return []hintbar.Hint{
			{Key: m.keys.quitBrowsing, Label: "quit"},
			{Key: "i", Label: "compose"},
			{Key: ":", Label: "command"},
			{Key: m.keys.reply, Label: "reply"},
			{Key: m.keys.editMessage, Label: "edit"},
			{Key: m.keys.help, Label: "keymap"},
		}
	}
}

// hintBarCounters is the right-hand group: how much there is, rather than
// what can be pressed.
func (m Model) hintBarCounters() string {
	return fmt.Sprintf("%d buffers", m.chatList.Count())
}

// topBarConnState maps the client's connection state onto the dot.
func topBarConnState(s telegram.ConnectionState) (topbar.ConnState, string) {
	switch s {
	case telegram.ConnectionStateReady:
		return topbar.Connected, "connected"
	case telegram.ConnectionStateConnecting:
		return topbar.Connecting, "connecting"
	default:
		return topbar.Disconnected, "disconnected"
	}
}
