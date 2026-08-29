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
	list         *widgets.List
	store        *store.Store
	tg           *telegram.Client
	theme        *theme.Theme
	width        int
	height       int
	focused      bool
	filter       string
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
	return Model{
		list:        &l,
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
func (m *Model) SetFocused(focused bool) {
	m.focused = focused
	m.list.Focused = focused
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
		if m.focused {
			// Terminal-independent folder switching: left/right arrows
			// and the lazygit-style '['/']' cycle folders, digits 1-9
			// jump straight to folder N (1 = All, always at index 0).
			// chatlist is the sole owner of the '['/']' aliases — the
			// app-level copy was removed this wave. Bare/alt h/l remain
			// app-level (internal/app/app.go's viFolder gate), the
			// terminal-independent alternative for terminals (e.g.
			// Ghostty's default "option acts as input") that can't
			// report alt as a distinguishable modifier at all.
			//
			// None of these collide with the list widget's own vi
			// motions (up/down/j/k/g/G/enter, handled below). Quick-type
			// (which used to intercept printable keys like these digits
			// before they reached this panel) was removed this wave, so
			// there's no longer anything upstream in app.go to keep in
			// sync with here.
			switch msg.String() {
			case "left", "[":
				m.CycleFolder(-1)
				return m, nil
			case "right", "]":
				m.CycleFolder(1)
				return m, nil
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				n := int(msg.String()[0] - '1')
				m.jumpToFolder(n)
				return m, nil
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
// width are dropped from the end; a single tab wider than the whole bar
// is truncated with an ellipsis.
func (m Model) renderFolderTabs() string {
	tabs := m.visibleFolderTabs()
	if len(tabs) == 0 {
		return ""
	}

	var b strings.Builder
	for _, t := range tabs {
		b.WriteString(t.rendered)
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

	var tabs []folderTab
	used := 0
	for i, f := range m.folders {
		style := m.theme.Tab
		if i == m.activeFolder {
			style = m.theme.TabActive
		}

		label := folderLabel(f)
		budget := m.width - used - style.GetHorizontalFrameSize()
		if budget < 0 {
			budget = 0
		}
		if ansi.StringWidth(label) > budget {
			label = truncateLabel(label, budget)
		}

		rendered := style.Render(label)
		w := ansi.StringWidth(rendered)
		if used+w > m.width && used > 0 {
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
