package chatlist

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/render"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

// Model is the chat list component.
type Model struct {
	// list is heap-allocated (rather than embedded by value) so that its
	// state stays consistent between Update (which returns a new Model by
	// value) and View (a read-only render pass on its own local copy):
	// mutating through the pointer is visible from every copy of Model,
	// value-receiver or not.
	list  *widgets.List
	store *store.Store
	tg    *telegram.Client
	roles theme.Roles

	// draftChats is the set of chats with unsent work parked in the
	// composer, projected in by the host. Read-only here.
	draftChats map[int64]bool

	// myUserID identifies Saved Messages: Telegram models it as the chat
	// with yourself, so it is only distinguishable from an ordinary DM by
	// comparing IDs. Zero until the app learns who we are, which just means
	// the row shows the DM sigil until then.
	myUserID int64
	width    int
	height   int
	focused  bool

	// filter is the applied local filter — a case-insensitive substring
	// match on the chat title, applied by refreshList within the active
	// folder; "" means no filter. It deliberately outlives the input:
	// `enter` closes filterInput but keeps filter applied, so the two are
	// separate pieces of state.
	//
	// filtering reports that the one-line filter input is open and
	// consuming key presses (see FilterActive); filterInput holds the
	// text being edited. filterJustOpened absorbs the keystroke that
	// opened the input, in case a caller both calls OpenFilter and
	// forwards that same key press here (see OpenFilter).
	filter           string
	filtering        bool
	filterInput      widgets.TextArea
	filterJustOpened bool

	loading      bool
	spinner      widgets.Spinner
	activeChatId int64

	// folders holds the user's chat folders, always with a synthesized or
	// server-provided "All" folder at index 0. activeFolder indexes into
	// it.
	folders      []*telegram.ChatFolder
	activeFolder int

	// dirty is heap-allocated for the same reason as list: it must be
	// observable and clearable from View (which only ever sees a
	// throwaway local copy of Model) as well as from Update.
	dirty *bool
}

// New creates a new chat list model.
func New(s *store.Store, tg *telegram.Client, r theme.Roles) Model {
	l := widgets.NewList()
	// Only StyleEmpty. Every other List style feeds the widget's own row
	// drawing, and this component supplies RenderRow — so the seven that
	// used to be set here were read by nothing at all. StyleEmpty is
	// different: the "No items" placeholder is drawn before RenderRow gets
	// a look in, so it is the one style a caller with a bespoke row still
	// owns.
	l.StyleEmpty = theme.OverlayMuted(r)

	sp := widgets.NewSpinner("Loading chats...")
	sp.Style = lipgloss.NewStyle().Foreground(r.Cyan)

	dirty := false

	// The filter input reuses the shared single-line TextArea so the
	// filter query gets the same rune-safe editing (printable-key
	// detection, backspace, ctrl+u/w, home/end) as every other text
	// surface in the app. It is rendered by renderFilterChip, not by
	// TextArea.View, because it shares one row with the folder tabs.
	fi := widgets.NewTextArea()
	fi.MultiLine = false

	m := Model{
		list:        &l,
		filterInput: fi,
		roles:       r,
		store:       s,
		tg:          tg,
		loading:     true,
		spinner:     sp,
		folders:     []*telegram.ChatFolder{defaultAllFolder()},
		dirty:       &dirty,
	}
	// The row renderer is installed here, not in SetRoles: a component
	// that renders two different ways depending on whether the caller
	// remembered a setter is a component with two behaviours, and the
	// tests construct this model directly.
	m.list.RenderRow = m.renderRow
	return m
}

// defaultAllFolder synthesizes the implicit "All chats" folder so the tab
// bar always has something to show, even before the folder list has
// loaded and even if the account has no custom folders.
func defaultAllFolder() *telegram.ChatFolder {
	return &telegram.ChatFolder{ID: telegram.AllChatsFolderID, Title: "All", Emoticon: "💬"}
}

// Init loads the initial chat list.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadChatsCmd(),
		m.loadFoldersCmd(),
		m.spinner.Tick(),
	)
}

func (m Model) loadChatsCmd() tea.Cmd {
	return func() tea.Msg {
		err := m.tg.LoadChats(50)
		if err != nil {
			return chatsLoadedMsg{err: err}
		}
		return chatsLoadedMsg{}
	}
}

// loadFoldersCmd fetches the user's chat folders. Folders are a purely
// optional refinement of the chat list, so a fetch error is swallowed
// silently: the synthesized "All" folder still lets the list render.
func (m Model) loadFoldersCmd() tea.Cmd {
	tg := m.tg
	return func() tea.Msg {
		folders, err := tg.GetChatFolders()
		if err != nil {
			return nil
		}
		return telegram.ChatFoldersMsg{Folders: folders}
	}
}

