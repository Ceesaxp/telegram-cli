package search

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

// hintText is the bottom affordance line shown on the overlay, so a user
// dropped into the overlay always has an on-screen answer to "how do I get
// out of this".
const hintText = "Enter: search · Tab: switch tab · ↑↓: select · Esc: close"

// mutedFg is used for the hint line and the empty-state message. It matches
// the gray widgets.List already uses for its own "No items" placeholder.
const mutedFg = lipgloss.Color("#565F89")

// Overlay geometry. The box is a centered, capped-size dialog rather than a
// box stretched to the full window: DialogBox contributes a 1-cell border
// and (1,2) padding per side, SearchInput contributes its own 1-cell border,
// and the bordered single-line input always renders as 3 rows (top border +
// content + bottom border).
const (
	maxBoxWidth  = 72
	maxBoxHeight = 24
	// minBoxWidth is floored high enough that the tab bar ("Chats",
	// "Messages", "Global", each padded 2 cells a side and space-joined)
	// always fits on tabs' own single row — widgets.Tabs wraps via
	// lipgloss's Width-based word wrap otherwise, since it can't be given
	// custom truncation from here (widgets/tabs.go is out of scope).
	minBoxWidth  = 40
	minBoxHeight = 12

	dialogChromeW = 6                     // DialogBox: (border 1 + padding 2) * 2 sides
	dialogChromeH = 4                     // DialogBox: (border 1 + padding 1) * 2 sides
	inputChromeW  = 2                     // SearchInput: border 1 * 2 sides
	inputRows     = 3                     // bordered single-line input: top + content + bottom
	fixedRows     = 1 + inputRows + 1 + 1 // title + input + tabs + hint

	// minContentWidth is the smallest content column lipgloss's own
	// wrap-then-pad pipeline settles to once the requested width can no
	// longer fit DialogBox's border+padding chrome plus real content
	// (empirically observed: below it, DialogBox silently renders wider
	// than asked rather than narrower, since it can't shrink its own
	// fixed border/padding any further).
	minContentWidth = 2

	// structuralMinWidth/structuralMinHeight are hard floors, distinct
	// from minBoxWidth/minBoxHeight above: they're not a *preference* but
	// the smallest box DialogBox's fixed chrome can actually render
	// without itself overflowing past the size it was asked for. Clamping
	// boxWidth/boxHeight to the window (below) can still legitimately push
	// them under these — in that case the structural floor wins even if
	// it means the box exceeds a pathologically small window, because
	// going smaller doesn't produce a smaller box, only a more broken one
	// (fixedRows silently overflowing boxHeight, or DialogBox's own
	// border+padding overflowing boxWidth).
	structuralMinWidth  = dialogChromeW + minContentWidth // 8
	structuralMinHeight = dialogChromeH + fixedRows + 1   // 11 (+1: at least one list row)
)

// geometry is the overlay's computed layout: the outer box dimensions the
// DialogBox style is asked to render at, and the widths/heights derived from
// it for the box's inner rows so nothing inside the box is wider than the
// box's own content-wrap width (the cause of the border-wrapping artifacts
// this replaces).
type geometry struct {
	boxWidth   int
	boxHeight  int
	innerWidth int // content width for title/tabs/list/hint rows
	inputWidth int // widgets.TextArea.Width (content+padding, pre-border)
	listHeight int
}

// computeGeometry derives a centered overlay box from the full window size,
// capped so it never stretches to fill the screen and floored so it stays
// usable on a tiny terminal.
func computeGeometry(w, h int) geometry {
	boxW := w - 8
	if boxW > maxBoxWidth {
		boxW = maxBoxWidth
	}
	if boxW < minBoxWidth {
		boxW = minBoxWidth
	}
	// The floor is a *preference*, not a guarantee: the caller places this
	// box with lipgloss.Place, which does not clip. If the window itself
	// is narrower than the floor, clamping to it would overflow/smear
	// past the terminal edge instead of degrading gracefully — so the
	// floor only applies when the window actually affords it.
	if boxW > w {
		boxW = w
	}
	// Below this, the box isn't degrading to fit the window anymore — it's
	// breaking (DialogBox's own border+padding chrome no longer fits in
	// boxW, so it silently renders wider than requested). Keep the
	// structural minimum instead; on a window this small there is no
	// artifact-free way to fit it, so we prefer a box of a known, sane
	// size over a smaller one that overflows unpredictably.
	if boxW < structuralMinWidth {
		boxW = structuralMinWidth
	}

	boxH := h - 6
	if boxH > maxBoxHeight {
		boxH = maxBoxHeight
	}
	if boxH < minBoxHeight {
		boxH = minBoxHeight
	}
	if boxH > h {
		boxH = h
	}
	// Same reasoning as boxW above: below the structural minimum, the
	// fixed rows (title/input/tabs/hint) no longer fit even with the list
	// squeezed to a single row, and DialogBox silently renders taller than
	// boxH instead of shrinking further.
	if boxH < structuralMinHeight {
		boxH = structuralMinHeight
	}

	innerW := boxW - dialogChromeW
	if innerW < 1 {
		innerW = 1
	}

	inputW := innerW - inputChromeW
	if inputW < 1 {
		inputW = 1
	}

	contentH := boxH - dialogChromeH
	listH := contentH - fixedRows
	if listH < 1 {
		listH = 1
	}

	return geometry{
		boxWidth:   boxW,
		boxHeight:  boxH,
		innerWidth: innerW,
		inputWidth: inputW,
		listHeight: listH,
	}
}

