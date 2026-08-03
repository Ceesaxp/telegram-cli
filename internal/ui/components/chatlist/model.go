package chatlist

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/media"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

// Model is the chat list component.
type Model struct {
	list         widgets.List
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
	return Model{
		list:        l,
		store:       s,
		tg:          tg,
		theme:       th,
		loading:     true,
		spinner:     sp,
		avatarCache: media.NewCache(100),
		avatarRend:  media.NewImageRenderer(protocol, 4, 2),
	}
}

// Init loads the initial chat list.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadChatsCmd(),
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

type chatsLoadedMsg struct {
	err error
}

// SetSize sets the component dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.list.Width = width
	m.list.Height = height
}

// ClickAt selects the chat shown at the given local row (inside the panel
// border) and returns its ID. ok is false when the row has no chat.
func (m *Model) ClickAt(localY int) (chatID int64, ok bool) {
	idx := m.list.ItemAtRow(localY)
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

// ScrollBy moves the selection by n items (negative scrolls up).
func (m *Model) ScrollBy(n int) {
	m.list.ScrollBy(n)
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

	case telegram.ChatLastMessageMsg:
		m.store.Chats.UpdateLastMessage(msg.ChatId, msg.LastMessage)
		m.refreshList()

	case telegram.ChatReadInboxMsg:
		m.store.Chats.UpdateReadInbox(msg.ChatId, msg.UnreadCount)
		m.refreshList()

	case telegram.ChatUpdateMsg:
		if msg.Chat != nil {
			m.store.Chats.Set(msg.Chat)
			m.refreshList()
		}

	case telegram.NewMessageMsg:
		m.refreshList()

	case widgets.SpinnerTickMsg:
		cmd := m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyPressMsg:
		if m.focused {
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

func (m *Model) refreshList() {
	chats := m.store.Chats.OrderedChats()
	items := make([]widgets.ListItem, 0, len(chats))

	for _, entry := range chats {
		if entry.Chat == nil {
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
			meta = formatTime(entry.LastMessage.Date)
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
		})
	}

	m.list.SetItems(items)
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
		text := c.Text.Text
		if len(text) > 50 {
			text = text[:50] + "..."
		}
		return text
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

func formatTime(timestamp int32) string {
	// Simplified; the render package handles full formatting.
	if timestamp == 0 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", (timestamp/3600)%24, (timestamp/60)%60)
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

	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Render(m.list.View())
}
