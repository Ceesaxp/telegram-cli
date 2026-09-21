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

// reachesClientAnywhere is reachesClient for a command that may be a
// batch: it runs every command the batch holds, and reports whether any of
// them went to the client.
func reachesClientAnywhere(cmd tea.Cmd) (reached bool) {
	if cmd == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			reached = true
		}
	}()
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if reachesClientAnywhere(c) {
				return true
			}
		}
	}
	return false
}

// jumpOf reads the jump a listing hands the host. It is the listing's
// only command: nothing is cleared before the reader has landed.
func jumpOf(t *testing.T, cmd tea.Cmd) MentionJumpMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("the listing handed the host no jump")
	}
	if reachesClient(cmd) {
		t.Fatal("the listing went to the client before the reader landed anywhere")
	}
	jump, ok := cmd().(MentionJumpMsg)
	if !ok {
		t.Fatalf("the listing handed the host %T, want a MentionJumpMsg", cmd())
	}
	return jump
}

// landOn is the host's half of a mention jump: it reopens the chat at the
// mention, and the first page comes back holding the given messages.
func landOn(m Model, target int64, page ...int64) (Model, tea.Cmd) {
	m.OpenChatAt(testChatID, "nadia", target)
	return m.Update(historyPage(m, 0, page...))
}

// The oldest unread mention is where g@ goes, and the host is handed the
// jump: the panel reads, the host navigates. Nothing is cleared yet: the
// jump may not reach the message, and a mention cleared unseen is gone
// from the listing for good.
func TestAListingJumpsToTheOldestMention(t *testing.T) {
	m := openQuietMentionChat(t)

	m, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5, 9}})
	jump := jumpOf(t, cmd)
	if jump.ChatId != testChatID || jump.MessageId != 5 {
		t.Errorf("jumped to chat %d message %d, want chat %d message 5",
			jump.ChatId, jump.MessageId, testChatID)
	}
	if jump.Remaining < 1 {
		t.Errorf("Remaining = %d with 9 still unread, want at least 1", jump.Remaining)
	}
	if m.askedMentions.has(testChatID, 5) {
		t.Error("the mention was recorded as asked before the reader landed on it")
	}
}

// Landing on the mention is the reader looking at it, so it is cleared
// then, at once rather than after a window, and voice notes too: the
// reader chose to go there. Once: coming back to it later is not a second
// arrival.
func TestLandingOnTheMentionClearsItOnce(t *testing.T) {
	m := openQuietMentionChat(t)
	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5, 9}})

	m, cmd := landOn(m, 5, 5, 4, 3, 2, 1)
	if !reachesClientAnywhere(cmd) {
		t.Fatal("landing on the mention sent no clear")
	}
	if !m.askedMentions.has(testChatID, 5) {
		t.Error("the mention landed on was not recorded as asked")
	}

	if _, cmd = landOn(m, 5, 5, 4, 3, 2, 1); reachesClientAnywhere(cmd) {
		t.Error("coming back to the mention cleared it a second time")
	}
}

// huntFromHigh is a chat read to message 100 and open at its newest
// page, 100 to 96, so that a mention far below that is further back than
// a jump's hunt walks.
func huntFromHigh(t *testing.T) Model {
	t.Helper()
	m := unreadChat(100, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(historyPage(m, 0, 100, 99, 98, 97, 96))
	return m
}

// huntUntilSettled plays the server for a jump's hunt backwards: each
// page it asks for is the five messages below the oldest loaded, until the
// panel stops asking. It returns what the last page produced.
func huntUntilSettled(t *testing.T, m Model, target int64) (Model, tea.Cmd) {
	t.Helper()
	m.OpenChatAt(testChatID, "nadia", target)
	m, cmd := m.Update(historyPage(m, 0, 100, 99, 98, 97, 96))
	for pages := 0; m.targetMsgID != 0; pages++ {
		if pages > maxTargetPages+1 {
			t.Fatalf("the hunt for %d did not stop after %d pages", target, pages)
		}
		oldest := m.store.Messages.OldestMessageId(testChatID)
		m, cmd = m.Update(historyPage(m, oldest, oldest-1, oldest-2, oldest-3, oldest-4, oldest-5))
	}
	return m, cmd
}

// The jump's hunt pages back a few pages, not the whole history, and a
// mention further back than that cannot be shown. It is not cleared, since
// nobody saw it, and the notice says why the reader is not looking at it
// rather than the generic miss.
func TestAMentionTooFarBackIsLeftUnread(t *testing.T) {
	m := huntFromHigh(t)
	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{3, 97}})

	m, cmd := huntUntilSettled(t, m, 3)
	if reachesClientAnywhere(cmd) {
		t.Error("a mention the hunt never reached was cleared")
	}
	if m.askedMentions.has(testChatID, 3) {
		t.Error("a mention the hunt never reached was recorded as asked")
	}
	if want := "that mention is further back than this chat loads"; m.notice != want {
		t.Errorf("notice = %q, want %q", m.notice, want)
	}
}

