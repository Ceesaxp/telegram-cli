package chatlist

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/config"
	"github.com/imtaqin/telegram-cli/internal/media"
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
	list    *widgets.List
	store   *store.Store
	tg      *telegram.Client
	theme   *theme.Theme
	width   int
	height  int
	focused bool

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
	avatarCache  *media.Cache
	avatarRend   *media.ImageRenderer

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
func New(s *store.Store, tg *telegram.Client, th *theme.Theme) Model {
	l := widgets.NewList()
	l.StyleNormal = th.ChatListItem
	l.StyleActive = th.ChatListItemActive
	l.StyleTitle = th.ChatListTitle
	l.StyleSub = th.ChatListPreview
	l.StyleMeta = th.ChatListTime
	l.StyleBadge = th.ChatListUnread
	l.StyleOnline = th.ChatListOnline

	sp := widgets.NewSpinner("Loading chats...")
	sp.Style = th.Spinner

	protocol := media.DetectProtocol()
	dirty := false

	// The filter input reuses the shared single-line TextArea so the
	// filter query gets the same rune-safe editing (printable-key
	// detection, backspace, ctrl+u/w, home/end) as every other text
	// surface in the app. It is rendered by renderFilterChip, not by
	// TextArea.View, because it shares one row with the folder tabs.
	fi := widgets.NewTextArea()
	fi.MultiLine = false

	return Model{
		list:        &l,
		filterInput: fi,
		store:       s,
		tg:          tg,
		theme:       th,
		loading:     true,
		spinner:     sp,
		avatarCache: media.NewCache(100),
		avatarRend:  media.NewImageRenderer(protocol, 4, 2),
		folders:     []*telegram.ChatFolder{defaultAllFolder()},
		dirty:       &dirty,
	}
}

