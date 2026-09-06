package chatview

import (
	"errors"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// echoModel is a thread that knows who it is, so its own messages get a
// send-state mark at all — sendStateFor reads ownership off the sender.
func echoModel() Model {
	m := newTestModel()
	m.myUserId = 100
	return m
}

// localEcho is what the app puts in the store the moment Enter is pressed.
func localEcho(id int64, text string) *telegram.Message {
	msg := textMessage(id, 100, text)
	msg.IsOutgoing = true
	return msg
}

// A message the server has not acknowledged must not wear a delivery tick:
// a check mark on a message still sitting in a socket buffer is the client
// claiming something it does not know.
func TestLocalEchoDrawsThePendingMarkNotATick(t *testing.T) {
	m := echoModel()
	echo := localEcho(-1, "on its way")
	m.store.Messages.Append(testChatID, echo)

	if state := m.sendStateFor(echo); state != sendPending {
		t.Fatalf("send state = %d, want pending", state)
	}
	out, _ := renderOne(m, echo)
	if !strings.Contains(out, sendPending.glyph()) {
		t.Errorf("rendered echo = %q, want the pending mark in it", out)
	}
	if strings.Contains(out, "✓") {
		t.Errorf("rendered echo = %q, want no delivery tick", out)
	}
}

// A send that failed must say so on the row itself. Leaving the pending
// mark would be the thread telling a lie it never takes back, and removing
// the row would throw away the text the user typed.
func TestSendFailureMarksTheEchoRatherThanRemovingIt(t *testing.T) {
	m := echoModel()
	echo := localEcho(-1, "never left")
	m.store.Messages.Append(testChatID, echo)
	renderOne(m, echo) // warm the cache, so the flip has something to drop

	m, _ = m.Update(telegram.MessageSendFailedMsg{
		ChatId: testChatID, OldMessageId: -1, Err: errors.New("connection lost"),
	})

	msgs := m.store.Messages.Get(testChatID)
	if len(msgs) != 1 || !msgs[0].SendFailed {
		t.Fatalf("store = %v, want the echo still there and marked failed", msgs)
	}
	if _, ok := m.cache.get(-1); ok {
		t.Error("the failed row kept its cached pending rendering")
	}
	if state := m.sendStateFor(msgs[0]); state != sendFailed {
		t.Fatalf("send state = %d, want failed", state)
	}
	out, _ := renderOne(m, msgs[0])
	if !strings.Contains(out, sendFailed.glyph()) {
		t.Errorf("rendered echo = %q, want the failure mark in it", out)
	}
}

// A failure for another chat's send must not touch this thread.
func TestSendFailureForAnotherChatIsIgnored(t *testing.T) {
	m := echoModel()
	echo := localEcho(-1, "fine")
	m.store.Messages.Append(testChatID, echo)

	m, _ = m.Update(telegram.MessageSendFailedMsg{
		ChatId: testChatID + 1, OldMessageId: -1, Err: errors.New("elsewhere"),
	})
	if m.store.Messages.Get(testChatID)[0].SendFailed {
		t.Fatal("a failure in another chat marked this thread's row")
	}
}

// The happy path: the confirmed message takes the echo's place, and there
// is exactly one of it.
func TestSendSuccessSwapsTheEchoForTheConfirmedMessage(t *testing.T) {
	m := echoModel()
	m.store.Messages.Append(testChatID, textMessage(8, 200, "earlier"))
	m.store.Messages.Append(testChatID, localEcho(-1, "**hi**"))

	confirmed := localEcho(9, "hi")
	m, _ = m.Update(telegram.MessageSendSucceededMsg{Message: confirmed, OldMessageId: -1})

	got := m.store.Messages.Get(testChatID)
	if len(got) != 2 || got[0].ID != 8 || got[1].ID != 9 {
		t.Fatalf("messages = %v, want the echo replaced by ID 9", messageIDs(got))
	}
}

// The dispatcher can beat the send call home. When it has, the success
// message must remove the echo rather than duplicate the message.
func TestSendSuccessRemovesTheEchoWhenTheUpdateArrivedFirst(t *testing.T) {
	m := echoModel()
	m.store.Messages.Append(testChatID, localEcho(-1, "**hi**"))

	// The server's own copy, arriving through the update stream.
	confirmed := localEcho(9, "hi")
	m, _ = m.Update(telegram.NewMessageMsg{Message: confirmed})
	if n := m.store.Messages.Count(testChatID); n != 2 {
		t.Fatalf("precondition: %d messages, want the echo and the arrival", n)
	}

	m, _ = m.Update(telegram.MessageSendSucceededMsg{Message: confirmed, OldMessageId: -1})

	got := m.store.Messages.Get(testChatID)
	if len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("messages = %v, want only ID 9", messageIDs(got))
	}
}

