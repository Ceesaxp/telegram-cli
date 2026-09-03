package chatlist

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"

	"github.com/Ceesaxp/telegram-cli/internal/ui/widgets"
)

// fakeDialogs stands in for the client's paging calls.
type fakeDialogs struct {
	more     bool
	pages    int
	perPage  int
	err      error
	requests int
}

func (f *fakeDialogs) MoreChatsToLoad() bool { return f.more }

func (f *fakeDialogs) LoadMoreChats(int) (int, error) {
	f.requests++
	if f.err != nil {
		return 0, f.err
	}
	if f.pages == 0 {
		f.more = false
		return 0, nil
	}
	f.pages--
	return f.perPage, nil
}

// listOf builds a model whose list holds n rows, with the cursor at index.
func listOf(t *testing.T, n, cursor int) Model {
	t.Helper()
	m := newLoadedModel(t)

	items := make([]widgets.ListItem, 0, n)
	for i := range n {
		items = append(items, widgets.ListItem{ID: itoa(i + 1), Title: "chat"})
	}
	m.list.SetItems(items)
	m.list.Cursor = cursor
	return m
}

// TestTheNextPageIsAskedForBeforeTheReaderRunsOut.
//
// Ahead of the bottom rather than at it: rows that arrive when the cursor is
// already on the last one arrive after the reader has noticed there are none
// left. The request was going to happen anyway; asking early only changes
// when.
func TestTheNextPageIsAskedForBeforeTheReaderRunsOut(t *testing.T) {
	for _, tc := range []struct {
		name          string
		items, cursor int
		want          bool
	}{
		{"at the very bottom", 100, 99, true},
		{"within the trigger", 100, 100 - pageAheadTrigger, true},
		{"just outside it", 100, 100 - pageAheadTrigger - 1, false},
		{"at the top of a long list", 100, 0, false},
		{"a list shorter than the trigger is always near its end", 3, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := listOf(t, tc.items, tc.cursor)
			if got := m.shouldPageAhead(); got != tc.want {
				t.Errorf("shouldPageAhead() = %v with the cursor at %d of %d, want %v",
					got, tc.cursor, tc.items, tc.want)
			}
		})
	}
}

// TestOnePageAtATime. Holding j at the bottom of the list must ask once, not
// once per keystroke — every one of those is a round trip to Telegram.
func TestOnePageAtATime(t *testing.T) {
	m := listOf(t, 50, 49)
	if !m.shouldPageAhead() {
		t.Fatal("precondition: the cursor is not near the end")
	}

	m.pagingMore = true
	if m.shouldPageAhead() {
		t.Error("a second page was requested while the first was still in flight")
	}
}

// TestAFilteredListIsNotAReasonToPage.
//
// Filtering narrows what is already loaded, so reaching the end of three
// matches says nothing about how much dialog list is left — and paging on it
// would fetch the whole account one keystroke at a time for anyone who types
// a filter that matches little.
func TestAFilteredListIsNotAReasonToPage(t *testing.T) {
	m := listOf(t, 3, 2)
	if !m.shouldPageAhead() {
		t.Fatal("precondition: a short list should otherwise page")
	}

	m.filter = "alpha"
	if m.shouldPageAhead() {
		t.Error("the end of a filtered list asked for more dialogs")
	}
}

// TestNothingIsAskedForWhileTheFirstLoadIsRunning.
func TestNothingIsAskedForWhileTheFirstLoadIsRunning(t *testing.T) {
	m := listOf(t, 3, 0)
	m.loading = true
	if m.shouldPageAhead() {
		t.Error("a page was requested before the first load finished")
	}
}

// TestAFailedPageDoesNotWedgeTheList. The in-flight flag has to clear on the
// way out however it went, or one network blip means the list never grows
// again for the rest of the session.
func TestAFailedPageDoesNotWedgeTheList(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  moreChatsLoadedMsg
	}{
		{"after a failure", moreChatsLoadedMsg{err: errors.New("network")}},
		{"after an empty page", moreChatsLoadedMsg{count: 0}},
		{"after a full one", moreChatsLoadedMsg{count: 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := listOf(t, 50, 49)
			m.pagingMore = true

			m, _ = m.Update(tc.msg)
			if m.pagingMore {
				t.Error("the in-flight flag survived; the list will never ask again")
			}
		})
	}
}

// TestNoClientMeansNoRequest — the model is constructed without one in
// several tests, and a nil dereference there would be a crash on a keystroke.
func TestNoClientMeansNoRequest(t *testing.T) {
	m := listOf(t, 3, 2)
	m.tg, m.dialogs = nil, nil
	if cmd := m.pageAheadCmd(); cmd != nil {
		t.Error("a model with no client produced a page request")
	}
}

// TestAnExhaustedListStopsAsking. Once the server has said there is no
// more, every keystroke at the bottom would otherwise be a round trip that
// can only come back empty.
func TestAnExhaustedListStopsAsking(t *testing.T) {
	m := listOf(t, 50, 49)
	m.dialogs = &fakeDialogs{more: false}

	if cmd := m.pageAheadCmd(); cmd != nil {
		t.Error("an exhausted list asked for another page")
	}
}

