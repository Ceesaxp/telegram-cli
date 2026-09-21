package chatview

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// Opening a chat also clears its unread reactions: the heart the phone
// shows when somebody reacted to the reader's message. Reactions sit on
// the reader's own messages anywhere in the history, not at the bottom,
// so this is about opening the chat rather than about what is on screen.
//
// The chat view holds a concrete client, so these tests read the request
// off the command the first page returns. The chats here are read to
// their newest message, which makes the clear the only command a first
// page can produce: none of them owes a read receipt or meta work.

// reactedChat is a chat read to its newest message, with reactions
// unread on the reader's messages.
func reactedChat(reactions int32) Model {
	m := unreadChat(5, 0)
	entry, _ := m.store.Chats.Get(testChatID)
	entry.UnreadReactionsCount = reactions
	return m
}

// Focused, the clear goes with the first page. There is no window to
// coalesce in: a chat is opened once.
func TestOpeningAChatClearsItsUnreadReactions(t *testing.T) {
	m := reactedChat(2)
	m.OpenChat(testChatID, "nadia")

	_, cmd := m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if cmd == nil {
		t.Fatal("the first page of a chat with unread reactions sent no clear")
	}
}

// Most chats have no unread reactions, and opening one costs no request.
// This is also the control for the test above: the same open with nothing
// to clear produces no command at all.
func TestOpeningAChatWithoutUnreadReactionsSendsNoClear(t *testing.T) {
	m := reactedChat(0)
	m.OpenChat(testChatID, "nadia")

	if _, cmd := m.Update(historyPage(m, 0, 5, 4, 3, 2, 1)); cmd != nil {
		t.Fatal("the first page of a chat without unread reactions produced a command")
	}
}

// A search hit, a link or a reply jump opens the chat at an older message,
// and that is still opening the chat. Unlike the read receipt, the clear
// does not wait for the newest messages to be on screen: the reactions are
// anywhere in the history, and opening the chat is what clears them.
func TestOpeningAChatAtAnOlderMessageStillClearsItsReactions(t *testing.T) {
	m := reactedChat(2)
	m.OpenChatAt(testChatID, "nadia", 2)

	_, cmd := m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if cmd == nil {
		t.Fatal("opening at message 2 sent no clear")
	}
}

// When the target is not on the first page, the first page goes straight
// on to fetch the next one back, and the clear has to go with that fetch
// rather than wait for a first paint the hunt returns early from.
func TestHuntingForAnOlderMessageStillClearsTheReactions(t *testing.T) {
	m := reactedChat(2)
	m.OpenChatAt(testChatID, "nadia", 2)

	m, cmd := m.Update(historyPage(m, 0, 10, 9, 8, 7, 6))
	if m.loadStatus != "Searching for message..." {
		t.Fatalf("the first page did not start the hunt: status %q", m.loadStatus)
	}
	// Two commands: the next page back, and the clear.
	if n := len(runBatch(t, cmd)); n != 2 {
		t.Fatalf("the hunt's first page returned %d commands, want the fetch and the clear", n)
	}
}

// runBatch runs a command that should be a batch and returns what it
// holds, without running any of that. A lone command here would be the
// history fetch itself, which this test's client cannot serve: running it
// reaches for a connection that is not there, and that is reported as the
// failure it is rather than as a crash.
func runBatch(t *testing.T, cmd tea.Cmd) tea.BatchMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command at all, want a batch")
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the command was not a batch: running it reached the client (%v)", r)
		}
	}()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("the command was not a batch")
	}
	return batch
}

// A chat opened while the terminal is in the background has not been
// looked at. The clear waits for focus, as the read receipt does.
func TestOpeningAChatWhileBlurredClearsItsReactionsOnFocus(t *testing.T) {
	m := reactedChat(2)
	m, _ = m.Update(tea.BlurMsg{})
	m.OpenChat(testChatID, "nadia")

	m, cmd := m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if cmd != nil {
		t.Fatal("the clear was sent while blurred")
	}

	m, cmd = m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("regaining focus sent no clear for the chat opened while blurred")
	}
	if _, cmd = m.Update(tea.FocusMsg{}); cmd != nil {
		t.Fatal("focus sent the clear a second time")
	}
}

// A reaction in a chat that is not open has not been seen. The chat list
// keeps its count, and opening that chat is what clears it.
func TestAReactionInAnotherChatClearsNothing(t *testing.T) {
	m := openQuietChat(t)

	m, cmd := m.Update(telegram.ChatUnreadReactionsMsg{ChatId: testChatID + 1})
	if cmd != nil {
		t.Fatal("a reaction in another chat scheduled something")
	}
	if _, cmd = m.Update(tea.FocusMsg{}); cmd != nil {
		t.Fatal("a reaction in another chat left a clear owed to this one")
	}
}

