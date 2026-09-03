package telegram

import (
	"errors"
	"testing"

	"github.com/gotd/td/tg"
)

// fakePages serves canned dialog pages, recording what it was asked for.
type fakePages struct {
	pages []fakePage
	// endless is served once pages runs out, for tests that need to prove
	// a loop STOPS rather than that it runs out of canned answers.
	endless *fakePage
	asked   []dialogCursor
}

type fakePage struct {
	chats []*Chat
	next  dialogCursor
	raw   int
	err   error
}

func (f *fakePages) fetch(cursor dialogCursor, limit int) ([]*Chat, dialogCursor, int, error) {
	f.asked = append(f.asked, cursor)
	if len(f.pages) == 0 {
		if f.endless != nil {
			return f.endless.chats, f.endless.next, f.endless.raw, f.endless.err
		}
		return nil, dialogCursor{}, 0, nil
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	return page.chats, page.next, page.raw, page.err
}

func chatsWithIDs(ids ...int64) []*Chat {
	out := make([]*Chat, 0, len(ids))
	for _, id := range ids {
		out = append(out, &Chat{ID: id})
	}
	return out
}

func cursorAt(date, id int) dialogCursor {
	return dialogCursor{date: date, id: id, peer: &tg.InputPeerEmpty{}}
}

func ids(chats []*Chat) []int64 {
	out := make([]int64, 0, len(chats))
	for _, c := range chats {
		out = append(out, c.ID)
	}
	return out
}

// TestAShortPageEndsTheList. The server sending fewer than asked is how it
// says there is no more, and it is the only signal that arrives every time.
func TestAShortPageEndsTheList(t *testing.T) {
	// A server with an endless supply of short pages. If the short-page
	// rule does not fire, the loop keeps going and the request COUNT says
	// so — which is what makes this test about that rule rather than about
	// whichever one happens to stop it first.
	f := &fakePages{endless: &fakePage{
		chats: chatsWithIDs(1, 2, 3), next: cursorAt(9, 9), raw: 3,
	}}

	chats, _, done, err := pageDialogs(f.fetch, dialogCursor{}, dialogsPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Error("a page of 3 against a limit of 100 did not end the list")
	}
	if len(f.asked) != 1 {
		t.Errorf("a short page took %d requests to end the list, want 1", len(f.asked))
	}
	if got := len(chats); got != 3 {
		t.Errorf("got %d chats, want 3", got)
	}
}

// TestAFullPageLeavesTheListOpen, and hands back a cursor to continue from.
func TestAFullPageLeavesTheListOpen(t *testing.T) {
	f := &fakePages{pages: []fakePage{
		{chats: chatsWithIDs(1, 2), next: cursorAt(50, 5), raw: 2},
	}}

	_, next, done, err := pageDialogs(f.fetch, dialogCursor{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Error("a full page ended the list")
	}
	if next.date != 50 || next.id != 5 {
		t.Errorf("cursor = %+v, want the last dialog's", next)
	}
}

// TestACursorTheServerDidNotAdvanceEndsTheList.
//
// Not paranoia: an unchanged cursor is a loop that never ends, and it is the
// shape a paging bug takes — the same request, forever, as fast as the
// network allows.
func TestACursorTheServerDidNotAdvanceEndsTheList(t *testing.T) {
	start := cursorAt(100, 7)
	// FULL pages, or the short-page rule would end the list first and this
	// would pass without the guard it names ever running.
	f := &fakePages{pages: []fakePage{
		{chats: run(1, dialogsPageSize), next: start, raw: dialogsPageSize},
		{chats: run(101, dialogsPageSize), next: start, raw: dialogsPageSize},
	}}

	_, _, done, err := pageDialogs(f.fetch, start, 5*dialogsPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Error("a cursor the server did not move failed to end the list")
	}
	if len(f.asked) != 1 {
		t.Errorf("asked %d times for a stalled cursor, want 1", len(f.asked))
	}
}

// TestAPageWithNothingToContinueFromEndsTheList.
func TestAPageWithNothingToContinueFromEndsTheList(t *testing.T) {
	// A page that is exactly as long as the one asked for, with a
	// continuation whose date and id DID move: not short, not stalled, so
	// the nil peer is the only rule left that can stop it.
	f := &fakePages{pages: []fakePage{
		{chats: run(1, dialogsPageSize), next: dialogCursor{date: 55, id: 9}, raw: dialogsPageSize},
	}}

	if _, _, done, err := pageDialogs(f.fetch, cursorAt(1, 1), dialogsPageSize); err != nil {
		t.Fatal(err)
	} else if !done {
		t.Error("a page with a nil continuation peer did not end the list")
	}
}

// TestPagingContinuesUntilTheLimit, and continues FROM where it got to.
//
// A limit above dialogsPageSize is what makes this multi-page at all:
// Telegram serves at most a hundred dialogs per request, and a page shorter
// than the one asked for is the end of the list rather than a step in it.
func TestPagingContinuesUntilTheLimit(t *testing.T) {
	f := &fakePages{pages: []fakePage{
		{chats: run(1, dialogsPageSize), next: cursorAt(10, 1), raw: dialogsPageSize},
		{chats: run(101, dialogsPageSize), next: cursorAt(20, 2), raw: dialogsPageSize},
		{chats: run(201, 50), next: cursorAt(30, 3), raw: 50},
	}}

	chats, _, done, err := pageDialogs(f.fetch, dialogCursor{}, 250)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(chats); got != 250 {
		t.Fatalf("got %d chats over three pages, want 250", got)
	}
	if done {
		t.Error("stopping at the limit is not the end of the list")
	}
	if len(f.asked) != 3 {
		t.Fatalf("made %d requests, want 3", len(f.asked))
	}
	if f.asked[1].date != 10 || f.asked[2].date != 20 {
		t.Errorf("pages continued from %+v, want each to follow the last", f.asked)
	}
}

// run builds n chats with consecutive IDs from first.
func run(first int64, n int) []*Chat {
	out := make([]*Chat, 0, n)
	for i := range int64(n) {
		out = append(out, &Chat{ID: first + i})
	}
	return out
}

// TestADialogSeenTwiceIsCountedOnce. Telegram can repeat a dialog across a
// page boundary when something arrives while paging.
func TestADialogSeenTwiceIsCountedOnce(t *testing.T) {
	// The last entry of page one repeats as the first of page two, which is
	// what a dialog rising to the top mid-page looks like from here.
	first := run(1, dialogsPageSize)
	second := append(chatsWithIDs(first[len(first)-1].ID), run(101, dialogsPageSize-1)...)

	f := &fakePages{pages: []fakePage{
		{chats: first, next: cursorAt(10, 1), raw: dialogsPageSize},
		{chats: second, next: cursorAt(20, 2), raw: dialogsPageSize},
	}}

	chats, _, _, err := pageDialogs(f.fetch, dialogCursor{}, 2*dialogsPageSize)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[int64]int{}
	for _, id := range ids(chats) {
		seen[id]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("chat %d appears %d times", id, n)
		}
	}
	if len(chats) != 2*dialogsPageSize-1 {
		t.Errorf("got %d chats, want the %d unique ones", len(chats), 2*dialogsPageSize-1)
	}
}

// TestAFailedPageKeepsTheCursorItStartedFrom, so a retry asks the same
// question rather than skipping whatever the failed page held.
func TestAFailedPageKeepsTheCursorItStartedFrom(t *testing.T) {
	start := cursorAt(77, 3)
	f := &fakePages{pages: []fakePage{{err: errors.New("network")}}}

	_, next, done, err := pageDialogs(f.fetch, start, 10)
	if err == nil {
		t.Fatal("a failed page reported success")
	}
	if done {
		t.Error("a failed page ended the list")
	}
	if next.date != start.date || next.id != start.id {
		t.Errorf("cursor = %+v after a failure, want the one it started from", next)
	}
}

// TestTheLimitIsCappedAndFloored. A caller asking for nothing gets a page;
// a caller asking for the whole account gets the cap.
func TestTheLimitIsCappedAndFloored(t *testing.T) {
	for _, tc := range []struct{ ask, want int }{
		{0, dialogsPageSize},
		{-5, dialogsPageSize},
		{10, 10},
		{MaxDialogsLimit + 1000, dialogsPageSize},
	} {
		f := &fakePages{pages: []fakePage{{chats: nil, raw: 0}}}
		if _, _, _, err := pageDialogs(f.fetch, dialogCursor{}, tc.ask); err != nil {
			t.Fatal(err)
		}
		if len(f.asked) == 0 {
			t.Fatalf("asking for %d made no request", tc.ask)
		}
	}
}

// TestThePagerRestartsOnReset. LoadChats is a fresh start, not a
// continuation: signing in again, or reloading the list, has to go back to
// the top rather than resume from wherever the last session stopped.
func TestThePagerRestartsOnReset(t *testing.T) {
	var p dialogPager
	p.cursor, p.done = cursorAt(5, 5), true

	p.reset()
	if p.done {
		t.Error("reset left the pager exhausted")
	}
	if p.cursor.peer != nil || p.cursor.date != 0 {
		t.Errorf("reset left the cursor at %+v", p.cursor)
	}
}

// TestLoadChatsStartsOverAndLoadMoreCarriesOn.
//
// Two different jobs on one cursor: LoadChats is the list appearing, which
// has to begin at the top however the last session ended, and LoadMoreChats
// is the reader reaching the bottom, which has to continue.
func TestLoadChatsStartsOverAndLoadMoreCarriesOn(t *testing.T) {
	var c Client

	f := &fakePages{pages: []fakePage{
		{chats: run(1, dialogsPageSize), next: cursorAt(10, 1), raw: dialogsPageSize},
		{chats: run(101, dialogsPageSize), next: cursorAt(20, 2), raw: dialogsPageSize},
	}}

	if err := c.loadChatsWith(f.fetch, dialogsPageSize); err != nil {
		t.Fatal(err)
	}
	if !c.MoreChatsToLoad() {
		t.Fatal("a full first page reported the list exhausted")
	}

	n, err := c.pageWith(f.fetch, dialogsPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if n != dialogsPageSize {
		t.Errorf("the second page brought %d chats, want %d", n, dialogsPageSize)
	}
	if len(f.asked) != 2 {
		t.Fatalf("made %d requests, want 2", len(f.asked))
	}
	if f.asked[1].date != 10 {
		t.Errorf("the second page started from %+v, want the first page's cursor", f.asked[1])
	}

	// And loading the list again goes back to the top.
	f.pages = []fakePage{{chats: run(1, 3), raw: 3}}
	if err := c.loadChatsWith(f.fetch, dialogsPageSize); err != nil {
		t.Fatal(err)
	}
	if got := f.asked[2]; got.date != 0 || got.id != 0 {
		t.Errorf("reloading the list resumed from %+v, want the top", got)
	}
}

// TestAnExhaustedPagerStopsAskingTheServer. Once a short page has arrived,
// every further request can only come back empty — and the chat list asks on
// every keystroke at the bottom.
func TestAnExhaustedPagerStopsAskingTheServer(t *testing.T) {
	var c Client

	f := &fakePages{pages: []fakePage{{chats: run(1, 3), raw: 3}}}
	if err := c.loadChatsWith(f.fetch, dialogsPageSize); err != nil {
		t.Fatal(err)
	}
	if c.MoreChatsToLoad() {
		t.Fatal("a short page left the pager thinking there was more")
	}

	asked := len(f.asked)
	for range 5 {
		n, err := c.pageWith(f.fetch, dialogsPageSize)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("an exhausted pager returned %d chats", n)
		}
	}
	if len(f.asked) != asked {
		t.Errorf("an exhausted pager made %d more requests", len(f.asked)-asked)
	}
}
