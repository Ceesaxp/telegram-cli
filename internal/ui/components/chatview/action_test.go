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