// A window still open when the reader moves on belongs to the chat they
// left. Its tick must not flush the next chat's clear early, and it must
// not keep the next chat from opening a window of its own.
func TestAClearsWindowDoesNotOutliveItsChat(t *testing.T) {
	m := openQuietChat(t)
	m, _ = m.Update(telegram.ChatUnreadReactionsMsg{ChatId: testChatID})

	const next = testChatID + 1
	m.OpenChat(next, "elsewhere")
	m, cmd := m.Update(telegram.ChatUnreadReactionsMsg{ChatId: next})
	if cmd == nil {
		t.Fatal("the next chat's reaction scheduled no clear: the left chat's window is still holding the flag")
	}

	m, cmd = m.Update(reactionsFlushMsg{chatID: testChatID})
	if cmd != nil {
		t.Fatal("the left chat's tick flushed the next chat's clear")
	}
	if _, cmd = m.Update(reactionsFlushMsg{chatID: next}); cmd == nil {
		t.Fatal("the next chat's own tick sent no clear")
	}
}

// The owed clear belongs to the chat that was opened. Moving on before
// focus returns means that chat was never looked at, and the clear must not
// follow the reader to the next one either.
func TestOpeningAnotherChatDropsTheOwedClear(t *testing.T) {
	m := reactedChat(2)
	m, _ = m.Update(tea.BlurMsg{})
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))

	m.OpenChat(testChatID+1, "elsewhere")

	if _, cmd := m.Update(tea.FocusMsg{}); cmd != nil {
		t.Fatal("focus sent a clear owed to the chat that was left")
	}
}

// The open has already cleared the chat. Scrolling back is not another
// open, even before the chat list has heard back and zeroed the count.
func TestPagingBackwardsSendsNoClear(t *testing.T) {
	m := reactedChat(2)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(historyPage(m, 0, 10, 9, 8, 7, 6))

	m.loading = true
	if _, cmd := m.Update(historyPage(m, 6, 5, 4, 3, 2, 1)); cmd != nil {
		t.Fatal("an older page sent the clear again")
	}
}

// openQuietChat is a chat that is open, loaded and owes nothing: read to
// its newest message, and with no unread reactions as far as the store
// knows. A reaction that arrives now is the only evidence there is one,
// which is the point: the chat list raises the count from the same
// message, and which panel sees it first is not something to lean on.
func openQuietChat(t *testing.T) Model {
	t.Helper()
	m := reactedChat(0)
	m.OpenChat(testChatID, "nadia")
	m, cmd := m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if cmd != nil {
		t.Fatal("the fixture's open owed something already")
	}
	return m
}

// A reaction to the reader's message in the chat they are reading has
// been seen, the way an arriving message has. The heart on the phone
// should not outlast the time the reader spends in the chat here.
func TestAReactionInTheOpenChatIsClearedAfterTheWindow(t *testing.T) {
	m := openQuietChat(t)

	m, cmd := m.Update(telegram.ChatUnreadReactionsMsg{ChatId: testChatID})
	if cmd == nil {
		t.Fatal("a reaction in the open chat scheduled no clear")
	}
	// The command is the window: it waits it out and comes back as the
	// flush for this chat.
	flush := cmd()
	if flush != (reactionsFlushMsg{chatID: testChatID}) {
		t.Fatalf("the reaction's command produced %#v, want the flush for this chat", flush)
	}

	m, cmd = m.Update(flush)
	if cmd == nil {
		t.Fatal("the flush sent no clear")
	}
	if _, cmd = m.Update(flush); cmd != nil {
		t.Fatal("a second flush sent a second clear for one reaction")
	}
}

// A popular message collects reactions in bursts, and the clear covers the
// whole chat, so a burst inside the window costs one request, not one
// per reaction.
func TestABurstOfReactionsIsOneClear(t *testing.T) {
	m := openQuietChat(t)

	var scheduled int
	for range 3 {
		var cmd tea.Cmd
		m, cmd = m.Update(telegram.ChatUnreadReactionsMsg{ChatId: testChatID})
		if cmd != nil {
			scheduled++
		}
	}
	if scheduled != 1 {
		t.Fatalf("%d flushes scheduled for a burst of three, want 1", scheduled)
	}

	m, cmd := m.Update(reactionsFlushMsg{chatID: testChatID})
	if cmd == nil {
		t.Fatal("the flush sent no clear")
	}
	if _, cmd = m.Update(reactionsFlushMsg{chatID: testChatID}); cmd != nil {
		t.Fatal("the burst cost a second clear")
	}
}

// With the terminal in the background the reader has not seen the
// reaction, however long the chat sits open. The clear waits for focus.
func TestAReactionWhileBlurredIsClearedOnFocus(t *testing.T) {
	m := openQuietChat(t)
	m, _ = m.Update(tea.BlurMsg{})

	m, cmd := m.Update(telegram.ChatUnreadReactionsMsg{ChatId: testChatID})
	if cmd != nil {
		t.Fatal("a reaction while blurred scheduled something")
	}

	m, cmd = m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("regaining focus sent no clear for the reaction that came in meanwhile")
	}
	if _, cmd = m.Update(tea.FocusMsg{}); cmd != nil {
		t.Fatal("focus sent the clear a second time")
	}
}
