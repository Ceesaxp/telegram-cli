package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
)

// ListItem represents a single item in a scrollable list.
type ListItem struct {
	ID       string
	Title    string
	Subtitle string
	Badge    string

	// MetaAt is the instant Meta describes, or 0 when Meta is not a time.
	// A relative label goes stale where an absolute one cannot, so the
	// owner keeps the fact beside the sentence about it.
	MetaAt int32
	Meta   string
	Online bool
	Muted  bool // chat notifications are muted; render dimmed

	// Kind and Saved describe what the item IS, for callers whose rows
	// show a type mark. TUI 2.0's chat list uses them for its sigil, which
	// replaces the avatar as the identity signal — the widget itself never
	// interprets them.
	Kind  int
	Saved bool
}

// List is a generic scrollable list widget with vim-style navigation.
type List struct {
	// RenderRow, when set, replaces this widget's own row drawing. It is
	// given one item and must return exactly itemHeight lines, each
	// already fitted to width. Everything else — cursor, scrolling,
	// click-to-row mapping — is unaffected.
	//
	// This exists so a caller can have a bespoke row without forking the
	// scroll machinery, which is where the fiddly arithmetic lives.
	RenderRow func(item ListItem, selected, focused bool, width int) []string

	Items   []ListItem
	Cursor  int
	Offset  int
	Width   int
	Height  int
	Focused bool

	StyleNormal lipgloss.Style
	StyleActive lipgloss.Style
	StyleTitle  lipgloss.Style
	StyleSub    lipgloss.Style
	StyleMeta   lipgloss.Style
	StyleBadge  lipgloss.Style
	StyleOnline lipgloss.Style

	// StyleEmpty draws the "No items" placeholder. Supplied by the caller
	// like every other style here — this widget used to reach for a
	// hard-coded #565F89, which is how a generic list ended up owning an
	// opinion about the application's palette.
	StyleEmpty lipgloss.Style

	itemHeight int
}

// NewList creates a new list widget.
func NewList() List {
	return List{
		itemHeight: 2, // title + subtitle
	}
}

// ItemAtRow returns the index of the item displayed at the given local row
// (0-based, relative to the top of the list's visible area), or -1.
func (l *List) ItemAtRow(row int) int {
	if row < 0 {
		return -1
	}
	idx := l.Offset + row/l.itemHeight
	if idx < 0 || idx >= len(l.Items) {
		return -1
	}
	return idx
}

// SelectIndex moves the cursor to the given item index. Returns false if the
// index is out of bounds.
func (l *List) SelectIndex(i int) bool {
	if i < 0 || i >= len(l.Items) {
		return false
	}
	l.Cursor = i
	l.ensureVisible()
	return true
}

// ScrollBy moves the cursor by n items (negative scrolls up).
func (l *List) ScrollBy(n int) {
	l.Cursor += n
	if l.Cursor < 0 {
		l.Cursor = 0
	}
	if len(l.Items) > 0 && l.Cursor > len(l.Items)-1 {
		l.Cursor = len(l.Items) - 1
	}
	l.ensureVisible()
}

// SelectedItem returns the currently selected item, or nil.
func (l *List) SelectedItem() *ListItem {
	if l.Cursor >= 0 && l.Cursor < len(l.Items) {
		return &l.Items[l.Cursor]
	}
	return nil
}

// SelectedID returns the ID of the currently selected item.
func (l *List) SelectedID() string {
	if item := l.SelectedItem(); item != nil {
		return item.ID
	}
	return ""
}

// SetItems replaces all items, keeping cursor in bounds.
func (l *List) SetItems(items []ListItem) {
	l.Items = items
	if l.Cursor >= len(items) {
		l.Cursor = max(0, len(items)-1)
	}
	l.ensureVisible()
}

// Update handles key events for navigation.
func (l *List) Update(msg tea.Msg) (selected bool) {
	if !l.Focused {
		return false
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			if l.Cursor > 0 {
				l.Cursor--
				l.ensureVisible()
			}
		case "down", "j":
			if l.Cursor < len(l.Items)-1 {
				l.Cursor++
				l.ensureVisible()
			}
		case "home", "g":
			l.Cursor = 0
			l.Offset = 0
		case "end", "G":
			l.Cursor = max(0, len(l.Items)-1)
			l.ensureVisible()
		case "enter":
			return true
		}
	}
	return false
}

func (l *List) ensureVisible() {
	visibleItems := l.Height / l.itemHeight
	if visibleItems <= 0 {
		visibleItems = 1
	}

	if l.Cursor < l.Offset {
		l.Offset = l.Cursor
	}
	if l.Cursor >= l.Offset+visibleItems {
		l.Offset = l.Cursor - visibleItems + 1
	}
}