type chatsLoadedMsg struct {
	err error
}

// SetSize sets the component dimensions.
// SetDraftChats tells the list which chats hold unsent work, so their
// preview row can say so. The composer owns the drafts; this is a projection
// of them, refreshed by the host whenever they change.
func (m *Model) SetDraftChats(ids map[int64]bool) {
	m.draftChats = ids
	*m.dirty = true
}

func (m *Model) SetMyUserID(id int64) {
	m.myUserID = id
	*m.dirty = true
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.list.Width = width

	listHeight := height - m.headerHeight() - m.footerHeight()
	if listHeight < 0 {
		listHeight = 0
	}
	m.list.Height = listHeight
}

// headerHeight and footerHeight are the chat list's own chrome rows: the
// filter header above and the motion hints below.
//
// Unlike the folder tab bar they replaced, both are ALWAYS one row. The tab
// bar was 0 rows before folders loaded, which meant the list's height budget
// and ClickAt's row arithmetic changed underneath a running app; a constant
// removes that whole class of off-by-one.
func (m Model) headerHeight() int { return 1 }
func (m Model) footerHeight() int { return 1 }

// storeChatCount is the unfiltered total, for the header's "shown/total".
func (m Model) storeChatCount() int {
	if m.store == nil {
		return 0
	}
	return len(m.store.Chats.OrderedChats())
}

// tabBarHeight returns the number of rows reserved for the folder tab
// bar. Folders always contains at least the synthesized "All" entry, so
// this is 1 whenever the model has been constructed via New.
// tabBarHeight is intentionally NOT loading-aware, even though View()
// renders no tab bar while m.loading (it shows only the centered
// spinner over the full panel height). This value also feeds SetSize's
// list-height budget, and SetSize is only re-invoked by the app on
// window resize (see internal/app/app.go's WindowSizeMsg handling) —
// never on the loading -> loaded transition. Deriving this from
// m.loading would size the list for a tab-bar-less layout while
// loading, then never correct that budget once loading flips false and
// the tab bar starts rendering, reintroducing the exact row-overlap bug
// this offset exists to prevent. ClickAt instead guards m.loading
// itself (below) so a click during the loading state can never be
// misattributed regardless of this value.
func (m Model) tabBarHeight() int {
	if len(m.folders) == 0 {
		return 0
	}
	return 1
}

// ClickAt selects the chat shown at the given local row (inside the panel
// border) and returns its ID. ok is false when the row has no chat — this
// includes clicks that land on the folder tab bar, which occupies the
// first tabBarHeight() rows above the list, and any click while the
// initial chat load is still in flight (View() renders only the spinner
// then, with no tab bar and no clickable list — see the tabBarHeight
// comment above for why that state is handled here rather than by
// varying tabBarHeight itself).
//
// ClickAt has no column, so a click on the tab bar row is only ever a
// no-op here — it cannot tell which folder tab was hit. ClickAtXY is the
// column-aware companion that can, and is what the app calls; this is now
// the row-only half it delegates to.
func (m *Model) ClickAt(localY int) (chatID int64, ok bool) {
	if m.loading {
		return 0, false
	}
	row := localY - m.headerHeight()
	if row < 0 {
		return 0, false
	}
	idx := m.list.ItemAtRow(row)
	if idx < 0 || !m.list.SelectIndex(idx) {
		return 0, false
	}
	item := m.list.SelectedItem()
	if item == nil {
		return 0, false
	}
	if _, err := fmt.Sscanf(item.ID, "%d", &chatID); err != nil {
		return 0, false
	}
	m.activeChatId = chatID
	return chatID, true
}

// ClickAtXY is ClickAt's column-aware companion. x and y are both local
// to the panel's content area (inside its border) — the same coordinate
// space ClickAt already uses for y. A click whose row lands on the
// folder tab bar switches to whichever folder tab column x falls
// within (a miss between/past tabs is a no-op) and never selects a
// chat; a click on any row below the tab bar behaves exactly like
// ClickAt(y).
//
// This is what the app calls, from handleMouseClick in
// internal/app/app.go, with both coordinates made panel-local:
//
//	row, col := y-1, x-1
//	if chatID, ok := m.chatList.ClickAtXY(col, row); ok {
//
// It used to call ClickAt(row), which has no column to hit-test the tab
// bar with, so a click on a folder tab was silently swallowed instead of
// switching folders.
func (m *Model) ClickAtXY(x, y int) (chatID int64, ok bool) {
	if m.loading {
		return 0, false
	}
	// Row 0 is the filter header now, not the folder tab bar — the tabs
	// moved to the frame's top bar with TUI 2.0. Clicking here must NOT
	// fall through to folder selection, or clicking the filter row would
	// silently switch folders. Click-to-select-a-tab needs re-wiring at the
	// top bar, which owns those pixels now; until then this row is inert.
	if y < m.headerHeight() {
		return 0, false
	}
	return m.ClickAt(y)
}

