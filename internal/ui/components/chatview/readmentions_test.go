package chatview

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// A message that names the reader stays an @ on the chat's row until it
// has been seen, and the official clients count it seen when it is on
// screen. These tests read the clear off the state that owes it and the
// command its flush returns, as readreactions_test.go does: the chat view
// holds a concrete client, and no test here runs the command.
//
// The fixture chat is read to its newest message, so a first page owes no
// read receipt, and it has no unread reactions, so the mentions' window is
// the only command a first page can produce.

// withMentions marks the given messages on a page as unread mentions of
// the reader, the flag the server sets on each one.
func withMentions(page historyLoadedMsg, ids ...int64) historyLoadedMsg {
	for _, msg := range page.messages {
		if slices.Contains(ids, msg.ID) {
			msg.UnreadMention = true
		}
	}
	return page
}

// owedMentionIDs is the owed set in ID order, so a test can compare it
// without caring which order the page listed them in.
func owedMentionIDs(m Model) []int64 {
	ids := slices.Clone(m.pendingMentionsRead)
	slices.Sort(ids)
	return ids
}

// Focused and opened at the newest messages, the mentions on the first
// page have been seen. They wait out the window, as the read receipt does,
// and then go in one request.
func TestOpeningAChatAtItsNewestClearsTheMentionsOnScreen(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")

	m, cmd := m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 2))
	if cmd == nil || !m.mentionsFlushPending {
		t.Fatalf("after the first page: cmd=%v, window scheduled=%v; want the window scheduled",
			cmd != nil, m.mentionsFlushPending)
	}
	if got := owedMentionIDs(m); !slices.Equal(got, []int64{2, 4}) {
		t.Fatalf("owed mentions %v after the first page, want [2 4]", got)
	}

	m, cmd = m.Update(mentionsFlushMsg{chatID: testChatID})
	if cmd == nil {
		t.Fatal("the window closed on a reader still in the chat and sent no clear")
	}
	if got := owedMentionIDs(m); len(got) != 0 {
		t.Fatalf("owed mentions %v after the flush, want them consumed", got)
	}
	if _, cmd = m.Update(mentionsFlushMsg{chatID: testChatID}); cmd != nil {
		t.Fatal("a second flush sent a second clear for the same mentions")
	}
}

// A voice note or a round video that names the reader has not been heard
// by being on screen. TDLib leaves those mentions for playing to clear, and
// so does this client: the rest of the page is still cleared.
func TestAVoiceOrVideoNoteMentionIsNotClearedByBeingOnScreen(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")

	page := withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 3, 2)
	for _, msg := range page.messages {
		switch msg.ID {
		case 4:
			msg.Content = &telegram.MessageVoiceNote{VoiceNote: &telegram.VoiceNote{Duration: 3}}
		case 3:
			msg.Content = &telegram.MessageVideoNote{VideoNote: &telegram.VideoNote{Duration: 3}}
		}
	}

	m, _ = m.Update(page)
	if got := owedMentionIDs(m); !slices.Equal(got, []int64{2}) {
		t.Fatalf("owed mentions %v, want only the text message 2", got)
	}
}

// A search hit, a link or a reply jump opens the chat at an older message,
// and the newest ones are below the fold. The mentions among them have not
// been seen, and the first page is not the screen.
func TestOpeningAChatAtAnOlderMessageClearsNoMentions(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChatAt(testChatID, "nadia", 2)

	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 2))
	if got := owedMentionIDs(m); len(got) != 0 || m.mentionsFlushPending {
		t.Fatalf("opening at message 2 owes %v (window scheduled=%v), want nothing",
			got, m.mentionsFlushPending)
	}
}

// Only the first page is the open. An older page fetched by scrolling back
// scrolls in from the top, one line at a time, and nothing says which of
// its mentions the reader stopped on.
func TestPagingBackwardsClearsNoMentions(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(historyPage(m, 0, 10, 9, 8, 7, 6))

	m.loading = true
	m, _ = m.Update(withMentions(historyPage(m, 6, 5, 4, 3, 2, 1), 4, 2))
	if got := owedMentionIDs(m); len(got) != 0 || m.mentionsFlushPending {
		t.Fatalf("an older page owes %v (window scheduled=%v), want nothing",
			got, m.mentionsFlushPending)
	}
}

// A chat opened while the terminal is in the background has not been
// looked at. The mentions stay owed until focus returns, and then go at
// once, as the read receipt does.
func TestOpeningAChatWhileBlurredClearsItsMentionsOnFocus(t *testing.T) {
	m := unreadChat(5, 0)
	m, _ = m.Update(tea.BlurMsg{})
	m.OpenChat(testChatID, "nadia")

	m, cmd := m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 2))
	if cmd != nil {
		t.Fatal("the first page scheduled something while blurred")
	}
	if got := owedMentionIDs(m); !slices.Equal(got, []int64{2, 4}) {
		t.Fatalf("owed mentions %v while blurred, want [2 4] held for focus", got)
	}

	m, cmd = m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("regaining focus sent no clear for the mentions seen while blurred")
	}
	if got := owedMentionIDs(m); len(got) != 0 {
		t.Fatalf("owed mentions %v after focus, want them consumed", got)
	}
	if _, cmd = m.Update(tea.FocusMsg{}); cmd != nil {
		t.Fatal("focus sent the clear a second time")
	}
}