// View renders the list.
func (l *List) View() string {
	if len(l.Items) == 0 {
		return l.StyleEmpty.
			Width(l.Width).
			Height(l.Height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("No items")
	}

	visibleItems := l.Height / l.itemHeight
	if visibleItems <= 0 {
		visibleItems = 1
	}

	var b strings.Builder

	end := min(l.Offset+visibleItems, len(l.Items))
	// A one-cell indent where the avatar column used to be. TUI 2.0 has no
	// avatars — the chat list replaced them with a type sigil, and this
	// widget's own row drawing kept a four-cell block of coloured initials
	// that only the two un-redesigned surfaces still reached. The indent
	// stays because the rows were laid out around it.
	const rowIndent = 1

	// The row style (StyleNormal/StyleActive) carries its own padding —
	// PaddingLeft(1)+PaddingRight(1) in the shipped theme — so the text
	// column budget must leave room for it too, on top of the avatar
	// column: content is handed to that style's Width(l.Width) through
	// FitLine below, and FitLine's contract is content-plus-frame fits
	// totalWidth, not content-equals-totalWidth (see FitLine's doc
	// comment for why the latter silently word-wraps instead of
	// rendering as one line). Both styles are assumed to share the same
	// frame size, as they do in the shipped theme; take the larger of
	// the two defensively so a future asymmetric theme can't reintroduce
	// the overflow.
	rowFrame := max(l.StyleNormal.GetHorizontalFrameSize(), l.StyleActive.GetHorizontalFrameSize())
	textW := l.Width - rowIndent - rowFrame
	if textW < 0 {
		textW = 0
	}

	for i := l.Offset; i < end; i++ {
		item := l.Items[i]
		isActive := i == l.Cursor

		// A caller can own the row's appearance entirely while this widget
		// keeps the cursor, scroll offset and hit-testing — which is the
		// part with the subtle arithmetic and the tests to match. TUI 2.0's
		// chat list uses this to draw its own two-line rows without
		// reimplementing any of that.
		if l.RenderRow != nil {
			for _, line := range l.RenderRow(item, isActive, l.Focused, l.Width) {
				b.WriteString(line)
				b.WriteString("\n")
			}
			continue
		}

		style := l.StyleNormal
		if isActive {
			style = l.StyleActive
		}

		titleStyle := l.StyleTitle
		badgeStyle := l.StyleBadge
		if item.Muted {
			// Dim muted chats: faint title, faint (de-emphasized) badge
			// instead of the loud unread style.
			titleStyle = titleStyle.Faint(true)
			badgeStyle = badgeStyle.Faint(true)
		}

		// Title line: prefix + title + right-aligned meta. Every width
		// computed below is in display cells (cell.Width), never
		// runes: emoji (🔕 📢 👥), flag sequences, and CJK/Cyrillic text
		// all commonly differ from their rune count.
		//
		// 8 cells are reserved for the meta/timestamp column up front —
		// restored from the pre-cell-accurate budgeting this package
		// shipped with before — so a long title cannot consume the
		// entire column and starve the timestamp out. The title is what
		// truncates to make room; the timestamp must render whenever the
		// item has one.
		prefix := ""
		if item.Online {
			prefix += "● "
		}
		if item.Muted {
			prefix += "🔕 "
		}
		prefixW := cell.Width(prefix)
		titleBudget := textW - prefixW - 8
		if titleBudget < 0 {
			titleBudget = 0
		}
		plainTitle := prefix + cell.Truncate(item.Title, titleBudget)
		titleLine := titleStyle.Render(plainTitle)
		if item.Meta != "" {
			metaW := textW - cell.Width(plainTitle) - 2
			if metaW > 0 {
				meta := cell.FitLine(l.StyleMeta.Align(lipgloss.Right), cell.Truncate(item.Meta, metaW), metaW)
				titleLine += meta
			}
		}

		// Subtitle line: subtitle + badge. 6 cells reserved for the
		// badge up front, restored from the pre-cell-accurate budgeting,
		// so a long subtitle can't starve it out (mirrors the title/meta
		// reservation above). The badge itself is rendered at its
		// natural width (content plus its own padding) rather than
		// stretched to fill a slot: badgeStyle carries a background
		// color, and stretching it via Width()+Align would paint a wide
		// colored bar instead of a tight badge. Rendering it with no
		// Width() call at all also means there is no wrap risk here
		// regardless of badgeStyle's padding (see FitLine's doc comment
		// for why Width() is the thing that wraps) — the gap is instead
		// computed from the badge's actual rendered width and only
		// appended if it fits.
		subBudget := textW - 6
		if subBudget < 0 {
			subBudget = 0
		}
		plainSub := cell.Truncate(item.Subtitle, subBudget)
		subLine := l.StyleSub.Render(plainSub)
		if item.Badge != "" {
			badge := badgeStyle.Render(cell.Truncate(item.Badge, 6))
			gap := textW - cell.Width(subLine) - cell.Width(badge)
			if gap >= 1 {
				subLine = subLine + strings.Repeat(" ", gap) + badge
			}
		}

		// One output line at a time, each rendered through the row style
		// via FitLine — never through a bare style.Width() call on
		// already-full-width content (see FitLine's doc comment for why
		// that wraps instead of clamping).
		textLines := [2]string{titleLine, subLine}

		var rowLines []string
		for ri := range 2 {
			content := strings.Repeat(" ", rowIndent) + textLines[ri]
			rowLines = append(rowLines, cell.FitLine(style, content, l.Width))
		}

		b.WriteString(strings.Join(rowLines, "\n"))
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
