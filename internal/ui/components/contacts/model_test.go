package contacts

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func people() []*telegram.User {
	return []*telegram.User{
		{ID: 1, FirstName: "Alice", LastName: "Anderson", Username: "alice"},
		{ID: 2, FirstName: "Bob", LastName: "Brown", Username: "bobby"},
		{ID: 3, FirstName: "Carol", LastName: "Clark", Username: "carol"},
	}
}

// loaded is a visible, focused, sized contacts panel holding three people —
// the state every test below starts from.
func loaded(t *testing.T) Model {
	t.Helper()
	m := New(store.NewStore(), nil, theme.DarkRoles(false))
	m.SetSize(38, 12)
	m.SetVisible(true)
	m.SetFocused(true)
	m.SetContactsForTest(people())
	if got := len(m.list.Items); got != 3 {
		t.Fatalf("seeded %d contacts, want 3", got)
	}
	return m
}

func press(m Model, key string) Model {
	var msg tea.KeyPressMsg
	switch key {
	case "esc":
		msg = tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	case "enter":
		msg = tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	default:
		r := []rune(key)[0]
		msg = tea.KeyPressMsg(tea.Key{Code: r, Text: key})
	}
	m, _ = m.Update(msg)
	return m
}

func typeIn(m Model, text string) Model {
	for _, r := range text {
		m = press(m, string(r))
	}
	return m
}

// --- the column, drawn like the column it borrows -------------------------

// TestEveryDrawnLineIsExactlyWide is the contract the frame beside it
// depends on: this panel takes the chat list's region, and a line that
// overshoots is clipped rather than shearing the screen.
//
// The rows BELOW the last contact are empty strings rather than runs of
// spaces, which is deliberate and is what the chat list does too: the frame
// fills the column's surface, and padding it here as well would be a second
// mechanism for one rule.
func TestEveryDrawnLineIsExactlyWide(t *testing.T) {
	for _, width := range []int{20, 38, 60} {
		m := loaded(t)
		m.SetSize(width, 12)
		// The header plus two lines per contact; everything after is the
		// frame's padding.
		drawn := 1 + 2*len(m.list.Items)
		for i, line := range strings.Split(m.View(), "\n") {
			if i >= drawn {
				if line != "" {
					t.Errorf("width %d: padding line %d is not empty: %q",
						width, i, ansi.Strip(line))
				}
				continue
			}
			if got := cell.Width(line); got != width {
				t.Errorf("width %d: line %d is %d cells: %q",
					width, i, got, ansi.Strip(line))
			}
		}
	}
}

// TestTheColumnIsAsTallAsItsBudget: the header plus the list rows fill the
// region, so the column does not end halfway down with the frame's surface
// below it.
func TestTheColumnIsAsTallAsItsBudget(t *testing.T) {
	m := loaded(t)
	if got, want := len(strings.Split(m.View(), "\n")), 12; got != want {
		t.Errorf("the column is %d rows, want %d", got, want)
	}
}

// TestAnUnselectedRowLeavesTheSurfaceToTheFrame. Panel is the column's
// surface and the frame fills it, including the rows below the last contact
// — painting it here as well would be a second mechanism for one rule, and
// the one that cannot cover the padding. The overlay styling this replaced
// painted a panel background on every row.
func TestAnUnselectedRowLeavesTheSurfaceToTheFrame(t *testing.T) {
	m := loaded(t)
	item := m.list.Items[0]

	for _, line := range m.renderRow(item, false, false, 38) {
		if p := cell.PaintedWidth(line); p != 0 {
			t.Errorf("an unselected row painted %d cells:\n%s",
				p, strings.ReplaceAll(line, "\x1b", "ESC"))
		}
	}
	if line := m.renderFilterHeader(38); cell.PaintedWidth(line) != 0 {
		t.Errorf("the filter header painted %d cells:\n%s",
			cell.PaintedWidth(line), strings.ReplaceAll(line, "\x1b", "ESC"))
	}

	// The selected row is the exception, and paints the whole width.
	for _, line := range m.renderRow(item, true, true, 38) {
		if p := cell.PaintedWidth(line); p != 38 {
			t.Errorf("the selected row painted %d cells, want 38", p)
		}
	}
}

