package chatview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// actionModel is a thread with the cursor on a message the test chooses,
// which is what every action here operates on.
func actionModel(t *testing.T, content telegram.MessageContent) Model {
	t.Helper()
	m := newTestModel()
	m.focused = true
	m.myUserId = 100
	m.chatID = testChatID
	m.SetSize(100, 20)

	msg := textMessage(1, 200, "placeholder")
	msg.Content = content
	m.store.Messages.Append(testChatID, msg)
	m.setCursor(msg.ID)
	return m
}

func text(s string) telegram.MessageContent {
	return &telegram.MessageText{Text: &telegram.FormattedText{Text: s}}
}

// --- y -------------------------------------------------------------------

// The message's own text, not the rendered body. The render has a gutter,
// wraps at the pane width and frames code blocks; none of that belongs in a
// paste buffer.
func TestYankTakesTheMessageSourceNotTheRender(t *testing.T) {
	const long = "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}"
	m := actionModel(t, text(long))

	if got := messageText(m.cursorMessage()); got != long {
		t.Errorf("messageText = %q, want the source unchanged", got)
	}
}

func TestYankReadsACaptionForMedia(t *testing.T) {
	m := actionModel(t, &telegram.MessagePhoto{
		Photo:   &telegram.Photo{},
		Caption: &telegram.FormattedText{Text: "the deploy graph"},
	})
	if got := messageText(m.cursorMessage()); got != "the deploy graph" {
		t.Errorf("messageText = %q, want the caption", got)
	}
}

// A photo nobody captioned has no words in it. That is a fact about the
// message, and the host says so rather than reporting a successful copy of
// nothing.
func TestYankingAMessageWithNoTextIsRefused(t *testing.T) {
	m := actionModel(t, &telegram.MessagePhoto{Photo: &telegram.Photo{}})

	if got := messageText(m.cursorMessage()); got != "" {
		t.Errorf("messageText = %q, want empty", got)
	}
	if cmd := m.YankCmd(); cmd != nil {
		t.Error("YankCmd returned a command for a message with no text")
	}
}

// The key still reports, even when there is nothing to copy — a press that
// does nothing and says nothing looks like it worked.
func TestYPressAlwaysReports(t *testing.T) {
	m := actionModel(t, &telegram.MessagePhoto{Photo: &telegram.Photo{}})

	_, cmd := m.handleKey(key('y'))
	if cmd == nil {
		t.Fatal("y produced no command on a message with no text")
	}
	msg, ok := cmd().(YankMsg)
	if !ok {
		t.Fatalf("y produced %T, want a YankMsg", cmd())
	}
	if msg.Runes != 0 || msg.Err != nil {
		t.Errorf("YankMsg = %+v, want the empty report", msg)
	}
}

// --- space ---------------------------------------------------------------

