package contacts

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
)

// ContactSelectedMsg is emitted when a contact is selected.
type ContactSelectedMsg struct {
	UserId int64
}

// Model is the contact list component.
type Model struct {
	list    widgets.List
	store   *store.Store
	tg      *telegram.Client
	roles   theme.Roles
	width   int
	height  int
	focused bool
	visible bool
	loaded  bool

	// all is every contact the account has, in display order. list.Items
	// is that set narrowed by the filter, so the two together are what the
	// header's "shown/total" counts — and re-filtering never has to ask
	// Telegram again.
	all []widgets.ListItem

	// The filter, in the same shape the chat list's has: filtering is
	// whether the input is open and consuming keys, filter is what is
	// applied. They are separate because `enter` closes the input while
	// KEEPING the filter, which is a normal browsing state.
	filtering   bool
	filter      string
	filterInput widgets.TextArea
}

// New creates a new contacts model.
func New(s *store.Store, tg *telegram.Client, r theme.Roles) Model {
	l := widgets.NewList()
	// Only StyleEmpty. Every other List style feeds the widget's own row
	// drawing, and this component supplies RenderRow — the six that used to
	// be set here were read by nothing once it did, and three of them
	// painted a panel background the frame already fills.
	l.StyleEmpty = theme.OverlayMuted(r)

	// The filter input reuses the shared single-line TextArea, so the query
	// gets the same rune-safe editing as every other text surface.
	fi := widgets.NewTextArea()
	fi.MultiLine = false

	m := Model{
		list:        l,
		store:       s,
		tg:          tg,
		roles:       r,
		filterInput: fi,
	}
	// Installed here rather than in a setter: a component that renders two
	// different ways depending on whether the caller remembered a call is a
	// component with two behaviours.
	m.list.RenderRow = m.renderRow
	return m
}

// SetSize sets the component dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.list.Width = width

	listHeight := height - m.headerHeight()
	if listHeight < 0 {
		listHeight = 0
	}
	m.list.Height = listHeight
}

// headerHeight is this column's only chrome row: the filter header above
// the list. ALWAYS one row, for the reason the chat list's is — a chrome
// row whose height depends on state changes the list's budget and ClickAt's
// arithmetic underneath a running app.
func (m Model) headerHeight() int { return 1 }

// ClickAt selects the contact shown at the given local row and returns its
// user ID. ok is false when the row has no contact — including row 0, which
// is the filter header rather than a contact.
func (m *Model) ClickAt(localY int) (userID int64, ok bool) {
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
	if _, err := fmt.Sscanf(item.ID, "%d", &userID); err != nil {
		return 0, false
	}
	return userID, true
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

// SetVisible shows or hides the contacts panel.
//
// Closing it drops any filter. The panel is opened to look somebody up, and
// a reader who narrowed it to one name, closed it, and came back to a list
// of one would be reading a state they had forgotten setting — the header
// says why, but "why is this list short" is a question the panel should not
// be asking on the way in.
func (m *Model) SetVisible(visible bool) {
	if !visible && m.visible {
		m.ClearFilter()
	}
	m.visible = visible
}

// IsVisible returns whether the contacts panel is visible.
func (m Model) IsVisible() bool {
	return m.visible
}

// OpenFilter opens the filter input over the contact list.
//
// The caller has consumed the key that asked for it: internal/app matches
// keys.search and returns, so this component never sees that press. The
// next key it does see is the first character of the query, and a "/" among
// them is a literal — a swallow here would make a query starting with one
// impossible to type.
func (m *Model) OpenFilter() {
	m.filtering = true
	m.filterInput.Value = m.filter
	m.filterInput.Cursor = m.filterInput.Len()
	m.filterInput.Focused = true
}

// FilterActive reports whether the filter input is open and consuming keys.
// False once it closes, INCLUDING while a filter is still applied (`enter`)
// — that is a normal browsing state in which j/k and enter work as usual.
func (m Model) FilterActive() bool { return m.filtering }

// FilterQuery is the applied filter, "" when none.
func (m Model) FilterQuery() string { return m.filter }

// ClearFilter drops the filter and closes the input, restoring the whole
// contact list. This is what `esc` does inside the input.
func (m *Model) ClearFilter() {
	m.filter = ""
	m.filterInput.Reset()
	m.closeFilterInput()
	m.applyFilter()
}

// closeFilterInput closes the input without touching the applied filter.
func (m *Model) closeFilterInput() {
	m.filtering = false
	m.filterInput.Focused = false
}

// updateFilterKey handles one key press while the filter input is open.
// EVERY key is consumed here: j/k and enter would otherwise move and open a
// contact while the user is typing a name that contains them, which is the
// whole point of an explicit input mode.
func (m Model) updateFilterKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.ClearFilter()
		return m, nil
	case "enter":
		m.closeFilterInput()
		return m, nil
	}

	before := m.filterInput.Value
	m.filterInput.Update(msg)
	if m.filterInput.Value == before {
		return m, nil
	}

	m.filter = m.filterInput.Value
	m.applyFilter()
	return m, nil
}

