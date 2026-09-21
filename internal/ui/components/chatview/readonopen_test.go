package chatview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// Opening a chat is a deliberate act in this client — the chat list's j/k
// move a cursor and open nothing — so a chat opened at its newest messages
// has been read, and the phone should stop calling it unread. These tests
// observe the receipt the way coalesce_test.go does: pendingReadID is the
// ID readHistory will be called with, readFlushPending says a flush is
// scheduled, and a flush that finds a client hands the receipt over.

// unreadChat is a model with a private chat in the store described the way
// its dialog describes it: read up to readUpTo, with unread still counted.
// The sender is known, so a page of text from them owes no meta work and
// the only command a first page can produce is the receipt's.
func unreadChat(readUpTo int64, unread int32) Model {
	m := newTestModel()
	m.chatID = 0 // nothing open until the test opens it
	// A flush needs a client to hand the receipt to. No test runs the
	// command, so no request is made.
	m.tg = &telegram.Client{}
	m.myUserId = 100
	m.store.Users.Set(&telegram.User{ID: 200, FirstName: "nadia"})
	m.store.Chats.Set(&telegram.Chat{
		ID:                     testChatID,
		Type:                   telegram.ChatTypePrivate,
		Title:                  "nadia",
		LastReadInboxMessageID: readUpTo,
		UnreadCount:            unread,
	})
	return m
}

// historyPage is a history page as the server returns it, newest first.
func historyPage(m Model, fromID int64, ids ...int64) historyLoadedMsg {
	msgs := make([]*telegram.Message, 0, len(ids))
	for _, id := range ids {
		msgs = append(msgs, textMessage(id, 200, fmt.Sprintf("message %d", id)))
	}
	return historyLoadedMsg{gen: m.gen, chatID: testChatID, fromID: fromID, messages: msgs}
}

// Opened at the newest messages, focused: one receipt, for the newest
// message on the page, once the coalescing window closes.
func TestOpeningAChatMarksItReadUpToTheNewestMessage(t *testing.T) {
	m := unreadChat(3, 2)
	m.OpenChat(testChatID, "nadia")

	m, cmd := m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if cmd == nil {
		t.Fatal("the first page of a chat with unread messages scheduled no read receipt")
	}
	if !m.readFlushPending || m.pendingReadID != 5 {
		t.Fatalf("after the first page: flush pending=%v, pendingReadID=%d; want a flush for 5",
			m.readFlushPending, m.pendingReadID)
	}

	m, cmd = m.Update(readFlushMsg{chatID: testChatID})
	if cmd == nil {
		t.Fatal("the flush handed no receipt to the client")
	}
	if m.pendingReadID != 0 {
		t.Fatalf("pendingReadID = %d after the flush, want it consumed", m.pendingReadID)
	}
	if _, cmd = m.Update(readFlushMsg{chatID: testChatID}); cmd != nil {
		t.Fatal("a second flush sent a second receipt for the same open")
	}
}

// Opened while the terminal is in the background, nothing has been read
// yet: the receipt waits for focus, exactly as an arrival's does.
func TestOpeningAChatWhileBlurredMarksItReadOnFocus(t *testing.T) {
	m := unreadChat(3, 2)
	m, _ = m.Update(tea.BlurMsg{})
	m.OpenChat(testChatID, "nadia")

	m, cmd := m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if cmd != nil || m.readFlushPending {
		t.Fatalf("a receipt was scheduled while blurred: cmd=%v, flush pending=%v", cmd != nil, m.readFlushPending)
	}
	if m.pendingReadID != 5 {
		t.Fatalf("pendingReadID = %d while blurred, want 5 held for focus", m.pendingReadID)
	}

	m, cmd = m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("regaining focus sent no receipt for the chat opened while blurred")
	}
	if m.pendingReadID != 0 {
		t.Fatalf("pendingReadID = %d after focus, want it consumed", m.pendingReadID)
	}
}

// A search hit, a t.me link or a reply jump opens the chat at an older
// message. The newest ones are below the fold and the reader has not seen
// them, so nothing is marked read.
func TestOpeningAChatAtAnOlderMessageMarksNothingRead(t *testing.T) {
	m := unreadChat(3, 2)
	m.OpenChatAt(testChatID, "nadia", 2)

	m, _ = m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if m.readFlushPending || m.pendingReadID != 0 {
		t.Fatalf("opening at message 2 scheduled a receipt: flush pending=%v, pendingReadID=%d",
			m.readFlushPending, m.pendingReadID)
	}
}

// Only the first page is the open. An older page is the reader scrolling
// back through history they have already been marked as having read.
func TestPagingBackwardsMarksNothingRead(t *testing.T) {
	m := unreadChat(3, 2)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(historyPage(m, 0, 10, 9, 8, 7, 6))
	m, _ = m.Update(readFlushMsg{chatID: testChatID})

	m.loading = true
	m, _ = m.Update(historyPage(m, 6, 5, 4, 3, 2, 1))
	if m.readFlushPending || m.pendingReadID != 0 {
		t.Fatalf("an older page scheduled a receipt: flush pending=%v, pendingReadID=%d",
			m.readFlushPending, m.pendingReadID)
	}
}