// J and K open every chat they pass through, and a chat left inside the
// window was not read: its mentions stay unread, and they must not follow
// the reader to the next chat either. The left chat's tick finds nothing
// to send, and neither does focus.
func TestPassingThroughAChatLeavesItsMentionsAlone(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 2))
	if len(m.pendingMentionsRead) == 0 {
		t.Fatal("precondition: the first page owed no mentions")
	}

	m.OpenChat(testChatID+1, "elsewhere")

	if got := owedMentionIDs(m); len(got) != 0 {
		t.Fatalf("owed mentions %v after the switch, want the left chat's dropped", got)
	}
	m, cmd := m.Update(mentionsFlushMsg{chatID: testChatID})
	if cmd != nil {
		t.Fatal("the passed-through chat's tick sent its clear after the reader left")
	}
	if _, cmd = m.Update(tea.FocusMsg{}); cmd != nil {
		t.Fatal("the passed-through chat's mentions were still owed")
	}
}

// Without a client there is nobody to send the clear to. The owed mentions
// are consumed all the same, as the read flush consumes its receipt; what
// the flush must not do is hand back a command that dereferences the
// missing client.
func TestAMentionsClearWithNoClientIsConsumedWithoutARequest(t *testing.T) {
	m := unreadChat(5, 0)
	m.tg = nil
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4))

	m, cmd := m.Update(mentionsFlushMsg{chatID: testChatID})
	if cmd != nil {
		t.Fatal("the flush handed back a request with no client to make it")
	}
	if got := owedMentionIDs(m); len(got) != 0 {
		t.Fatalf("owed mentions %v, want them consumed", got)
	}
}

// openQuietMentionChat is a chat open, loaded and owing nothing: no
// receipt, no reactions, no mentions on its first page.
func openQuietMentionChat(t *testing.T) Model {
	t.Helper()
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, cmd := m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if cmd != nil {
		t.Fatal("the fixture's open owed something already")
	}
	return m
}

// arrivingMention is message id arriving in the open chat, naming the
// reader.
func arrivingMention(id int64) telegram.NewMessageMsg {
	msg := textMessage(id, 200, "@reader look")
	msg.UnreadMention = true
	return telegram.NewMessageMsg{Message: msg}
}

// A mention that arrives in the chat the reader is looking at has been
// seen, the way the message itself has been read. The @ on the phone
// should not outlast the time spent in the chat here.
func TestAMentionArrivingInTheOpenChatIsCleared(t *testing.T) {
	m := openQuietMentionChat(t)

	m, _ = m.Update(arrivingMention(6))
	if got := owedMentionIDs(m); !slices.Equal(got, []int64{6}) || !m.mentionsFlushPending {
		t.Fatalf("after the arrival: owed %v, window scheduled=%v; want [6] and the window",
			got, m.mentionsFlushPending)
	}
	if _, cmd := m.Update(mentionsFlushMsg{chatID: testChatID}); cmd == nil {
		t.Fatal("the flush sent no clear for the mention that arrived")
	}
}

// A voice note that names the reader has arrived, not been heard.
func TestAVoiceNoteMentionArrivingIsNotCleared(t *testing.T) {
	m := openQuietMentionChat(t)
	arrival := arrivingMention(6)
	arrival.Message.Content = &telegram.MessageVoiceNote{VoiceNote: &telegram.VoiceNote{Duration: 3}}

	m, _ = m.Update(arrival)
	if got := owedMentionIDs(m); len(got) != 0 {
		t.Fatalf("owed %v after a voice note arrived, want nothing", got)
	}
}

// A mention in another chat has not been seen. The chat list counts it,
// and that chat's own open is what clears it.
func TestAMentionInAnotherChatIsNotCleared(t *testing.T) {
	m := openQuietMentionChat(t)
	arrival := arrivingMention(6)
	arrival.Message.ChatID = testChatID + 1

	m, _ = m.Update(arrival)
	if got := owedMentionIDs(m); len(got) != 0 {
		t.Fatalf("owed %v after a mention in another chat, want nothing", got)
	}
}

// A clear the server has not answered yet leaves the flags on the page, and
// a reopen of the same chat — g@ reopens it to jump, and so does ctrl+o —
// brings the same page back. What this client has asked for once it does
// not ask for again: a second request would clear nothing and cost a pts
// step.
func TestAMentionAskedToClearIsNeverOwedAgain(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 2))
	m, _ = m.Update(mentionsFlushMsg{chatID: testChatID})

	m.OpenChat(testChatID, "nadia")
	m, cmd := m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 2))
	if got := owedMentionIDs(m); len(got) != 0 || cmd != nil {
		t.Fatalf("the reopen owes %v (cmd=%v), want nothing: both were asked already",
			got, cmd != nil)
	}

	m, _ = m.Update(arrivingMention(4))
	if got := owedMentionIDs(m); len(got) != 0 {
		t.Fatalf("a replayed arrival owes %v, want nothing", got)
	}
}