// applyFilter narrows the loaded contacts by the query, case-insensitively,
// over both the name and the username — the two things a person is looked up
// by, and a filter that matched only one of them would be a filter you have
// to guess the shape of.
func (m *Model) applyFilter() {
	if m.filter == "" {
		m.list.SetItems(m.all)
		return
	}

	needle := strings.ToLower(m.filter)
	matched := make([]widgets.ListItem, 0, len(m.all))
	for _, item := range m.all {
		if strings.Contains(strings.ToLower(item.Title), needle) ||
			strings.Contains(strings.ToLower(item.Subtitle), needle) {
			matched = append(matched, item)
		}
	}
	m.list.SetItems(matched)
}

// SetContactsForTest seeds the list without a Telegram client, so a caller
// in another package can render the column. The load path is otherwise the
// only way in, and it needs a live connection.
func (m *Model) SetContactsForTest(users []*telegram.User) {
	m.loaded = true
	m.refreshList(users)
}

type contactsLoadedMsg struct {
	users []*telegram.User
	err   error
}

// LoadContacts fetches the contact list.
func (m *Model) LoadContacts() tea.Cmd {
	return func() tea.Msg {
		users, err := m.tg.GetContacts()
		if err != nil {
			return contactsLoadedMsg{err: err}
		}
		for _, user := range users {
			m.store.Users.Set(user)
		}
		return contactsLoadedMsg{users: users}
	}
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case contactsLoadedMsg:
		m.loaded = true
		if msg.err == nil {
			m.refreshList(msg.users)
		}

	case tea.KeyPressMsg:
		// The filter input, while open, owns every key — including the
		// motions and enter below. Checked before m.focused because the
		// app hands keys to this component precisely because FilterActive
		// is true.
		if m.filtering {
			return m.updateFilterKey(msg)
		}

		if m.focused {
			// esc clears an applied-but-closed filter before it closes the
			// panel, the same ladder the chat list follows: one step per
			// press, and the step that widens the list back comes first.
			if msg.String() == "esc" && m.filter != "" {
				m.ClearFilter()
				return m, nil
			}

			if selected := m.list.Update(msg); selected {
				item := m.list.SelectedItem()
				if item != nil {
					var userID int64
					fmt.Sscanf(item.ID, "%d", &userID)
					return m, func() tea.Msg {
						return ContactSelectedMsg{UserId: userID}
					}
				}
			}

			if msg.String() == "esc" {
				m.visible = false
			}
		}
	}

	return m, nil
}

func (m *Model) refreshList(users []*telegram.User) {
	sort.Slice(users, func(i, j int) bool {
		return users[i].FirstName < users[j].FirstName
	})

	items := make([]widgets.ListItem, 0, len(users))
	for _, user := range users {
		name := user.FirstName
		if user.LastName != "" {
			name += " " + user.LastName
		}

		subtitle := ""
		if user.Username != "" {
			subtitle = "@" + user.Username
		}

		_, online := user.Status.(*telegram.UserStatusOnline)

		items = append(items, widgets.ListItem{
			ID:       fmt.Sprintf("%d", user.ID),
			Title:    name,
			Subtitle: subtitle,
			Online:   online,
		})
	}

	m.all = items
	m.applyFilter()
}

// View renders the contact list into the column the chat list otherwise
// occupies: a filter header and then the rows, on the same grid, with no
// frame of its own.
//
// It used to render a bold "Contacts" heading over a box it sized itself,
// which is the pre-2.0 overlay idiom — a different palette and a different
// geometry from the column it was drawn into. The frame owns the surface;
// this returns lines and lets it.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	// The list is padded out to its budget so the column is its full height
	// whatever it holds, rather than as tall as it has contacts.
	rows := strings.Split(m.list.View(), "\n")
	for len(rows) < m.list.Height {
		rows = append(rows, "")
	}

	return strings.Join(
		append([]string{m.renderFilterHeader(m.width)}, rows...), "\n")
}
