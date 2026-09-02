package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/imtaqin/telegram-cli/internal/render"
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
			Width:   l.ChatListWidth,
			Surface: m.roles.Panel,
			Lines:   frame.Lines(leftView),
		},
		Thread: frame.Column{
			Width:   l.ThreadWidth,
			Surface: m.roles.Bg,
			Lines:   threadLines,
		},
		Rail: frame.Column{
			Width:   l.RailWidth,
			Surface: m.roles.Panel,
			Lines:   frame.Lines(m.rail.View()),
		},
	}

	// In single-panel mode one column owns the width, and which one depends
	// on focus. The frame reads ChatList for it, so put the right content
	// there rather than teaching the frame about focus.
	if l.SinglePanel {
		switch m.focus {
		case PanelChatList, PanelContacts:
			body.ChatList.Lines = frame.Lines(leftView)
			body.ChatList.Surface = m.roles.Panel
		default:
			body.ChatList.Lines = threadLines
			// The thread is drawn on bg wherever it is drawn. Leaving the
			// surface as panel here would make one column of the app change
			// colour purely because the terminal got narrow.
			body.ChatList.Surface = m.roles.Bg
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
	m.noticeAt = time.Now()
}

// refreshChrome updates the two chrome rows from current state. Called
// whenever something they display changes, rather than rebuilt inside View —
// View must stay a pure function of the model.
func (m *Model) refreshChrome() {
	m.topBar.SetWidth(m.width)
	m.topBar.SetFolders(m.topBarFolders())
	// render.Now rather than time.Now: this is the one wall clock on
	// screen, and a golden frame that cannot fix it cannot be asserted.
	m.topBar.SetClock(render.Now().Format("15:04"))

	// Zero until account.getAuthorizations answers, and zero drops the
	// cell — the top bar says nothing about devices rather than guessing at
	// a number. Decision 7's other placeholder, the transport version, is
	// gone rather than wired: gotd speaks MTProto 2.0 and nothing else.
	m.topBar.SetDevices(m.deviceCount)

	// The header's buffer number is the chat list's row, and the list
	// reorders as messages arrive. Refreshed on the same tick as the two
	// chrome rows so it cannot be left describing where a chat used to be.
	m.chatView.SetBufferIndex(m.chatList.BufferIndex(m.chatView.ChatId()))

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
			{Key: "y", Label: "yank"},
			{Key: m.keys.editMessage, Label: "edit"},
			{Key: m.keys.help, Label: "keymap"},
		}
	}
}

// hintBarCounters is the right-hand group: how much there is, rather than
// what can be pressed.
//
// Three counts, and each is omitted when it would say nothing. Before any
// chat is open there is no history to size, and a client with nothing
// unread should not spend six cells saying "0 unread" — the interesting
// state is the one where the number is not zero.
//
// The bar cuts from the LEFT if it has to, so the order is a priority
// ranking: what is unread outlives how many chats there are, which outlives
// the size of the history you are already looking at.
func (m Model) hintBarCounters() string {
	var parts []string

	if n := m.store.Messages.Count(m.chatView.ChatId()); n > 0 {
		parts = append(parts, fmt.Sprintf("idx %d msgs", n))
	}
	parts = append(parts, fmt.Sprintf("%d buffers", m.chatList.Count()))
	if n := m.store.Chats.TotalUnread(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d unread", n))
	}
	return strings.Join(parts, " · ")
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
