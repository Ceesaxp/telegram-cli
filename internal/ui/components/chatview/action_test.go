package chatview

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// cursorOnMessage is a chat with one message from the given sender under
// the cursor.
func cursorOnMessage(t *testing.T, senderID int64) Model {
	t.Helper()
	m := newTestModel()
	msg := textMessage(10, senderID, "hello")
	m.store.Messages.Append(testChatID, msg)
	m.cursorID = msg.ID
	m.cursorPinned = true
	return m
}

// Telegram allows editing only your own messages, so e on somebody else's is
// correctly refused — but a refusal that says nothing is indistinguishable
// from a key that does not work, which is how "e is not working either" gets
// reported for a rule the client is right to enforce.
func TestEditingSomebodyElsesMessageIsRefusedOutLoud(t *testing.T) {
	m := cursorOnMessage(t, 999)
	m.myUserId = 100

	cmd := m.messageAction("edit")
	if cmd == nil {
		t.Fatal("refusing the edit produced no message at all")
	}
	got, ok := cmd().(MediaPlayMsg)
	if !ok {
		t.Fatalf("e on another user's message produced %T, want a notice", cmd())
	}
	if got.Status != "error" {
		t.Errorf("the refusal is reported as %q, not an error", got.Status)
	}
	if !strings.Contains(strings.ToLower(got.Info), "edit") {
		t.Errorf("the notice does not say what was refused: %q", got.Info)
	}
}

// And it is a refusal, not a no-op with a message: the edit must not be
// handed to the app.
func TestEditingSomebodyElsesMessageDoesNotStartAnEdit(t *testing.T) {
	m := cursorOnMessage(t, 999)
	m.myUserId = 100

	if _, ok := m.messageAction("edit")().(MessageActionMsg); ok {
		t.Error("e on another user's message started an edit")
	}
}

// Your own message edits, and reply is allowed on anything.
func TestYourOwnMessageEditsAndAnyMessageReplies(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		senderID int64
	}{
		{"edit your own", "edit", 100},
		{"reply to your own", "reply", 100},
		{"reply to somebody else's", "reply", 999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := cursorOnMessage(t, tt.senderID)
			m.myUserId = 100

			got, ok := m.messageAction(tt.action)().(MessageActionMsg)
			if !ok {
				t.Fatalf("%s did not start a %s", tt.name, tt.action)
			}
			if got.Action != tt.action || got.MessageId != 10 {
				t.Errorf("got %+v, want action %q on message 10", got, tt.action)
			}
		})
	}
}

// The download directory is configuration, and has to reach the panel that
// saves — an ApplyStorage that reads cfg and keeps nothing leaves `s` with
// an empty destination, which the save then refuses.
func TestApplyStorageCarriesTheDownloadDirectory(t *testing.T) {
	m := newTestModel()
	if m.downloadDir != "" {
		t.Fatalf("a fresh panel already has a download directory: %q", m.downloadDir)
	}

	m.ApplyStorage(config.StorageConfig{
		FilesDir:    "/cache",
		DownloadDir: "/home/you/Downloads",
	})
	if m.downloadDir != "/home/you/Downloads" {
		t.Errorf("downloadDir = %q, want the configured directory", m.downloadDir)
	}
	// The cache is not the destination: saving into it under the sender's
	// filename is what this change exists to stop doing.
	if m.downloadDir == "/cache" {
		t.Error("the panel saves into the media cache")
	}
}

// cursorOnEcho is a chat whose cursor sits on a local echo — a row this
// client invented an ID for and the server has never seen.
func cursorOnEcho(t *testing.T, failed bool) Model {
	t.Helper()
	m := newTestModel()
	m.myUserId = 100
	msg := textMessage(-1, 100, "on its way")
	msg.IsOutgoing = true
	msg.SendFailed = failed
	m.store.Messages.Append(testChatID, msg)
	m.cursorID = msg.ID
	m.cursorPinned = true
	return m
}