// TestMovingTheCursorIsWhatAsks. The rules being right is no use if nothing
// consults them: this is the wiring from a keystroke to the request.
func TestMovingTheCursorIsWhatAsks(t *testing.T) {
	m := listOf(t, 50, 48)
	fake := &fakeDialogs{more: true, pages: 1, perPage: 50}
	m.dialogs = fake
	m.SetFocused(true)

	m, cmd := m.Update(specialKey(tea.KeyDown))
	if cmd == nil {
		t.Fatal("moving the cursor to the bottom produced no command at all")
	}
	// Run whatever the batch holds; only the page request answers.
	if msg := cmd(); msg != nil {
		if batch, ok := msg.([]tea.Msg); ok {
			for _, sub := range batch {
				_ = sub
			}
		}
	}
	if fake.requests == 0 && !m.pagingMore {
		t.Error("no page was requested when the cursor reached the bottom")
	}
}

// TestTheNewRowsAreDrawn. A page that arrives and is not put on screen is a
// request nobody can see the result of.
func TestTheNewRowsAreDrawn(t *testing.T) {
	m := newLoadedModel(t, "Alice")
	m.SetSize(40, 20)
	_ = m.View()
	before := len(m.list.Items)

	m.store.Chats.Set(&telegram.Chat{ID: 900, Title: "Later Chat", Type: telegram.ChatTypePrivate})
	m, _ = m.Update(moreChatsLoadedMsg{count: 1})

	if len(m.list.Items) <= before {
		t.Errorf("the list still holds %d rows after a page arrived", len(m.list.Items))
	}
}

// TestOpeningAChatNearTheBottomDoesNotWedgePaging.
//
// pageAheadCmd does two things at once: it marks a request in flight and
// returns the command that clears the mark. A caller that drops the command
// therefore disables paging for the rest of the session — and the selection
// branch used to do exactly that, returning ChatSelectedMsg on its own.
//
// Pressing Enter near the bottom of the list, or G then Enter, was enough.
func TestOpeningAChatNearTheBottomDoesNotWedgePaging(t *testing.T) {
	m := listOf(t, 50, 49)
	fake := &fakeDialogs{more: true, pages: 5, perPage: 50}
	m.dialogs = fake
	m.SetFocused(true)

	m, cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter produced no command at all")
	}
	if !m.pagingMore {
		// Fine in itself — but then the request was never made, and the
		// test below would not be testing anything.
		t.Fatal("precondition: enter did not start a page request")
	}

	// The command that clears the flag has to be among what was returned.
	drain(t, cmd, &m)
	if m.pagingMore {
		t.Fatal("the page request was charged for and its command discarded; " +
			"paging is now off for the session")
	}

	// And the list can still page afterwards.
	if !m.shouldPageAhead() {
		t.Error("the list refuses to page after a chat was opened near the bottom")
	}
}

// TestSelectingAChatStillReportsIt — the behaviour the early return existed
// for must survive being folded into the batch.
func TestSelectingAChatStillReportsIt(t *testing.T) {
	m := listOf(t, 50, 49)
	m.dialogs = &fakeDialogs{more: true, pages: 1, perPage: 50}
	m.SetFocused(true)

	m, cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter produced no command")
	}

	var selected *ChatSelectedMsg
	walk(cmd, func(msg tea.Msg) {
		if sel, ok := msg.(ChatSelectedMsg); ok {
			selected = &sel
		}
	})
	if selected == nil {
		t.Fatal("enter no longer reports the selected chat")
	}
	if selected.ChatId != 50 {
		t.Errorf("selected chat %d, want the cursored 50", selected.ChatId)
	}
}

// TestTheWheelPagesToo. The mouse moves the same cursor the keyboard does,
// and a chat list that only grew for keyboard users would be a surprising
// place to draw that line.
func TestTheWheelPagesToo(t *testing.T) {
	m := listOf(t, 50, 40)
	fake := &fakeDialogs{more: true, pages: 1, perPage: 50}
	m.dialogs = fake

	// The FIRST scroll into the trigger zone is the one that asks; the rest
	// correctly return nil while that request is in flight.
	var asked tea.Cmd
	for range 9 {
		if cmd := m.ScrollBy(1); cmd != nil && asked == nil {
			asked = cmd
		}
	}
	if asked == nil {
		t.Fatal("scrolling to the bottom asked for nothing")
	}
	if !m.pagingMore {
		t.Error("the wheel reached the end of the list without requesting a page")
	}

	// And the command it handed back is the one that clears the flag, so a
	// caller which returns it keeps paging alive.
	drain(t, asked, &m)
	if m.pagingMore {
		t.Error("the wheel's command did not clear the in-flight flag")
	}
}

// drain runs cmd, and anything it batches, back into the model.
func drain(t *testing.T, cmd tea.Cmd, m *Model) {
	t.Helper()
	walk(cmd, func(msg tea.Msg) {
		*m, _ = m.Update(msg)
	})
}

// walk runs cmd and hands every message it produces to fn, descending into
// batches.
func walk(cmd tea.Cmd, fn func(tea.Msg)) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch v := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, sub := range v {
			walk(sub, fn)
		}
	default:
		fn(msg)
	}
}