// TestTheSelectionBarRunsDownBothRows, as it does in the chat list: a mark
// on the name line only marks the name, and the username underneath reads as
// belonging to no row in particular.
func TestTheSelectionBarRunsDownBothRows(t *testing.T) {
	m := loaded(t)
	for i, line := range m.renderRow(m.list.Items[0], true, true, 38) {
		if runes := []rune(ansi.Strip(line)); len(runes) == 0 || runes[0] != '▌' {
			t.Errorf("row %d does not start with the selection bar: %q",
				i, ansi.Strip(line))
		}
	}
}

// TestTheRowIsTheChatListsGrid: same column offsets, because the two
// surfaces swap into one region and a reader whose eye has learned where a
// name starts should not have to relearn it.
func TestTheRowIsTheChatListsGrid(t *testing.T) {
	m := loaded(t)
	lines := m.renderRow(m.list.Items[0], false, false, 38)
	if len(lines) != 2 {
		t.Fatalf("a contact is %d lines, want 2", len(lines))
	}

	name := ansi.Strip(lines[0])
	if got := name[rowSigilCol]; got != '@' {
		t.Errorf("the sigil column holds %q, want the private-chat @", string(got))
	}
	if !strings.HasPrefix(name[rowTextCol:], "Alice Anderson") {
		t.Errorf("the name does not start at column %d: %q", rowTextCol, name)
	}
	if user := ansi.Strip(lines[1]); !strings.HasPrefix(user[rowTextCol:], "@alice") {
		t.Errorf("the username does not start at column %d: %q", rowTextCol, user)
	}
}

// TestTheHeaderNamesTheSurface. The bold "Contacts" heading is gone; the
// filter row's placeholder is what says which list this is, in the row that
// also lets you narrow it.
func TestTheHeaderNamesTheSurface(t *testing.T) {
	m := loaded(t)
	header := ansi.Strip(m.renderFilterHeader(38))
	if !strings.Contains(header, "filter contacts") {
		t.Errorf("the header does not name the surface: %q", header)
	}
	if !strings.Contains(header, "3/3") {
		t.Errorf("the header does not count the contacts: %q", header)
	}
}

// --- the filter -----------------------------------------------------------