// Every action key ends in a call naming a message ID to Telegram, and a
// local echo has none to give: acting on one asks the server to reply to,
// edit, delete or react to whatever real message happens to carry that
// number.
func TestServerActionsAreRefusedOnALocalEcho(t *testing.T) {
	for _, action := range []string{"reply", "edit", "delete", "forward", "react", "pin", "thread"} {
		t.Run(action, func(t *testing.T) {
			m := cursorOnEcho(t, false)

			cmd := m.messageAction(action)
			if cmd == nil {
				t.Fatal("the refusal produced no message at all")
			}
			got := cmd()
			if _, ok := got.(MessageActionMsg); ok {
				t.Fatalf("%s on a local echo was handed to the app", action)
			}
			notice, ok := got.(MediaPlayMsg)
			if !ok {
				t.Fatalf("%s on a local echo produced %T, want a notice", action, got)
			}
			if notice.Status != "error" {
				t.Errorf("the refusal is reported as %q, not an error", notice.Status)
			}
			// The notice says which of the two states the row is in,
			// because "wait" and "it will never work" are different
			// answers.
			if !strings.Contains(notice.Info, "not sent yet") {
				t.Errorf("the notice does not say why: %q", notice.Info)
			}
		})
	}
}

// A failed echo is not going to confirm, and saying "not sent yet" about it
// would be an invitation to wait for something that will never happen.
func TestARefusedActionOnAFailedEchoSaysTheSendFailed(t *testing.T) {
	m := cursorOnEcho(t, true)

	notice, ok := m.messageAction("reply")().(MediaPlayMsg)
	if !ok {
		t.Fatal("reply on a failed echo was not refused")
	}
	if !strings.Contains(notice.Info, "send failed") {
		t.Errorf("notice = %q, want it to say the send failed", notice.Info)
	}
}

// A failed echo has to have a way out, or it is selectable forever: nothing
// about it ever reached Telegram, so dropping the row is a local edit.
func TestDeleteDismissesAFailedEchoLocally(t *testing.T) {
	m := cursorOnEcho(t, true)
	renderOne(m, m.store.Messages.Get(testChatID)[0]) // warm the cache

	m, cmd := m.handleKey(key('d'))

	if n := m.store.Messages.Count(testChatID); n != 0 {
		t.Fatalf("%d messages left, want the failed echo gone", n)
	}
	if _, ok := m.cache.get(-1); ok {
		t.Error("the dismissed row kept its cached rendering")
	}
	if cmd == nil {
		t.Fatal("the dismissal said nothing at all")
	}
	notice, ok := cmd().(MediaPlayMsg)
	if !ok {
		t.Fatalf("the dismissal produced %T, want a notice", cmd())
	}
	if _, isAction := cmd().(MessageActionMsg); isAction {
		t.Fatal("dismissing a failed echo asked Telegram to delete it")
	}
	if !strings.Contains(notice.Info, "never sent") {
		t.Errorf("notice = %q, want it to say the message was never sent", notice.Info)
	}
}

// A pending echo gets no such exit: it may still confirm, and removing it
// would throw away a message that is on its way.
func TestDeleteOnAPendingEchoIsRefusedRatherThanDismissed(t *testing.T) {
	m := cursorOnEcho(t, false)

	m, cmd := m.handleKey(key('d'))

	if n := m.store.Messages.Count(testChatID); n != 1 {
		t.Fatalf("%d messages left, want the pending echo kept", n)
	}
	notice, ok := cmd().(MediaPlayMsg)
	if !ok {
		t.Fatalf("d on a pending echo produced %T, want a notice", cmd())
	}
	if !strings.Contains(notice.Info, "not sent yet") {
		t.Errorf("notice = %q, want it to say the send has not landed", notice.Info)
	}
}

// And a real message still goes to Telegram the way it always did: the gate
// is about placeholders, not about delete.
func TestDeleteOnAServerMessageStillReachesTelegram(t *testing.T) {
	m := cursorOnMessage(t, 100)
	m.myUserId = 100

	_, cmd := m.handleKey(key('d'))
	if cmd == nil {
		t.Fatal("d on a real message did nothing")
	}
	got, ok := cmd().(MessageActionMsg)
	if !ok {
		t.Fatalf("d on a real message produced %T, want the delete action", cmd())
	}
	if got.Action != "delete" || got.MessageId != 10 {
		t.Errorf("got %+v, want a delete of message 10", got)
	}
}