// The next g@ does not walk into the same wall: the mention out of reach
// is skipped for the session, and the reader goes on to the one after.
func TestTheNextGAtSkipsAMentionOutOfReach(t *testing.T) {
	m := huntFromHigh(t)
	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{3, 97}})
	m, _ = huntUntilSettled(t, m, 3)

	_, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{3, 97}})
	if jump := jumpOf(t, cmd); jump.MessageId != 97 {
		t.Errorf("the next g@ jumped to %d, want 97, past the one out of reach", jump.MessageId)
	}
}

// g@ again goes to the one after. The server may not have cleared the
// first yet when the second listing is taken, and the listing then still
// starts with it; the ledger is what moves the reader on rather than back
// to where they are.
func TestAgainGoesToTheMentionAfter(t *testing.T) {
	m := openQuietMentionChat(t)
	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5, 9}})
	m, _ = landOn(m, 5, 5, 4, 3, 2, 1)

	_, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5, 9}})
	if jump := jumpOf(t, cmd); jump.MessageId != 9 {
		t.Errorf("the second g@ jumped to %d, want 9", jump.MessageId)
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

// fullPage is a listing as long as g@ asks for, the oldest mentions from
// first upwards.
func fullPage(first int64) []int64 {
	ids := make([]int64, mentionListLimit)
	for i := range ids {
		ids[i] = first + int64(i)
	}
	return ids
}

// How many are left after this one. A listing shorter than g@ asked for
// is every unread mention there is, so it is the answer whatever the
// store counts. Only a full one may stop short of the rest, and then the
// store's count, which knows of the others, is the better guess if it is
// the larger. None is never less than none.
func TestRemainingTrustsAShortListingAndTheCountOnlyPastAFullOne(t *testing.T) {
	for name, tc := range map[string]struct {
		ids   []int64
		count int32
		want  int
	}{
		"a full page, and the count knows of more":        {fullPage(5), 25, 24},
		"a full page, and the count behind":               {fullPage(5), 3, mentionListLimit - 1},
		"a short page is all of them, whatever the count": {[]int64{5, 9}, 25, 1},
		"a short page knows of more than a count behind":  {[]int64{5, 9, 12}, 1, 2},
		"the last one": {[]int64{5}, 1, 0},
		"the last one, with the count already behind": {[]int64{5}, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			m := openQuietMentionChat(t)
			setMentionCount(t, m, tc.count)

			_, cmd := m.Update(mentionsListedMsg{chatID: testChatID, ids: tc.ids})
			if jump := jumpOf(t, cmd); jump.Remaining != tc.want {
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
	m, _ = landOn(m, 5, 5, 4, 3, 2, 1)

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
// go to the same one. Whichever asks first, the other must not ask again:
// here the window closes before the reader lands.
func TestAMentionTheWindowClearedIsNotClearedAgainOnLanding(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4))
	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{4}})

	m, _ = m.Update(mentionsFlushMsg{chatID: testChatID})

	if _, cmd := landOn(m, 4, 5, 4, 3, 2, 1); reachesClientAnywhere(cmd) {
		t.Fatal("landing asked again for the mention the window had already cleared")
	}
}

// Without a client there is nobody to send the clear to. Landing must not
// hand back a command that dereferences the missing client.
func TestLandingWithNoClientSendsNoClear(t *testing.T) {
	m := openQuietMentionChat(t)
	m.tg = nil
	m, _ = m.Update(mentionsListedMsg{chatID: testChatID, ids: []int64{5}})

	if _, cmd := landOn(m, 5, 5, 4, 3, 2, 1); cmd != nil {
		t.Fatal("landing handed back a command with no client to make it")
	}
}