// ScrollBy moves the selection by n items (negative scrolls up).
func (m *Model) ScrollBy(n int) {
	m.list.ScrollBy(n)
}

// SelectDelta moves the selection cursor by delta within the current
// (folder-filtered) item list — the same list ClickAt/ScrollBy operate
// on — clamping at either end (no wrap), and returns the newly selected
// chat's ID. It updates activeChatId on success.
//
// Unlike the tea.KeyPressMsg-driven navigation in Update, this is meant
// to be called directly from a global keybinding (e.g. next/prev chat)
// regardless of which panel currently has focus, so it does not consult
// m.focused.
//
// ok is false when the list is empty, the initial chat load is still in
// flight (mirroring ClickAt's loading guard — see the tabBarHeight
// comment above for why), or the selected item's ID fails to parse.
func (m *Model) SelectDelta(delta int) (chatID int64, ok bool) {
	if m.loading || len(m.list.Items) == 0 {
		return 0, false
	}
	m.list.ScrollBy(delta)
	item := m.list.SelectedItem()
	if item == nil {
		return 0, false
	}
	if _, err := fmt.Sscanf(item.ID, "%d", &chatID); err != nil {
		return 0, false
	}
	m.activeChatId = chatID
	return chatID, true
}

// SetFocused sets focus state.
//
// Losing focus while the filter input is open closes the input but keeps
// the filter applied — the same thing `enter` does. Without this, a panel
// switch (or an overlay taking focus) would leave FilterActive() true
// while the keys that reach this component do not, stranding the input
// open with no way to close it.
func (m *Model) SetFocused(focused bool) {
	m.focused = focused
	m.list.Focused = focused
	if !focused && m.filtering {
		m.closeFilterInput()
	}
}

// OpenFilter opens the local chat-list filter input: `/` in the chat
// list, matching vi's "search the buffer in front of you" (the global
// cross-chat search stays on ctrl+g). While it is open FilterActive
// reports true and the app is expected to route key presses straight to
// Update, which consumes them all (see updateFilterKey).
//
// Reopening an already-applied filter keeps the existing query and puts
// the cursor at its end, so `/` is also "edit the current filter".
//
// Unlike ClickAt/ClickAtXY/SelectDelta, this deliberately does NOT bail
// out while the initial chat load is still in flight. Those three resolve
// a screen row (or a cursor delta) into a chat and so must refuse to
// answer while View shows nothing but the spinner; opening a filter
// resolves nothing. A query typed during the load narrows an empty list
// — harmlessly, since refreshList rebuilds from an empty store — and is
// applied to the first real list by the refreshList in Update's
// chatsLoadedMsg branch. That is strictly better than swallowing the
// keystroke and leaving the user wondering why "/" did nothing, and it
// keeps FilterActive() reachable for callers that cannot leave the
// loading state (internal/app's key tests drive a Model whose telegram
// client is nil, so no chatsLoadedMsg ever arrives there).
//
// The caller should treat the key that opened the filter as consumed and
// NOT also forward it to Update. If it does anyway, that first key press
// is swallowed rather than typed into the query (filterJustOpened).
func (m *Model) OpenFilter() {
	m.filtering = true
	m.filterJustOpened = true
	m.filterInput.Value = m.filter
	m.filterInput.Cursor = m.filterInput.Len()
	m.filterInput.Focused = true
}

// FilterActive reports whether the filter input is open and consuming
// keys. It is false once the input is closed, INCLUDING when a filter is
// still applied (`enter`) — the applied-but-closed state is a normal
// browsing state in which j/k/enter/folder keys work as usual, and is
// advertised by the filter chip in the tab bar row.
func (m Model) FilterActive() bool {
	return m.filtering
}

// FilterQuery returns the currently applied filter ("" when none). It
// reflects what the list is actually filtered by, not what is being
// typed — although while the input is open the two are the same, since
// the filter is applied live on every keystroke.
func (m Model) FilterQuery() string {
	return m.filter
}