// fitLine truncates s to at most width display cells so a single-row status
// string (title, hint, empty-state message) can never trigger lipgloss's
// wrap-on-overflow and grow the row count the geometry above assumes.
func fitLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return cell.Clamp(s, width)
}

// SearchResultMsg is emitted when a search result is selected.
type SearchResultMsg struct {
	ChatId    int64
	MessageId int64
}

// Tab represents a search tab.
type Tab int

const (
	TabChats Tab = iota
	TabMessages
	TabGlobal
)

// Model is the search overlay component.
type Model struct {
	input       widgets.TextArea
	tabs        widgets.Tabs
	list        widgets.List
	store       *store.Store
	tg          *telegram.Client
	theme       *theme.Theme
	width       int
	height      int
	geo         geometry
	visible     bool
	query       string
	hasSearched bool // a search has completed for the current session
}

// New creates a new search model.
func New(s *store.Store, tg *telegram.Client, th *theme.Theme) Model {
	ta := widgets.NewTextArea()
	ta.Placeholder = "Search..."
	ta.Style = th.SearchInput
	ta.Focused = true

	tabs := widgets.NewTabs([]string{"Chats", "Messages", "Global"})
	tabs.StyleTab = th.Tab
	tabs.StyleTabActive = th.TabActive

	l := widgets.NewList()
	l.StyleNormal = th.SearchResult
	l.StyleActive = th.SearchResultActive
	l.StyleTitle = th.ChatListTitle
	l.StyleSub = th.ChatListPreview

	return Model{
		input: ta,
		tabs:  tabs,
		list:  l,
		store: s,
		tg:    tg,
		theme: th,
	}
}

// SetSize sets the component dimensions. width/height are the FULL window
// size — the overlay computes its own centered, capped box from them (see
// computeGeometry) rather than stretching to fill the window.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.geo = computeGeometry(width, height)

	m.input.Width = m.geo.inputWidth
	m.tabs.Width = m.geo.innerWidth
	m.list.Width = m.geo.innerWidth
	m.list.Height = m.geo.listHeight
}

// SetVisible shows or hides the search overlay.
func (m *Model) SetVisible(visible bool) {
	m.visible = visible
	if visible {
		m.input.Focused = true
		m.input.Reset()
		m.list.SetItems(nil)
		m.list.Focused = false
		m.query = ""
		m.hasSearched = false
	}
}

// IsVisible returns whether the search is visible.
func (m Model) IsVisible() bool {
	return m.visible
}

type searchResultsMsg struct {
	tab   Tab
	items []widgets.ListItem
}

func (m *Model) searchCmd() tea.Cmd {
	query := m.query
	tab := Tab(m.tabs.Active)

	return func() tea.Msg {
		var items []widgets.ListItem

		switch tab {
		case TabChats:
			chats, err := m.tg.SearchChats(query, 20)
			if err == nil {
				for _, chat := range chats {
					items = append(items, widgets.ListItem{
						ID:       fmt.Sprintf("%d", chat.ID),
						Title:    chat.Title,
						Subtitle: chatTypeLabel(chat),
					})
				}
			}

		case TabMessages:
			found, err := m.tg.SearchMessages(query, 20)
			if err == nil {
				for _, msg := range found {
					chat, _ := m.tg.GetChat(msg.ChatID)
					title := fmt.Sprintf("%d", msg.ChatID)
					if chat != nil {
						title = chat.Title
					}
					items = append(items, widgets.ListItem{
						ID:       fmt.Sprintf("%d:%d", msg.ChatID, msg.ID),
						Title:    title,
						Subtitle: messagePreview(msg),
					})
				}
			}

		case TabGlobal:
			chats, err := m.tg.SearchChats(query, 20)
			if err == nil {
				for _, chat := range chats {
					items = append(items, widgets.ListItem{
						ID:       fmt.Sprintf("%d", chat.ID),
						Title:    chat.Title,
						Subtitle: chatTypeLabel(chat),
					})
				}
			}
		}

		return searchResultsMsg{tab: tab, items: items}
	}
}