// A chat already read to its newest message costs nothing to open. Every
// receipt is an RPC and a pts step, and here every pts step also triggers
// a getDifference.
func TestOpeningAReadChatSendsNoReceipt(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")

	m, _ = m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if m.readFlushPending || m.pendingReadID != 0 {
		t.Fatalf("a chat read to its newest message scheduled a receipt: flush pending=%v, pendingReadID=%d",
			m.readFlushPending, m.pendingReadID)
	}
}

// The read mark counts incoming messages only, so a chat where the reader
// wrote last has its newest message above the mark with nothing to read.
// Comparing the mark against that message sent a receipt on every first
// open of every chat the reader had answered.
func TestOpeningAChatTheReaderAnsweredSendsNoReceipt(t *testing.T) {
	m := unreadChat(5, 0)
	m.OpenChat(testChatID, "nadia")

	page := historyPage(m, 0, 5, 4, 3, 2, 1)
	own := textMessage(6, 100, "my answer")
	own.IsOutgoing = true
	page.messages = append([]*telegram.Message{own}, page.messages...)

	m, _ = m.Update(page)
	if m.readFlushPending || m.pendingReadID != 0 {
		t.Fatalf("a chat whose newest message is the reader's own scheduled a receipt: flush pending=%v, pendingReadID=%d",
			m.readFlushPending, m.pendingReadID)
	}
}

// The read mark alone does not prove there is nothing to read. When the
// count still says unread, the receipt goes out: skipping it would leave
// the badge on the phone that this whole feature exists to clear.
func TestOpeningAChatStillCountedUnreadSendsAReceipt(t *testing.T) {
	m := unreadChat(5, 1)
	m.OpenChat(testChatID, "nadia")

	m, _ = m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	if !m.readFlushPending || m.pendingReadID != 5 {
		t.Fatalf("a chat counted unread scheduled no receipt: flush pending=%v, pendingReadID=%d",
			m.readFlushPending, m.pendingReadID)
	}
}

// A chat the store has no dialog for — reached by a link, say — has no read
// mark to prove it was read, so opening it reads it.
func TestOpeningAChatTheStoreDoesNotKnowSendsAReceipt(t *testing.T) {
	m := unreadChat(3, 2)
	m.OpenChat(testChatID+1, "stranger")

	msg := historyPage(m, 0, 5, 4, 3, 2, 1)
	msg.chatID = testChatID + 1
	m, _ = m.Update(msg)
	if !m.readFlushPending || m.pendingReadID != 5 {
		t.Fatalf("an unknown chat scheduled no receipt: flush pending=%v, pendingReadID=%d",
			m.readFlushPending, m.pendingReadID)
	}
}

// A non-positive ID is a local echo's placeholder, not a message the
// server knows. Reading up to it reads nothing, so the receipt is for the
// newest real message instead.
func TestOpeningAChatReadsPastALocalEcho(t *testing.T) {
	m := unreadChat(3, 2)
	m.OpenChat(testChatID, "nadia")

	m, _ = m.Update(historyPage(m, 0, -1, 5, 4, 3, 2, 1))
	if m.pendingReadID != 5 {
		t.Fatalf("pendingReadID = %d, want 5, the newest message the server knows", m.pendingReadID)
	}
}

// The receipt this sends comes straight back: the client announces
// ChatMarkedReadMsg, and the chat list moves the store's read mark to the
// newest message and zeroes its count. The amber divider must not follow
// the store. It marks where the reader came in, and it stays there until
// they leave the buffer (docs/tui-2.0.md) — a divider that vanished the
// moment the chat opened would never show the reader anything.
func TestTheUnreadDividerSurvivesTheReadItCauses(t *testing.T) {
	m := unreadChat(3, 2)
	m.OpenChat(testChatID, "nadia")
	m, _ = m.Update(historyPage(m, 0, 5, 4, 3, 2, 1))
	m, _ = m.Update(readFlushMsg{chatID: testChatID})

	// The round trip, as the chat list completes it on the shared store.
	m.store.Chats.MarkReadUpTo(testChatID, 5)
	if entry, _ := m.store.Chats.Get(testChatID); entry.UnreadCount != 0 ||
		entry.Chat.LastReadInboxMessageID != 5 {
		t.Fatalf("the round trip left the store at count=%d mark=%d, want 0 and 5",
			entry.UnreadCount, entry.Chat.LastReadInboxMessageID)
	}
	m, _ = m.Update(telegram.ChatMarkedReadMsg{ChatId: testChatID, MaxMessageId: 5})

	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "2 NEW") {
			continue
		}
		if i+1 >= len(lines) || !strings.Contains(lines[i+1], "message 4") {
			t.Fatalf("the divider moved off message 4, the first unread at open:\n%s",
				strings.Join(lines, "\n"))
		}
		return
	}
	t.Fatalf("the divider is gone after the read it caused:\n%s", strings.Join(lines, "\n"))
}