// TestFilterNarrowsByNameAndUsername: those are the two things a person is
// looked up by, and a filter matching only one is one you have to guess the
// shape of.
func TestFilterNarrowsByNameAndUsername(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  string
	}{
		{"ali", "Alice Anderson"}, // first name
		{"brown", "Bob Brown"},    // last name
		{"bobby", "Bob Brown"},    // username, which differs from the name
		{"CAROL", "Carol Clark"},  // case-insensitive
	} {
		t.Run(tc.query, func(t *testing.T) {
			m := loaded(t)
			m.OpenFilter()
			m = typeIn(m, tc.query)

			if got := len(m.list.Items); got != 1 {
				t.Fatalf("%q matched %d contacts, want 1", tc.query, got)
			}
			if got := m.list.Items[0].Title; got != tc.want {
				t.Errorf("%q matched %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

// TestTheFilterOwnsTheKeyboard: j, k and enter are letters in somebody's
// name while a query is being typed, not motions. That is the whole point of
// an explicit input mode.
func TestTheFilterOwnsTheKeyboard(t *testing.T) {
	m := loaded(t)
	m.OpenFilter()

	before := m.list.Cursor
	m = typeIn(m, "j")
	if m.list.Cursor != before {
		t.Error("j moved the cursor while the filter input was open")
	}
	if m.filterInput.Value != "j" {
		t.Errorf("the query is %q, want the typed j", m.filterInput.Value)
	}

	// enter closes the input and KEEPS the filter, rather than opening a
	// contact.
	m, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd != nil {
		t.Errorf("enter inside the filter produced %T", cmd())
	}
	if m.FilterActive() {
		t.Error("enter left the input open")
	}
	if m.FilterQuery() != "j" {
		t.Errorf("enter dropped the filter: %q", m.FilterQuery())
	}
}

// TestEveryKeyAfterOpenFilterIsQueryText, the first one included.
//
// internal/app matches keys.search, calls OpenFilter and RETURNS — this
// component never sees the key that opened it. So there is nothing to
// swallow, and swallowing anyway is what made a query starting with "/"
// impossible to type: the latch was still armed when the user's own slash
// arrived. There is no self-binding here to need one.
func TestEveryKeyAfterOpenFilterIsQueryText(t *testing.T) {
	m := loaded(t)
	m.OpenFilter()

	m = press(m, "/")
	if m.filterInput.Value != "/" {
		t.Errorf("the first typed slash was swallowed: %q", m.filterInput.Value)
	}
	m = typeIn(m, "a/b")
	if m.filterInput.Value != "/a/b" {
		t.Errorf("query = %q, want every slash to survive", m.filterInput.Value)
	}
}

// TestEscWidensBeforeItCloses is the Esc ladder, one step per press: the
// step that gives the contacts back comes before the one that gives the
// panel back.
func TestEscWidensBeforeItCloses(t *testing.T) {
	m := loaded(t)
	m.OpenFilter()
	m = typeIn(m, "ali")
	if len(m.list.Items) != 1 {
		t.Fatalf("precondition: the filter matched %d", len(m.list.Items))
	}

	// Inside the input, esc clears and closes it — and leaves the panel up.
	m = press(m, "esc")
	if m.FilterActive() || m.FilterQuery() != "" {
		t.Errorf("esc left the filter: active=%v query=%q", m.FilterActive(), m.FilterQuery())
	}
	if len(m.list.Items) != 3 {
		t.Errorf("esc did not restore the whole list: %d", len(m.list.Items))
	}
	if !m.IsVisible() {
		t.Error("the first esc closed the panel as well as the filter")
	}

	// Applied but closed: esc clears that too, still without closing.
	m.OpenFilter()
	m = typeIn(m, "ali")
	m = press(m, "enter")
	m = press(m, "esc")
	if m.FilterQuery() != "" {
		t.Errorf("esc did not clear the applied filter: %q", m.FilterQuery())
	}
	if !m.IsVisible() {
		t.Error("esc closed the panel instead of clearing the filter")
	}

	// With nothing to clear, esc closes.
	m = press(m, "esc")
	if m.IsVisible() {
		t.Error("esc with no filter did not close the panel")
	}
}

// TestTheHeaderCountsWhatIsShown, so a list that fell from twelve to three
// says why rather than reading as contacts going missing.
func TestTheHeaderCountsWhatIsShown(t *testing.T) {
	m := loaded(t)
	m.OpenFilter()
	m = typeIn(m, "ali")

	if got := ansi.Strip(m.renderFilterHeader(38)); !strings.Contains(got, "1/3") {
		t.Errorf("the header reads %q, want it to show 1 of 3", got)
	}
}

// TestClickAtAccountsForTheHeaderRow: local row 0 is the filter header, so
// the first contact starts at row 1 and a click must not be attributed one
// row early.
func TestClickAtAccountsForTheHeaderRow(t *testing.T) {
	m := loaded(t)

	if _, ok := m.ClickAt(0); ok {
		t.Error("a click on the filter header selected a contact")
	}
	id, ok := m.ClickAt(1)
	if !ok {
		t.Fatal("a click on the first contact row selected nothing")
	}
	if id != 1 {
		t.Errorf("the first row is contact %d, want 1", id)
	}
}