// Negative IDs must not reorder anything: the thread draws the store in
// insertion order, and the echo belongs at the bottom where it was typed.
func TestLocalEchoStaysAtTheBottomOfTheThread(t *testing.T) {
	m := echoModel()
	for id := int64(1); id <= 3; id++ {
		m.store.Messages.Append(testChatID, textMessage(id, 200, "older"))
	}
	m.store.Messages.Append(testChatID, localEcho(-1, "newest"))

	got := m.store.Messages.Get(testChatID)
	if ids := messageIDs(got); len(ids) != 4 || ids[3] != -1 {
		t.Fatalf("messages = %v, want the echo last", ids)
	}

	blocks, counts := m.renderedMessages(got)
	if len(blocks) != 4 || len(counts) != 4 {
		t.Fatalf("rendered %d blocks, want 4", len(blocks))
	}
	last := strings.Join(blocks[3], "\n")
	if !strings.Contains(last, "newest") {
		t.Fatalf("last rendered block = %q, want the echo", last)
	}
}

func messageIDs(msgs []*telegram.Message) []int64 {
	ids := make([]int64, len(msgs))
	for i, msg := range msgs {
		ids[i] = msg.ID
	}
	return ids
}

// The attachment echo is a document card for a file nobody has downloaded —
// no File behind it at all — and the thread has to draw it as one, with the
// pending mark and the real filename, rather than fall over the missing
// file.
func TestAttachmentEchoRendersAsAPendingFileCard(t *testing.T) {
	m := echoModel()
	echo := localEcho(-1, "")
	echo.Content = &telegram.MessageDocument{
		Document: &telegram.Document{FileName: "patch.png", MimeType: "image/png"},
		Caption:  &telegram.FormattedText{Text: "the fix"},
	}
	m.store.Messages.Append(testChatID, echo)

	if state := m.sendStateFor(echo); state != sendPending {
		t.Fatalf("send state = %d, want pending", state)
	}
	out, _ := renderOne(m, echo)
	if !strings.Contains(out, "patch.png") {
		t.Errorf("rendered echo = %q, want the filename in it", out)
	}
	if !strings.Contains(out, "the fix") {
		t.Errorf("rendered echo = %q, want the caption in it", out)
	}
	if !strings.Contains(out, sendPending.glyph()) {
		t.Errorf("rendered echo = %q, want the pending mark in it", out)
	}
}

// A send outlives the chat it was made in: submit, then switch chats while
// the round trip runs. The placeholder still has to be reconciled, or the
// reader comes back to the message twice over — once as the echo nobody
// swapped out and once as the copy the update stream delivered.
func TestSendSuccessReconcilesAChatThatIsNotOpen(t *testing.T) {
	const other = testChatID + 1
	m := echoModel()
	echo := localEcho(-1, "**sent from elsewhere**")
	echo.ChatID = other
	m.store.Messages.Append(other, echo)

	confirmed := localEcho(9, "sent from elsewhere")
	confirmed.ChatID = other
	m, _ = m.Update(telegram.MessageSendSucceededMsg{Message: confirmed, OldMessageId: -1})

	got := m.store.Messages.Get(other)
	if len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("messages in the closed chat = %v, want the echo replaced by ID 9", messageIDs(got))
	}
}

// And when the update stream beat the send home into that closed chat, the
// echo is removed rather than duplicated — the same rule the open chat gets.
func TestSendSuccessLeavesNoDuplicateInAChatThatIsNotOpen(t *testing.T) {
	const other = testChatID + 1
	m := echoModel()
	echo := localEcho(-1, "**hi**")
	echo.ChatID = other
	confirmed := localEcho(9, "hi")
	confirmed.ChatID = other
	m.store.Messages.Append(other, echo)
	m.store.Messages.Append(other, confirmed)

	m, _ = m.Update(telegram.MessageSendSucceededMsg{Message: confirmed, OldMessageId: -1})

	got := m.store.Messages.Get(other)
	if len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("messages in the closed chat = %v, want only ID 9", messageIDs(got))
	}
}

// A failure for a chat that is no longer open still marks its row. Left
// unmarked it sits there pending forever, which is the client telling a lie
// it never takes back.
func TestSendFailureMarksTheEchoInAChatThatIsNotOpen(t *testing.T) {
	const other = testChatID + 1
	m := echoModel()
	open := localEcho(-1, "this chat's own row")
	m.store.Messages.Append(testChatID, open)
	renderOne(m, open) // warm the open chat's cache at the same ID

	elsewhere := localEcho(-1, "never left")
	elsewhere.ChatID = other
	m.store.Messages.Append(other, elsewhere)

	m, _ = m.Update(telegram.MessageSendFailedMsg{
		ChatId: other, OldMessageId: -1, Err: errors.New("connection lost"),
	})

	if got := m.store.Messages.Get(other); len(got) != 1 || !got[0].SendFailed {
		t.Fatalf("closed chat = %v, want its echo marked failed", got)
	}
	// The store write is unconditional; the cache is not. Dropping the open
	// chat's rendering off another chat's failure would be a redraw for a
	// row that did not change.
	if _, ok := m.cache.get(-1); !ok {
		t.Error("another chat's failure dropped this thread's cached row")
	}
	if m.store.Messages.Get(testChatID)[0].SendFailed {
		t.Error("another chat's failure marked this thread's row")
	}
}
