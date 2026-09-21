package chatview

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// g@ goes to the next unread mention, oldest first, the way the @ button
// on the official clients walks them. Asking which mentions are unread is
// a round trip, so the key returns the question and the answer comes back
// as mentionsListedMsg; these tests hand the panel that answer directly.

// reachesClient runs cmd and reports whether it went to the client. The
// fixtures' client has no connection, so a command that calls it panics,
// and the panic is recovered and reported as the call it is.
func reachesClient(cmd tea.Cmd) (reached bool) {
	defer func() {
		if recover() != nil {
			reached = true
		}
	}()
	cmd()
	return false
}

// g is a prefix, and @ is its suffix for the next mention. What it starts
// is a question to the server, not a move: only the server knows which
// mentions are still unread.
func TestGThenAtAsksForTheUnreadMentions(t *testing.T) {
	m := openQuietMentionChat(t)

	m, cmd := press(m, "g", "@")
	if m.pendingG {
		t.Error("g@ left the prefix armed")
	}
	if cmd == nil {
		t.Fatal("g@ produced no command")
	}
	if !reachesClient(cmd) {
		t.Fatal("g@'s command did not ask the client for the unread mentions")
	}
}

// mentionJump runs what the panel returned for a listing and sorts it into
// the jump it hands the host and the number of commands that went to the
// client, which are the clears.
func mentionJump(t *testing.T, cmd tea.Cmd) (MentionJumpMsg, int) {
	t.Helper()
	var (
		jump   MentionJumpMsg
		jumps  int
		clears int
	)
	for _, c := range runBatch(t, cmd) {
		if reachesClient(c) {
			clears++
			continue
		}
		if got, ok := c().(MentionJumpMsg); ok {
			jump = got
			jumps++
		}
	}
	if jumps != 1 {
		t.Fatalf("the listing handed the host %d jumps, want 1", jumps)
	}
	return jump, clears
}

// The oldest unread mention is where g@ goes. Landing there is the reader
// choosing to look at it, so it is cleared at once rather than after a
// window, and the host is handed the jump: the panel reads, the host
// navigates.
func TestAListingJumpsToTheOldestMentionAndClearsIt(t *testing.T) {
	m := openQuietMentionChat(t)

	m, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5, 9}})
	jump, clears := mentionJump(t, cmd)
	if jump.ChatId != testChatID || jump.MessageId != 5 {
		t.Errorf("jumped to chat %d message %d, want chat %d message 5",
			jump.ChatId, jump.MessageId, testChatID)
	}
	if jump.Remaining < 1 {
		t.Errorf("Remaining = %d with 9 still unread, want at least 1", jump.Remaining)
	}
	if clears != 1 {
		t.Errorf("the jump sent %d clears, want one for the mention it lands on", clears)
	}
	if !m.askedMentions.has(testChatID, 5) {
		t.Error("the mention jumped to was not recorded as asked")
	}
}

// g@ again goes to the one after. The server may not have cleared the
// first yet when the second listing is taken, and the listing then still
// starts with it; the ledger is what moves the reader on rather than back
// to where they are.
func TestAgainGoesToTheMentionAfter(t *testing.T) {
	m := openQuietMentionChat(t)
	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5, 9}})

	m, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5, 9}})
	jump, clears := mentionJump(t, cmd)
	if jump.MessageId != 9 {
		t.Errorf("the second g@ jumped to %d, want 9", jump.MessageId)
	}
	if clears != 1 {
		t.Errorf("the second jump sent %d clears, want one, for 9 alone", clears)
	}
}

// setMentionCount sets how many unread mentions the store counts for the
// open chat.
func setMentionCount(t *testing.T, m Model, n int32) {
	t.Helper()
	entry, ok := m.store.Chats.Get(testChatID)
	if !ok {
		t.Fatal("the fixture chat is not in the store")
	}
	entry.UnreadMentionsCount = n
}

// How many are left after this one. The listing is capped, so a chat
// with more mentions than a page says less than the store's count does,
// and the store's count can be behind the listing too. The larger of the
// two is the better guess, and none is never less than none.
func TestRemainingIsTheLargerOfTheListingAndTheCount(t *testing.T) {
	for name, tc := range map[string]struct {
		ids   []int64
		count int32
		want  int
	}{
		"the count knows of more than the page": {[]int64{5, 9}, 25, 24},
		"the page knows of more than the count": {[]int64{5, 9, 12}, 1, 2},
		"the last one":                          {[]int64{5}, 1, 0},
		"a count already behind":                {[]int64{5}, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			m := openQuietMentionChat(t)
			setMentionCount(t, m, tc.count)

			_, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: tc.ids})
			if jump, _ := mentionJump(t, cmd); jump.Remaining != tc.want {
				t.Errorf("Remaining = %d for %v with %d counted, want %d",
					jump.Remaining, tc.ids, tc.count, tc.want)
			}
		})
	}
}