// Message IDs outside a channel are the account's own numbering, but a
// channel numbers its messages itself, so the same ID in two chats is two
// messages. Asking about one says nothing about the other.
func TestAMentionAskedInOneChatIsStillOwedInAnother(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4))
	m, _ = m.Update(mentionsFlushMsg{chatID: testChatID})

	const other = testChatID + 1
	m.OpenChat(other, "elsewhere")
	page := withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4)
	page.chatID = other
	m, _ = m.Update(page)
	if got := owedMentionIDs(m); !slices.Equal(got, []int64{4}) {
		t.Fatalf("the other chat owes %v, want its own message 4", got)
	}
}

// The ledger lives as long as the session, so it is bounded. It forgets
// the chat it heard about least recently: the reason it outlives a switch
// is a jump back into the chat, and a chat the reader has not been near in
// a while is not where they are jumping.
func TestTheMentionLedgerForgetsTheChatAskedAboutLeastRecently(t *testing.T) {
	var l mentionLedger
	for chat := int64(1); chat <= maxAskedChats; chat++ {
		l.record(chat, 7)
	}
	// Chat 1 is asked about again, which leaves chat 2 the stalest.
	l.record(1, 8)
	l.record(maxAskedChats+1, 7)

	if l.has(2, 7) {
		t.Error("the stalest chat is still remembered past the cap")
	}
	for _, chat := range []int64{1, 3, maxAskedChats + 1} {
		if !l.has(chat, 7) {
			t.Errorf("chat %d was forgotten, want only the stalest one gone", chat)
		}
	}
}

// Within a chat it forgets the oldest ask first. A clear asked for that
// long ago has been answered one way or the other.
func TestTheMentionLedgerForgetsAChatsOldestAsks(t *testing.T) {
	var l mentionLedger
	for id := int64(1); id <= maxAskedPerChat+1; id++ {
		l.record(5, id)
	}

	if l.has(5, 1) {
		t.Error("the oldest ask is still remembered past the cap")
	}
	if !l.has(5, 2) || !l.has(5, maxAskedPerChat+1) {
		t.Error("the ledger forgot more than the oldest ask")
	}
}

// :read-mentions clears every mention in the chat at once, asked for
// explicitly, so it does not wait for focus or a window.
func TestReadAllMentionsAsksTheClient(t *testing.T) {
	m := openQuietMentionChat(t)

	cmd := m.ReadAllMentionsCmd()
	if cmd == nil {
		t.Fatal("no command with a chat open")
	}
	if !reachesClient(cmd) {
		t.Fatal("the command did not ask the client to clear the mentions")
	}
}

// Nothing open is nothing to clear, and no client is nobody to ask. Nil
// either way, so the caller can say so rather than hand back a command
// that dereferences nothing.
func TestReadAllMentionsNeedsAChatAndAClient(t *testing.T) {
	m := unreadChat(5, 0)
	if m.ReadAllMentionsCmd() != nil {
		t.Error("a command with no chat open")
	}

	m = openQuietMentionChat(t)
	m.tg = nil
	if m.ReadAllMentionsCmd() != nil {
		t.Error("a command with no client")
	}
}

// Clearing every mention makes the ones the window still owes redundant:
// the window closing afterwards must not ask for them again.
func TestReadAllMentionsDropsTheOwedClears(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4))

	m.ReadAllMentionsCmd()

	if _, cmd := m.Update(mentionsFlushMsg{chatID: testChatID}); cmd != nil {
		t.Fatal("the window asked for a mention the full clear already covered")
	}
}

// A clear the server refused did not happen. Left in the ledger, the
// mention would never be asked for again, and g@ would call a chat with an
// @ on the server empty and zero the count. The failure takes the IDs back
// out, whichever chat is open when it lands, and the next open owes them
// again.
func TestAFailedClearIsForgotten(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 2))
	m, _ = m.Update(mentionsFlushMsg{chatID: testChatID})
	m.OpenChat(testChatID+1, "elsewhere")

	m, _ = m.Update(mentionsClearFailedMsg{chatID: testChatID, ids: []int64{2, 4}})
	if m.askedMentions.has(testChatID, 2) || m.askedMentions.has(testChatID, 4) {
		t.Fatal("the failed clear is still recorded as asked")
	}

	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(withMentions(historyPage(m, 0, 5, 4, 3, 2, 1), 4, 2))
	if got := owedMentionIDs(m); !slices.Equal(got, []int64{2, 4}) {
		t.Fatalf("the next open owes %v, want the mentions whose clear failed, [2 4]", got)
	}
}