// ApplyMedia sets the avatar image protocol from [media] config.
// Avatar cell size stays 4x2 regardless of MaxImageWidth/Height.
func (m *Model) ApplyMedia(cfg config.MediaConfig) {
	m.avatarRend = media.NewImageRenderer(media.ResolveProtocol(cfg.ImageProtocol), 4, 2)
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
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.list.Width = width

	listHeight := height - m.tabBarHeight()
	if listHeight < 0 {
		listHeight = 0
	}
	m.list.Height = listHeight
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
	row := localY - m.tabBarHeight()
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
	if y < m.tabBarHeight() {
		m.clickFolderTabAt(x)
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

// CycleFolder moves the active folder tab by delta (wrapping around) and
// refilters the chat list to match.
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
		cmds = append(cmds, m.downloadAvatarsCmd())

	case avatarsLoadedMsg:
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

		badge := ""
		if entry.UnreadCount > 0 {
			badge = fmt.Sprintf("%d", entry.UnreadCount)
		}

		online := false
		if entry.Chat.Type == telegram.ChatTypePrivate {
			online = m.store.Users.IsOnline(entry.Chat.ID)
		}

		// Check avatar cache
		avatar := ""
		cacheKey := fmt.Sprintf("av:%d", entry.Chat.ID)
		if cached, ok := m.avatarCache.Get(cacheKey); ok {
			avatar = cached
		}

		items = append(items, widgets.ListItem{
			ID:       fmt.Sprintf("%d", entry.Chat.ID),
			Title:    chatIcon(entry.Chat) + " " + entry.Chat.Title,
			Subtitle: preview,
			Badge:    badge,
			Meta:     meta,
			Online:   online,
			Avatar:   avatar,
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

type avatarsLoadedMsg struct{}

func (m Model) downloadAvatarsCmd() tea.Cmd {
	return func() tea.Msg {
		chats := m.store.Chats.OrderedChats()
		for _, entry := range chats {
			if entry.Chat == nil || entry.Chat.Photo == nil {
				continue
			}
			cacheKey := fmt.Sprintf("av:%d", entry.Chat.ID)
			if _, ok := m.avatarCache.Get(cacheKey); ok {
				continue // already cached
			}

			photo := entry.Chat.Photo
			if !photo.Downloaded || photo.Path == "" {
				file, err := m.tg.DownloadFileSync(photo.ID)
				if err != nil || file == nil {
					continue
				}
				photo = file
			}

			if photo.Path != "" {
				rendered, err := m.avatarRend.RenderFile(photo.Path)
				if err == nil && rendered != "" {
					m.avatarCache.Set(cacheKey, rendered)
				}
			}
		}
		return avatarsLoadedMsg{}
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

	tabBar := m.renderFolderTabs()
	listView := m.list.View()

	content := listView
	if tabBar != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, tabBar, listView)
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Render(content)
}

// renderFolderTabs renders a one-line tab bar of folder emoticon+title
// pairs, highlighting the active tab. Tabs that do not fit the available
// width are dropped from the end; a tab whose label alone is too wide has
// the label truncated with an ellipsis, and a tab that cannot fit even
// then (its style's padding already exceeds what is left) is dropped
// too — including the first one, so that no tab ever claims columns it
// is not painted in.
//
// When a filter is applied (or being typed) the filter chip is pinned to
// the right end of this same row — the chat list panel budgets its rows
// once, in SetSize, so the filter cannot claim a row of its own without
// desynchronizing the list height and ClickAt's row math (see
// tabBarHeight). The tabs get whatever width the chip leaves them.
func (m Model) renderFolderTabs() string {
	// The row is painted whenever tabBarHeight() reserves one, even when
	// nothing fits in it. Returning "" while tabBarHeight() still says 1
	// would shift every list row up by one against the height budget
	// SetSize computed and against ClickAt's row math — the same
	// off-by-one-row bug the tabBarHeight comment exists to prevent — and
	// a wide filter chip can now legitimately squeeze every tab out.
	if m.tabBarHeight() == 0 || m.width <= 0 {
		return ""
	}

	tabs := m.visibleFolderTabs()
	chip := m.renderFilterChip()

	var b strings.Builder
	for _, t := range tabs {
		b.WriteString(t.rendered)
	}

	if chip != "" {
		// visibleFolderTabs budgets against tabsAvailWidth and now drops
		// every tab that does not fit within it, so this truncation is a
		// no-op safety net: it must never actually cut, because a cut
		// here would paint fewer columns than the hit-test believes are
		// clickable. It stays only to keep the CHIP — the one thing on
		// this row explaining why chats are missing — from being the
		// piece FitLine drops if some future change reintroduces an
		// overflow.
		tabsBudget := m.tabsAvailWidth()
		tabsText := b.String()
		if ansi.StringWidth(tabsText) > tabsBudget {
			tabsText = ansi.Truncate(tabsText, tabsBudget, "")
		}
		pad := m.width - ansi.StringWidth(tabsText) - ansi.StringWidth(chip)
		if pad < 0 {
			pad = 0
		}
		b.Reset()
		b.WriteString(tabsText)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(chip)
	}

	// visibleFolderTabs's own per-tab budgeting (below) already
	// guarantees the concatenated, already-styled tabs total at most
	// m.width display cells; FitLine — with a bare, unpadded style, so
	// its own frame size is 0 — pads that out to exactly m.width and
	// serves as a defense-in-depth truncation safety net against any
	// future off-by-one in that budgeting. This is the same shared
	// helper list.go's rows and the status bar use, deliberately in
	// place of a bare lipgloss Width() call: see FitLine's doc comment
	// for why Width() on padded content is exactly what wrapped this bar
	// onto an invisible second line in the first place.
	return widgets.FitLine(lipgloss.NewStyle(), b.String(), m.width)
}

// filterClearHint is the text that makes clearing the filter
// discoverable from the indicator itself: whatever else gets dropped at
// narrow widths, a user who can see the chip can read how to get their
// chats back.
const filterClearHint = " esc:clear"

// renderFilterChip renders the filter indicator — "/query" as a badge,
// with a trailing cursor block while the input is open and the
// esc-clears hint whenever the row can afford it. It returns "" when no
// filter is applied and the input is closed, which is what keeps the tab
// bar unchanged in the common case.
//
// Everything here is measured in display cells (ansi.StringWidth) and cut
// with ansi.Truncate, never by rune or byte count, so an emoji or CJK
// query cannot shear the row: the chip's own width is what
// tabsAvailWidth hands to the folder tabs, and a wrong number there would
// overflow the one-line bar exactly the way FitLine exists to prevent.
func (m Model) renderFilterChip() string {
	if !m.filtering && m.filter == "" {
		return ""
	}

	query := m.filter
	if m.filtering {
		query = m.filterInput.Value
	}

	body := "/" + query
	if m.filtering {
		body += "█"
	}

	chip := m.theme.Badge.Render(body)
	hint := lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render(filterClearHint)

	if m.width <= 0 {
		return chip
	}
	if ansi.StringWidth(chip)+ansi.StringWidth(hint) <= m.width {
		return chip + hint
	}
	if ansi.StringWidth(chip) > m.width {
		return ansi.Truncate(chip, m.width, "")
	}
	return chip
}

// tabsAvailWidth is the width the folder tabs may occupy: the whole bar,
// less whatever the filter chip has pinned to its right end. Both
// visibleFolderTabs (layout AND click hit-testing) and renderFolderTabs
// budget against this single number so the painted row and the column
// ranges clicks are resolved against can never disagree.
func (m Model) tabsAvailWidth() int {
	avail := m.width - ansi.StringWidth(m.renderFilterChip())
	if avail < 0 {
		avail = 0
	}
	return avail
}

// folderTab is one rendered folder tab: its folder index, the fully
// STYLED text exactly as it will be painted (Tab or TabActive already
// applied), and the half-open column range [start, end) — in the same
// display-cell units as that styled text's width — it occupies within
// the tab bar.
type folderTab struct {
	index      int
	rendered   string
	start, end int
}

// visibleFolderTabs computes the folder tab bar layout: each folder's
// label, styled through Tab or TabActive (whichever m.activeFolder makes
// it), left to right, dropping tabs once the running total would exceed
// the available width. renderFolderTabs and clickFolderTabAt both
// consume this slice directly — never recomputing anything themselves —
// so they can never drift out of sync with each other or with what's
// actually painted.
//
// Measuring and truncating the STYLED text (not the bare label) matters:
// Tab/TabActive carry their own horizontal padding (Padding(0,2) in the
// shipped theme, i.e. a 4-cell frame), so a tab's actual on-screen width
// is the label's width PLUS that padding. Budgeting against the bare
// label alone undercounts every tab by its frame size, which both
// mis-hit-tests clicks (a click meant for one tab lands on its neighbor,
// since the column ranges used for hit-testing don't match what's
// painted) and, in aggregate, lets more tabs through the width budget
// than actually fit once their padding is added — overflowing the bar
// and forcing exactly the kind of wrap FitLine exists to prevent (see
// its doc comment), which pushed a folder tab the model considered
// "visible" off-screen entirely.
func (m Model) visibleFolderTabs() []folderTab {
	if len(m.folders) == 0 || m.width <= 0 {
		return nil
	}

	// The tabs get the row less the filter chip, not the whole row (see
	// tabsAvailWidth).
	avail := m.tabsAvailWidth()

	var tabs []folderTab
	used := 0
	for i, f := range m.folders {
		style := m.theme.Tab
		if i == m.activeFolder {
			style = m.theme.TabActive
		}

		label := folderLabel(f)
		budget := avail - used - style.GetHorizontalFrameSize()
		if budget < 0 {
			budget = 0
		}
		if ansi.StringWidth(label) > budget {
			label = truncateLabel(label, budget)
		}

		rendered := style.Render(label)
		w := ansi.StringWidth(rendered)
		// A tab that does not fit is DROPPED, including the first one.
		// This used to admit the first tab unconditionally ("&& used >
		// 0"), on the theory that showing one truncated tab beats
		// showing none. But the label truncation above can only shrink
		// the label, never the style's own frame (Padding(0,2) = 4
		// cells), so at a narrow width — or with a wide filter chip
		// holding the right end of the row — that first tab claimed
		// columns the renderer then truncated away. The hit-test reads
		// these ranges (clickFolderTabAt) and the renderer paints from
		// them, so the two disagreed exactly where the tab was invisible:
		// clicking the filter query itself switched folders. Dropping
		// the tab keeps every reported range painted, and leaves the
		// chip's columns belonging to nobody.
		if used+w > avail {
			break
		}

		tabs = append(tabs, folderTab{index: i, rendered: rendered, start: used, end: used + w})
		used += w
	}
	return tabs
}

// clickFolderTabAt switches the active folder to whichever tab occupies
// column x in the tab bar (a click between/past tabs is a no-op).
// Reports whether a tab was hit, purely for callers that want to know;
// ClickAtXY does not need it since a tab-bar click never selects a chat
// either way.
func (m *Model) clickFolderTabAt(x int) bool {
	for _, t := range m.visibleFolderTabs() {
		if x >= t.start && x < t.end {
			if t.index != m.activeFolder {
				m.activeFolder = t.index
				m.refreshList()
			}
			return true
		}
	}
	return false
}

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

// truncateLabel truncates plain (unstyled) text to at most maxWidth
// display cells (ansi.StringWidth), not runes — folder emoticons are
// frequently double-width — appending an ellipsis when it actually had
// to cut something.
func truncateLabel(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s
	}
	return ansi.Truncate(s, maxWidth, "…")
}