// Space is the big easy key and it means one thing. Making it also open
// documents and spawn video players would make it "do whatever this message
// implies", which is what enter is for.
func TestSpacePlaysVoiceAndNothingElse(t *testing.T) {
	file := &telegram.File{ID: "f1"}
	tests := []struct {
		name    string
		content telegram.MessageContent
		want    bool
	}{
		{"a voice note", &telegram.MessageVoiceNote{VoiceNote: &telegram.VoiceNote{File: file}}, true},
		{"an audio message", &telegram.MessageAudio{Audio: &telegram.Audio{File: file}}, true},
		{"a video", &telegram.MessageVideo{Video: &telegram.Video{File: file}}, false},
		{"a document", &telegram.MessageDocument{Document: &telegram.Document{File: file}}, false},
		{"plain text", text("hello"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := actionModel(t, tt.content)
			if got := m.PlayVoiceCmd() != nil; got != tt.want {
				t.Errorf("PlayVoiceCmd non-nil = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpaceIsWiredToTheVoicePlayer(t *testing.T) {
	m := actionModel(t, &telegram.MessageVoiceNote{
		VoiceNote: &telegram.VoiceNote{File: &telegram.File{ID: "f1"}},
	})
	if _, cmd := m.handleKey(specialKey(' ')); cmd == nil {
		t.Error("space did not reach the voice player")
	}
}

// --- M -------------------------------------------------------------------

// The point of an explicit mark-read is to clear the badge while you keep
// reading where you are. A version that scrolled would defeat it.
func TestMarkReadDoesNotMoveTheScroll(t *testing.T) {
	m := keysTestModel()
	m.tg = nil // MarkReadCmd needs a client to send; the scroll does not

	m.scrollOffset = 4
	before := m.scrollOffset
	after, _ := m.handleKey(key('M'))

	if after.scrollOffset != before {
		t.Errorf("M moved the scroll from %d to %d", before, after.scrollOffset)
	}
}

// The unread divider stays where the reader left it until they leave the
// buffer. Marking read clears the badge, not the reader's place in the
// history.
func TestMarkReadLeavesTheUnreadDividerAlone(t *testing.T) {
	m := keysTestModel()
	m.tg = nil
	m.unreadFromID = 5
	m.unreadCount = 3

	after, _ := m.handleKey(key('M'))
	if after.unreadFromID != 5 || after.unreadCount != 3 {
		t.Errorf("M moved the unread divider: from=%d count=%d",
			after.unreadFromID, after.unreadCount)
	}
}

// --- enter ---------------------------------------------------------------

// A photo raises the overlay; everything else keeps the behaviour that
// already works, because a video or a document has no in-terminal form this
// client can draw.
func TestEnterOverlaysOnlyPhotos(t *testing.T) {
	file := &telegram.File{ID: "f1"}
	tests := []struct {
		name    string
		content telegram.MessageContent
		overlay bool
	}{
		{"a photo", &telegram.MessagePhoto{Photo: photoWith(file)}, true},
		{"a video", &telegram.MessageVideo{Video: &telegram.Video{File: file}}, false},
		{"a document", &telegram.MessageDocument{Document: &telegram.Document{File: file}}, false},
		{"plain text", text("hello"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := actionModel(t, tt.content)
			if got := m.OverlayPhotoCmd() != nil; got != tt.overlay {
				t.Errorf("OverlayPhotoCmd non-nil = %v, want %v", got, tt.overlay)
			}
		})
	}
}

// 'o' is the way past the overlay for a photo the terminal draws badly, and
// the overlay's own hint row advertises it. It must never raise the overlay.
func TestOAlwaysOpensExternally(t *testing.T) {
	m := actionModel(t, &telegram.MessagePhoto{Photo: photoWith(&telegram.File{ID: "f1"})})

	_, cmd := m.handleKey(key('o'))
	if cmd == nil {
		t.Fatal("o produced no command")
	}
	// Through carriesOpenPhoto rather than a direct type assertion: the
	// overlay arrives inside a tea.Batch, so asserting on cmd() alone sees
	// a BatchMsg and passes whatever is in it.
	if carriesOpenPhoto(t, cmd) {
		t.Error("o raised the overlay instead of opening externally")
	}
}

// The overlay is announced before the download starts, so the user sees the
// window they asked for rather than a pause.
func TestEnterOpensTheOverlayBeforeItDownloads(t *testing.T) {
	m := actionModel(t, &telegram.MessagePhoto{Photo: photoWith(&telegram.File{ID: "f1"})})

	_, cmd := m.handleKey(key('\r'))
	if cmd == nil {
		t.Fatal("enter produced no command on a photo")
	}
	if !carriesOpenPhoto(t, cmd) {
		t.Error("enter did not ask for the overlay")
	}
}

// carriesOpenPhoto walks a batch looking for the overlay request. A batch is
// a message with a slice of commands inside it, so this has to run them.
func carriesOpenPhoto(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		_, direct := cmd().(OpenPhotoMsg)
		return direct
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		if _, isOpen := c().(OpenPhotoMsg); isOpen {
			return true
		}
	}
	return false
}

func photoWith(f *telegram.File) *telegram.Photo {
	return &telegram.Photo{Sizes: []*telegram.PhotoSize{
		{Type: "x", Width: 1280, Height: 960, File: f},
	}}
}