// ClearFilter drops the filter and closes the input, restoring the full
// (folder-filtered) chat list. This is what `esc` does, and what the
// "esc:clear" hint in the filter chip advertises.
func (m *Model) ClearFilter() {
	m.filter = ""
	m.filterInput.Reset()
	m.closeFilterInput()
	m.refreshList()
}

// closeFilterInput closes the input without touching the applied filter.
func (m *Model) closeFilterInput() {
	m.filtering = false
	m.filterJustOpened = false
	m.filterInput.Focused = false
}

// updateFilterKey handles one key press while the filter input is open.
// EVERY key is consumed here: the list's own vi motions, the folder
// switches and the digit jumps below all become literal text while
// typing a query, which is the whole point of an explicit input mode.
//
//   - esc     clears the filter and closes the input
//   - enter   closes the input, KEEPING the filter applied
//   - other   is handed to the shared TextArea (printables append,
//     backspace deletes, ctrl+u/ctrl+w kill) and the list is
//     re-filtered live from the new value
func (m Model) updateFilterKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	// Swallow a re-delivery of the key that opened the input (see
	// OpenFilter). Any other key clears the latch, so a query can still
	// contain a literal "/" anywhere.
	justOpened := m.filterJustOpened
	m.filterJustOpened = false

	switch msg.String() {
	case "esc":
		m.ClearFilter()
		return m, filteredCmd("")
	case "enter":
		m.closeFilterInput()
		return m, nil
	case "/":
		if justOpened && m.filterInput.Len() == 0 {
			return m, nil
		}
	}

	before := m.filterInput.Value
	m.filterInput.Update(msg)
	if m.filterInput.Value == before {
		return m, nil
	}

	m.filter = m.filterInput.Value
	m.refreshList()
	return m, filteredCmd(m.filter)
}

// filteredCmd announces a filter change to the app layer.
func filteredCmd(query string) tea.Cmd {
	return func() tea.Msg {
		return ChatListFilteredMsg{Query: query}
	}
}

// ActiveChatId returns the currently selected chat ID.
func (m *Model) ActiveChatId() int64 {
	return m.activeChatId
}

// ActiveFolderID returns the ID of the currently active folder tab —
// telegram.AllChatsFolderID when no folders are set (the default,
// pre-load state). Exists for the app layer and tests: activeFolder is
// unexported, and View() renders nothing but a loading spinner while
// m.loading, so callers otherwise have no way to observe which folder is
// selected (e.g. to assert a folder-cycling keybinding actually changed
// something, rather than merely that the key was dispatched).
func (m Model) ActiveFolderID() int32 {
	if m.activeFolder < 0 || m.activeFolder >= len(m.folders) || m.folders[m.activeFolder] == nil {
		return telegram.AllChatsFolderID
	}
	return m.folders[m.activeFolder].ID
}

// FolderNames returns the folder tab labels in display order.
//
// The tabs are DRAWN by the top bar in TUI 2.0, but selection and key
// handling stay here — this is the projection that lets the two live in
// different packages without the folder keymap moving with the pixels.
func (m Model) FolderNames() []string {
	out := make([]string, 0, len(m.folders))
	for _, f := range m.folders {
		out = append(out, folderLabel(f))
	}
	return out
}

// ActiveFolderIndex returns the selected tab's position, or 0 when no
// folders have loaded yet — the synthesized "All" tab is always first and
// always present, so index 0 is never wrong, only uninformative.
func (m Model) ActiveFolderIndex() int {
	if m.activeFolder < 0 || m.activeFolder >= len(m.folders) {
		return 0
	}
	return m.activeFolder
}

// Count returns how many chats the list is currently showing, after the
// active folder and any live filter have been applied. It is what the hint
// bar's buffer counter reports, so it must be what the user can actually
// see rather than the total held in the store.
func (m Model) Count() int { return len(m.list.Items) }

// CycleFolder moves the active folder tab by delta (wrapping around) and
// refilters the chat list to match.
// SetFoldersForTest installs folders by title. It exists so tests in other
// packages — the app's top-bar click routing, chiefly — can set up a folder
// list without constructing telegram.ChatFolder values or faking a server
// response.
func (m *Model) SetFoldersForTest(titles []string) {
	folders := make([]*telegram.ChatFolder, 0, len(titles))
	for i, title := range titles {
		folders = append(folders, &telegram.ChatFolder{ID: int32(i), Title: title})
	}
	m.setFolders(folders)
}

// SelectFolderIndex activates the folder at index, reporting whether it
// actually changed anything.
//
// It exists because the folder TABS are drawn by the frame's top bar now,
// while folder STATE still lives here. The top bar can say which tab was
// clicked but not what that means; this is the other half.
func (m *Model) SelectFolderIndex(index int) bool {
	if index < 0 || index >= len(m.folders) || index == m.activeFolder {
		return false
	}
	m.activeFolder = index
	m.refreshList()
	return true
}