func chatTypeLabel(chat *telegram.Chat) string {
	switch chat.Type {
	case telegram.ChatTypePrivate:
		return "Private chat"
	case telegram.ChatTypeBasicGroup:
		return "Group"
	case telegram.ChatTypeSupergroup:
		return "Supergroup"
	case telegram.ChatTypeChannel:
		return "Channel"
	default:
		return "Chat"
	}
}

func messagePreview(msg *telegram.Message) string {
	if msg == nil || msg.Content == nil {
		return ""
	}
	if text, ok := msg.Content.(*telegram.MessageText); ok {
		return truncatePreviewText(text.Text.Text, 60)
	}
	return "Media message"
}

// truncatePreviewText flattens newlines/tabs to single spaces — so a
// multi-line message does not break the list widget's fixed 2-line row —
// and truncates on runes rather than bytes, so multibyte text (e.g.
// Cyrillic, CJK, emoji) is not corrupted by a mid-rune cut.
//
// Duplicated from chatlist's helper of the same name rather than shared,
// to avoid introducing a cross-component import for a handful of lines.
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

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case searchResultsMsg:
		m.list.SetItems(msg.items)
		m.hasSearched = true
		m.list.Focused = true
		m.input.Focused = false

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			// app.go's top-level key handler intercepts Esc before this
			// component ever sees it (it closes the overlay outright and
			// returns early — see the "Escape: close overlay or go back"
			// branch in Model.Update there), so in practice that layer
			// always wins and this case is unreachable through the app.
			// It stays here — closing on a single Esc regardless of
			// whether focus is on the input or the list — purely so the
			// component behaves the same way standalone (e.g. under test)
			// as it does embedded in the app, rather than implementing a
			// two-stage "first Esc unfocuses the list, second closes"
			// flow that the app layer would never let fire anyway.
			m.visible = false
			m.list.Focused = false
			m.input.Focused = false
			return m, nil
		case "tab":
			m.tabs.Update(msg)
			if m.query != "" {
				return m, m.searchCmd()
			}
		case "enter":
			if m.input.Focused {
				m.query = m.input.Value
				return m, m.searchCmd()
			}
			if m.list.Focused {
				item := m.list.SelectedItem()
				if item != nil {
					var chatID, messageID int64
					n, _ := fmt.Sscanf(item.ID, "%d:%d", &chatID, &messageID)
					if n == 1 {
						fmt.Sscanf(item.ID, "%d", &chatID)
					}
					m.visible = false
					return m, func() tea.Msg {
						return SearchResultMsg{ChatId: chatID, MessageId: messageID}
					}
				}
			}
		default:
			if m.input.Focused {
				m.input.Update(msg)
			} else {
				m.list.Update(msg)
			}
		}
	}

	return m, nil
}

// View renders the search overlay.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	g := m.geo

	title := m.theme.AuthTitle.Render(fitLine(m.titleText(), g.innerWidth))
	input := m.input.View()
	tabs := m.tabs.View()
	results := m.resultsView(g)
	hint := lipgloss.NewStyle().
		Foreground(mutedFg).
		Width(g.innerWidth).
		Render(fitLine(hintText, g.innerWidth))

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, input, tabs, results, hint,
	)

	// g.boxWidth/g.boxHeight are the box's OUTER dimensions. DialogBox adds
	// its own 1-cell border plus (1,2) padding on top of whatever is passed
	// to Width/Height, so the arguments here are the outer size minus that
	// border (the padding is baked into DialogBox itself) — passing
	// g.boxWidth/g.boxHeight directly, as the previous version effectively
	// did with m.width/m.height, is what stretched the box and let its
	// content overflow the actual wrap width.
	return m.theme.DialogBox.
		Width(g.boxWidth - 2).
		Height(g.boxHeight - 2).
		Render(content)
}

// titleText reflects search state in the title row: a plain "Search" before
// any search has run, or a result count once one has — the affordance the
// field report asked for in place of a silently bare list.
func (m Model) titleText() string {
	if m.hasSearched {
		return fmt.Sprintf("Search (%d)", len(m.list.Items))
	}
	return "Search"
}

// resultsView renders the list, or — in place of the list widget's generic
// "No items" — a message explaining why: nothing has been searched yet, or
// the search came back empty.
func (m Model) resultsView(g geometry) string {
	if len(m.list.Items) == 0 {
		msg := "type to search"
		if m.hasSearched {
			msg = fmt.Sprintf("no results for %q", m.query)
		}
		return lipgloss.NewStyle().
			Width(g.innerWidth).
			Height(g.listHeight).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(mutedFg).
			Render(fitLine(msg, g.innerWidth))
	}
	return m.list.View()
}
