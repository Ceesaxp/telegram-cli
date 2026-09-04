package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/Ceesaxp/telegram-cli/internal/render"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/topbar"
	"github.com/Ceesaxp/telegram-cli/internal/ui/frame"
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

	m.reactions.SetWidth(m.width)

	// One resolution, one registry, every surface that draws a hint fed
	// from it on the same tick (decision I-6). The chat list footer, the
	// media overlay's row and the reaction row used to hold literals; a
	// literal cannot be wrong in a way anything can detect, which is how
	// "u unread" sat in the footer with nothing bound to u.
	surface := m.surface()
	m.hintBar.SetWidth(m.width)
	m.hintBar.SetHints(m.hintsFor(surface))
	m.hintBar.SetRight(m.hintBarCounters())
	m.chatList.SetFooterHints(m.footerHints())
	m.mediaView.SetHints(m.hintsFor(SurfaceMedia))
	m.reactions.SetHints(m.hintsFor(SurfaceReactions))

	// The badge reads the same surface, in the same place, on the same
	// tick. Decision 3 requires the badge to describe key routing rather
	// than alter it, and one call answering for both is what makes
	// disagreeing between them impossible.
	m.composer.SetMode(composerMode(surface.Mode()))
}

// composerMode projects the app's interaction mode onto the composer's own
// badge enum.
//
// Two enums rather than a shared type: the app imports the composer and not
// the other way round, and a third package existing to hold four constants
// would cost more than this switch. TestComposerModeIsExhaustive walks every
// InteractionMode so a fifth one cannot land here as a silent NORMAL.
func composerMode(mode InteractionMode) composer.AppMode {
	switch mode {
	case ModeInsert:
		return composer.AppInsert
	case ModeVi:
		return composer.AppVi
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
//
// The buffer count says what it is a count OF while a filter is applied.
// It has always followed the filter — Count is the rendered list — so
// filtering already dropped it from twelve to three; what it did not do was
// explain itself, and a number that falls on its own reads as chats going
// missing. "3 of 12" is the shape the chat list's own filter header already
// draws in that column, and the attach picker's prompt row after it.
//
// Derived here rather than pushed in. Nothing announces a filter change: the
// list applies it locally and this runs on the next tick, which is a refresh
// that cannot be forgotten and cannot go stale. See chatlist/messages.go.
func (m Model) hintBarCounters() string {
	var parts []string

	if n := m.store.Messages.Count(m.chatView.ChatId()); n > 0 {
		parts = append(parts, fmt.Sprintf("idx %d msgs", n))
	}
	parts = append(parts, m.bufferCount())
	if n := m.store.Chats.TotalUnread(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d unread", n))
	}
	return strings.Join(parts, " · ")
}

// bufferCount is the chat-list count, qualified while the list is filtered.
//
// The test is the applied QUERY, not FilterActive: that reports whether the
// input is open, and `enter` closes the input while leaving the filter on.
// That state — filtered, but with no cursor blinking in the header to say
// why — is exactly the one where a bare "3 buffers" misleads, so it is the
// state this must cover rather than the one it must not.
//
// The total is chatlist.TotalCount, which is the denominator the filter
// header draws, so the two surfaces cannot disagree about the same list.
func (m Model) bufferCount() string {
	shown := m.chatList.Count()
	if m.chatList.FilterQuery() == "" {
		return fmt.Sprintf("%d buffers", shown)
	}
	if total := m.chatList.TotalCount(); total > shown {
		return fmt.Sprintf("%d of %d buffers", shown, total)
	}
	// A filter that excludes nothing is not worth six cells saying so.
	return fmt.Sprintf("%d buffers", shown)
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