// With nothing left to go to, g@ says so rather than doing nothing: a key
// that visibly does nothing reads as a broken key.
func TestAnEmptyListingSaysThereAreNoMentions(t *testing.T) {
	m := openQuietMentionChat(t)

	m, cmd := m.Update(mentionsListedMsg{chatID: testChatID})
	if m.notice != "no unread mentions" {
		t.Errorf("notice = %q, want %q", m.notice, "no unread mentions")
	}
	if cmd != nil {
		t.Error("an empty listing with nothing counted produced a command")
	}
}

// When the server lists none but the store still counts some, the count
// is wrong: a mention cleared on another device, or read here in a way
// this client did not see. The @ on the row is corrected locally, through
// the message the chat list already takes a clear from.
func TestAnEmptyListingCorrectsAStaleCount(t *testing.T) {
	m := openQuietMentionChat(t)
	setMentionCount(t, m, 2)

	m, cmd := m.Update(mentionsListedMsg{chatID: testChatID})
	if m.notice != "no unread mentions" {
		t.Errorf("notice = %q, want %q", m.notice, "no unread mentions")
	}
	if cmd == nil {
		t.Fatal("a stale count was left standing")
	}
	want := telegram.ChatMentionsReadMsg{ChatId: testChatID, All: true}
	if got, ok := cmd().(telegram.ChatMentionsReadMsg); !ok || got.ChatId != want.ChatId || !got.All {
		t.Errorf("the correction was %#v, want %#v", got, want)
	}
}

// A listing whose every mention this client has already asked to clear
// is, from where the reader stands, an empty one.
func TestAListingOfAskedMentionsOnlyIsEmpty(t *testing.T) {
	m := openQuietMentionChat(t)
	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5}})

	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5}})
	if m.notice != "no unread mentions" {
		t.Errorf("notice = %q, want %q", m.notice, "no unread mentions")
	}
}

// The listing is a round trip, long enough to press g@ and then move on.
// An answer about a chat the reader has left is dropped where it lands: a
// jump back into it would undo the move they made second, and a notice
// would describe a chat they are not looking at.
func TestAListingForAChatSinceLeftIsIgnored(t *testing.T) {
	m := openQuietMentionChat(t)
	m.OpenChat(testChatID+1, "elsewhere")

	m, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5, 9}})
	if cmd != nil {
		t.Error("a listing for the chat that was left produced a command")
	}
	if m.notice != "" {
		t.Errorf("a listing for the chat that was left put up %q", m.notice)
	}
	if m.askedMentions.has(testChatID, 5) {
		t.Error("a listing for the chat that was left recorded an ask it never made")
	}
}

// A listing that failed says so, in the voice the panel's other failures
// use; the @ stays where it is.
func TestAFailedListingSaysSo(t *testing.T) {
	m := openQuietMentionChat(t)
	setMentionCount(t, m, 2)

	m, cmd := m.Update(mentionsListedMsg{chatID: testChatID, err: errors.New("no connection")})
	if m.notice != "could not load mentions" {
		t.Errorf("notice = %q, want %q", m.notice, "could not load mentions")
	}
	if cmd != nil {
		t.Error("a failed listing produced a command; the count must not be corrected on no evidence")
	}
}

// A mention on screen at open waits out the window, and g@ inside it can
// land on the same one and clear it at once. The window's clear must not
// then ask for it a second time.
func TestAMentionJumpedToInsideTheWindowIsNotAskedForAgain(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4))

	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{4}})

	if _, cmd := m.Update(mentionsFlushMsg{chatID: testChatID}); cmd != nil {
		t.Fatal("the window asked again for the mention g@ had already cleared")
	}
}

// Without a client there is nobody to send the clear to, and the jump
// still goes: it is the host's to make. What must not come back is a
// command that dereferences the missing client.
func TestAListingWithNoClientJumpsWithoutAClear(t *testing.T) {
	m := openQuietMentionChat(t)
	m.tg = nil

	_, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5}})
	if cmd == nil {
		t.Fatal("no jump without a client")
	}
	if reachesClient(cmd) {
		t.Fatal("the listing handed back a clear with no client to make it")
	}
	if jump, ok := cmd().(MentionJumpMsg); !ok || jump.MessageId != 5 {
		t.Errorf("got %#v, want the jump to 5 on its own", jump)
	}
}