func (m *Model) CycleFolder(delta int) {
	n := len(m.folders)
	if n == 0 {
		return
	}
	m.activeFolder = ((m.activeFolder+delta)%n + n) % n
	m.refreshList()
}

// jumpToFolder sets the active folder tab directly to index n, clamped to
// the valid range, and refilters the chat list. Used by the digit 1-9
// folder-jump keys (digit "1" -> index 0, the folder always present
// there — either the server's own default folder or the synthesized
// "All").
func (m *Model) jumpToFolder(n int) {
	if len(m.folders) == 0 {
		return
	}
	if n < 0 {
		n = 0
	}
	if n >= len(m.folders) {
		n = len(m.folders) - 1
	}
	if n == m.activeFolder {
		return
	}
	m.activeFolder = n
	m.refreshList()
}

// markDirty flags the list for a rebuild on the next View call.
func (m *Model) markDirty() {
	*m.dirty = true
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case chatsLoadedMsg:
		m.loading = false
		m.spinner.Active = false
		m.refreshList()

	case telegram.ChatFoldersMsg:
		// Also arrives live from the update listener on folder edits.
		m.setFolders(msg.Folders)
		cmds = append(cmds, m.FolderLoadCmd())

	case folderDialogsLoadedMsg:
		for _, ch := range msg.chats {
			if ch != nil {
				m.store.Chats.Set(ch)
			}
		}
		m.refreshList()

	case telegram.ChatMuteChangedMsg:
		m.store.Chats.SetMuted(msg.ChatId, msg.Muted)
		m.markDirty()

	case telegram.ChatLastMessageMsg:
		m.store.Chats.UpdateLastMessage(msg.ChatId, msg.LastMessage)
		m.markDirty()

	case telegram.ChatReadInboxMsg:
		m.store.Chats.UpdateReadInbox(msg.ChatId, msg.UnreadCount)
		m.markDirty()

	case telegram.ChatUpdateMsg:
		if msg.Chat != nil {
			m.store.Chats.Set(msg.Chat)
			m.markDirty()
		}

	case telegram.NewMessageMsg:
		m.markDirty()

	case widgets.SpinnerTickMsg:
		cmd := m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyPressMsg:
		// The filter input, while open, owns every key (including the
		// folder and motion keys below) — see updateFilterKey. It is
		// checked before m.focused because the app hands keys to this
		// component precisely because FilterActive() is true.
		if m.filtering {
			return m.updateFilterKey(msg)
		}

		if m.focused {
			// Terminal-independent folder switching: left/right arrows
			// and the lazygit-style '['/']' cycle folders, digits 1-9
			// jump straight to folder N (1 = All, always at index 0).
			// chatlist is the sole owner of the '['/']' aliases — the
			// app-level copy was removed in an earlier wave. Alt+h/alt+l
			// remain app-level (internal/app/app.go), for hands that
			// reach for vi directions; BARE h/l are NOT folder keys any
			// more — this wave rebinds them to lazygit-style panel
			// movement in app.go — so '['/']', the arrows and the digits
			// here are the alt-free, terminal-independent fallback for
			// terminals (e.g. Ghostty's default "option acts as input")
			// that can't report alt as a distinguishable modifier at all.
			//
			// None of these collide with the list widget's own vi
			// motions (up/down/j/k/g/G/enter, handled below). Quick-type
			// (which used to intercept printable keys like these digits
			// before they reached this panel) was removed in an earlier
			// wave, so there's no longer anything upstream in app.go to
			// keep in sync with here.
			switch msg.String() {
			case "/":
				// The app normally binds '/' and calls OpenFilter
				// itself; handling it here too keeps the component
				// self-contained standalone (and under test), and is
				// harmless either way because OpenFilter is idempotent
				// and swallows a re-delivered '/'.
				m.OpenFilter()
				return m, nil
			case "esc":
				// Only meaningful in the applied-but-closed state, the
				// one the filter chip's "esc:clear" hint advertises. The
				// app's own Esc ladder normally intercepts this key
				// first; when it does, '/' then esc still clears.
				if m.filter != "" {
					m.ClearFilter()
					return m, filteredCmd("")
				}
				return m, nil
			case "left", "[":
				m.CycleFolder(-1)
				return m, m.FolderLoadCmd()
			case "right", "]":
				m.CycleFolder(1)
				return m, m.FolderLoadCmd()
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				n := int(msg.String()[0] - '1')
				m.jumpToFolder(n)
				return m, m.FolderLoadCmd()
			}

			if selected := m.list.Update(msg); selected {
				item := m.list.SelectedItem()
				if item != nil {
					var chatID int64
					fmt.Sscanf(item.ID, "%d", &chatID)
					m.activeChatId = chatID
					return m, func() tea.Msg {
						return ChatSelectedMsg{ChatId: chatID}
					}
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// setFolders replaces the folder state, always keeping a synthesized or
// server-provided "All" folder at index 0, and preserves the active tab
// by folder ID (not index) when possible.
func (m *Model) setFolders(folders []*telegram.ChatFolder) {
	var activeID int32 = telegram.AllChatsFolderID
	if m.activeFolder >= 0 && m.activeFolder < len(m.folders) && m.folders[m.activeFolder] != nil {
		activeID = m.folders[m.activeFolder].ID
	}

	m.folders = normalizeFolders(folders)
	m.activeFolder = 0
	for i, f := range m.folders {
		if f != nil && f.ID == activeID {
			m.activeFolder = i
			break
		}
	}

	m.refreshList()
}

// FolderLoadCmd fetches include/pin dialogs for the active folder that
// are not already in the store. The folder's own peer list is the
// membership source — not a recency slice of getDialogs. Nil client
// (tests) is a no-op.
func (m Model) FolderLoadCmd() tea.Cmd {
	if m.tg == nil {
		return nil
	}
	var folder *telegram.ChatFolder
	if m.activeFolder >= 0 && m.activeFolder < len(m.folders) {
		folder = m.folders[m.activeFolder]
	}
	if !folder.NeedsPeerFetch() {
		return nil
	}
	already := make(map[int64]struct{})
	for _, e := range m.store.Chats.OrderedChats() {
		if e != nil && e.Chat != nil {
			already[e.Chat.ID] = struct{}{}
		}
	}
	tg := m.tg
	return func() tea.Msg {
		chats, err := tg.LoadFolderDialogs(folder, already)
		if err != nil || len(chats) == 0 {
			return folderDialogsLoadedMsg{}
		}
		return folderDialogsLoadedMsg{chats: chats}
	}
}

type folderDialogsLoadedMsg struct {
	chats []*telegram.Chat
}

// normalizeFolders ensures an "All chats" folder is always present at
// index 0. The server only ever names the default folder explicitly via
// dialogFilterDefault (see folders.go); when present it carries no
// title/emoticon of its own, so one is filled in for display.
func normalizeFolders(folders []*telegram.ChatFolder) []*telegram.ChatFolder {
	var all *telegram.ChatFolder
	rest := make([]*telegram.ChatFolder, 0, len(folders))
	for _, f := range folders {
		if f != nil && f.ID == telegram.AllChatsFolderID {
			all = f
			continue
		}
		rest = append(rest, f)
	}

	if all == nil {
		all = defaultAllFolder()
	} else if all.Title == "" {
		cp := *all
		cp.Title = "All"
		if cp.Emoticon == "" {
			cp.Emoticon = "💬"
		}
		all = &cp
	}

	out := make([]*telegram.ChatFolder, 0, len(rest)+1)
	out = append(out, all)
	out = append(out, rest...)
	return out
}

func (m *Model) refreshList() {
	*m.dirty = false

	// The cursor is an index into the list that is about to be rebuilt,
	// so remember which CHAT it points at. SetItems only clamps the index
	// into range, which keeps the selection in bounds but slides it onto
	// a different chat whenever the list shrinks (a folder switch, or a
	// filter keystroke removing rows above the cursor). Re-selecting by
	// ID below keeps the highlighted chat under the cursor for as long as
	// it survives the filter; when it doesn't, the clamped index stands.
	prevID := m.list.SelectedID()

	var folder *telegram.ChatFolder
	if m.activeFolder >= 0 && m.activeFolder < len(m.folders) {
		folder = m.folders[m.activeFolder]
	}

	chats := orderForFolder(folder, m.store.Chats.OrderedChats())
	items := make([]widgets.ListItem, 0, len(chats))

	for _, entry := range chats {
		if entry.Chat == nil {
			continue
		}

		if !m.chatInFolder(folder, entry) {
			continue
		}

		if m.filter != "" {
			if !strings.Contains(strings.ToLower(entry.Chat.Title), strings.ToLower(m.filter)) {
				continue
			}
		}

		preview := ""
		meta := ""
		if entry.LastMessage != nil {
			preview = messagePreview(entry.LastMessage)
			meta = render.FormatTimestampSmart(entry.LastMessage.Date)
		}
		// A parked draft outranks the last message in the preview row
		// (decision 13). What somebody else said is still in the chat when
		// you open it; that you left something half-written in there is a
		// thing you would otherwise have to remember on your own.
		if m.draftChats[entry.Chat.ID] {
			preview = "draft: saved locally"
		}

		badge := ""
		if entry.UnreadCount > 0 {
			badge = fmt.Sprintf("%d", entry.UnreadCount)
		}

		online := false
		if entry.Chat.Type == telegram.ChatTypePrivate {
			online = m.store.Users.IsOnline(entry.Chat.ID)
		}

		items = append(items, widgets.ListItem{
			ID: fmt.Sprintf("%d", entry.Chat.ID),
			// The title is bare now: the type mark is the sigil the row
			// renderer draws in its own column, not a prefix glued to the
			// text, so it cannot be truncated away with a long name.
			Title:    entry.Chat.Title,
			Kind:     int(entry.Chat.Type),
			Saved:    entry.Chat.ID == m.myUserID,
			Subtitle: preview,
			Badge:    badge,
			Meta:     meta,
			Online:   online,
			Muted:    entry.Chat.Muted,
		})
	}

	m.list.SetItems(items)

	if prevID != "" && prevID != m.list.SelectedID() {
		for i := range items {
			if items[i].ID == prevID {
				m.list.SelectIndex(i)
				break
			}
		}
	}
}

// chatInFolder reports whether entry belongs in folder, applying
// Telegram's folder semantics:
//   - explicitly excluded chats are always out;
//   - explicitly pinned or included chats are always in, bypassing the
//     category flags and the mute/read excludes below;
//   - otherwise, when the folder sets any category flag, the chat must
//     match at least one of them (see matchesFolderCategory);
//   - a custom folder with no category flags is include/pin only: chats
//     not on those lists are out (this is how a "these chats" folder is
//     stored — flags all false, include_peers set);
//   - the implicit All folder (ID 0, no flags, no include/pin lists)
//     shows every remaining chat;
//   - ExcludeMuted drops muted chats, ExcludeRead drops read chats — both
//     only for chats that reached this point (pinned/included chats
//     already returned true above, matching Telegram's own behavior).
//
// folder == nil (should not normally happen; New/normalizeFolders always
// leave at least the synthesized "All" folder in place) means "no
// filter".
func (m *Model) chatInFolder(folder *telegram.ChatFolder, entry *store.ChatEntry) bool {
	if folder == nil {
		return true
	}
	chat := entry.Chat

	for _, id := range folder.ExcludedChatIDs {
		if id == chat.ID {
			return false
		}
	}

	for _, id := range folder.PinnedChatIDs {
		if id == chat.ID {
			return true
		}
	}
	for _, id := range folder.IncludedChatIDs {
		if id == chat.ID {
			return true
		}
	}

	hasCategories := folder.Contacts || folder.NonContacts || folder.Groups || folder.Channels || folder.Bots
	if hasCategories {
		if !m.matchesFolderCategory(folder, chat) {
			return false
		}
	} else if folder.ID != telegram.AllChatsFolderID {
		// Custom / chatlist folder with no type flags: only the include
		// and pin lists above count. Falling through here used to treat
		// them like All, so switching tabs left the full chat list on screen.
		return false
	}

	if folder.ExcludeMuted && chat.Muted {
		return false
	}
	if folder.ExcludeRead && entry.UnreadCount == 0 {
		return false
	}

	return true
}

// matchesFolderCategory approximates Telegram's per-folder category
// flags against our domain types.
//
// APPROXIMATION: telegram.User (internal/telegram/types.go) carries no
// IsContact-like field in this codebase, so folder.Contacts and
// folder.NonContacts cannot be distinguished from our data. Both are
// therefore treated as "any private, non-bot chat" — i.e. Contacts ||
// NonContacts covers the same set of chats. This over-includes chats
// relative to real Telegram semantics (a folder with only Contacts set
// would, on the real client, hide non-contact DMs) but never
// under-includes, and is documented here rather than silently wrong.
func (m *Model) matchesFolderCategory(folder *telegram.ChatFolder, chat *telegram.Chat) bool {
	switch chat.Type {
	case telegram.ChatTypeBasicGroup, telegram.ChatTypeSupergroup:
		return folder.Groups
	case telegram.ChatTypeChannel:
		return folder.Channels
	case telegram.ChatTypePrivate:
		isBot := false
		// Private chat IDs share the user ID namespace (see the existing
		// IsOnline lookup below in refreshList, which relies on the same
		// convention).
		if u, ok := m.store.Users.Get(chat.ID); ok {
			isBot = u.IsBot
		}
		if isBot {
			return folder.Bots
		}
		return folder.Contacts || folder.NonContacts
	default:
		return false
	}
}

// orderForFolder reorders chats so that entries named in folder's
// PinnedChatIDs come first, in that order; every other chat keeps its
// existing relative order (ChatStore.OrderedChats' pinned-flag-then-
// recency ordering). Chats not present in chats are ignored.
func orderForFolder(folder *telegram.ChatFolder, chats []*store.ChatEntry) []*store.ChatEntry {
	if folder == nil || len(folder.PinnedChatIDs) == 0 {
		return chats
	}

	pinnedIndex := make(map[int64]int, len(folder.PinnedChatIDs))
	for i, id := range folder.PinnedChatIDs {
		pinnedIndex[id] = i
	}

	pinned := make([]*store.ChatEntry, len(folder.PinnedChatIDs))
	rest := make([]*store.ChatEntry, 0, len(chats))
	for _, entry := range chats {
		if entry.Chat == nil {
			rest = append(rest, entry)
			continue
		}
		if idx, ok := pinnedIndex[entry.Chat.ID]; ok {
			pinned[idx] = entry
			continue
		}
		rest = append(rest, entry)
	}

	out := make([]*store.ChatEntry, 0, len(chats))
	for _, e := range pinned {
		if e != nil {
			out = append(out, e)
		}
	}
	out = append(out, rest...)
	return out
}

func chatIcon(chat *telegram.Chat) string {
	switch chat.Type {
	case telegram.ChatTypePrivate:
		return "👤"
	case telegram.ChatTypeBasicGroup:
		return "👥"
	case telegram.ChatTypeSupergroup:
		return "👥"
	case telegram.ChatTypeChannel:
		return "📢"
	default:
		return "💬"
	}
}

func messagePreview(msg *telegram.Message) string {
	if msg == nil || msg.Content == nil {
		return ""
	}

	switch c := msg.Content.(type) {
	case *telegram.MessageText:
		return truncatePreviewText(c.Text.Text, 50)
	case *telegram.MessagePhoto:
		return "📷 Photo"
	case *telegram.MessageVideo:
		return "🎥 Video"
	case *telegram.MessageDocument:
		return "📎 " + c.Document.FileName
	case *telegram.MessageVoiceNote:
		return "🎤 Voice message"
	case *telegram.MessageVideoNote:
		return "📹 Video message"
	case *telegram.MessageSticker:
		return "🏷 " + c.Sticker.Emoji + " Sticker"
	case *telegram.MessageAnimation:
		return "🎬 GIF"
	case *telegram.MessageAudio:
		return "🎵 Audio"
	case *telegram.MessageLocation:
		return "📍 Location"
	case *telegram.MessageContact:
		return "👤 Contact"
	case *telegram.MessagePoll:
		return "📊 Poll"
	default:
		return "💬 Message"
	}
}

// truncatePreviewText flattens newlines/tabs to single spaces — so a
// multi-line message does not break the list widget's fixed 2-line row —
// and truncates on runes rather than bytes, so multibyte text (e.g.
// Cyrillic, CJK, emoji) is not corrupted by a mid-rune cut.
func truncatePreviewText(s string, maxRunes int) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")

	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 0 {
		return ""
	}
	return string(r[:maxRunes]) + "..."
}

// View renders the chat list.
func (m Model) View() string {
	if m.loading {
		return lipgloss.NewStyle().
			Width(m.width).
			Height(m.height).
			Align(1, 1). // center
			Render(m.spinner.View())
	}

	if *m.dirty {
		m.refreshList()
	}

	// Folder tabs used to live at the top of this column; TUI 2.0 moves
	// them to the frame's top bar and gives this row to the filter instead.
	// Selection and key handling stayed here — only the drawing moved.
	return strings.Join([]string{
		m.renderFilterHeader(m.width),
		m.list.View(),
		m.renderListFooter(m.width),
	}, "\n")
}

// Folder-tab rendering and its hit-test used to live here. They moved to
// internal/ui/components/topbar with TUI 2.0, which draws the tabs in the
// frame's top chrome row; this column's first row is the filter header now.
// Folder STATE is still owned here — see SelectFolderIndex and CycleFolder,
// which is the half the top bar calls back into.

func folderLabel(f *telegram.ChatFolder) string {
	if f == nil {
		return ""
	}
	title := f.Title
	if title == "" {
		title = "All"
	}
	if f.Emoticon != "" {
		return f.Emoticon + " " + title
	}
	return title
}
