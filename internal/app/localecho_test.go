package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
)

// echoText digs the plain text out of a stored message, so the assertions
// below read as "what does the thread now say" rather than as three type
// assertions each.
func echoText(t *testing.T, msg *telegram.Message) string {
	t.Helper()
	text, ok := msg.Content.(*telegram.MessageText)
	if !ok || text.Text == nil {
		t.Fatalf("message %d holds %T, want text", msg.ID, msg.Content)
	}
	return text.Text.Text
}

// A text send must be visible the instant the composer clears. The send
// itself takes a round trip — up to the operation timeout on a reconnecting
// link — and before the local echo the thread showed nothing at all for the
// whole of it.
func TestTextSubmitEchoesIntoTheStoreImmediately(t *testing.T) {
	m := newTestModel(t)
	m.myUserId = 42

	updated, _ := m.Update(composer.MessageSubmittedMsg{ChatId: 7, Text: "**hello**", ReplyToId: 3})
	got := updated.(Model)

	msgs := got.store.Messages.Get(7)
	if len(msgs) != 1 {
		t.Fatalf("stored %d messages, want 1", len(msgs))
	}
	echo := msgs[0]
	if echo.ID >= 0 {
		t.Errorf("echo ID = %d, want a negative placeholder", echo.ID)
	}
	if !echo.IsOutgoing || echo.ChatID != 7 || echo.ReplyToMessageID != 3 {
		t.Errorf("echo = %+v, want an outgoing reply in chat 7", echo)
	}
	if echo.Date == 0 {
		t.Error("echo carries no date, so it would sort under the epoch day divider")
	}
	sender, ok := echo.SenderID.(*telegram.MessageSenderUser)
	if !ok || sender.UserID != 42 {
		t.Errorf("echo sender = %+v, want this account — the thread reads ownership off it", echo.SenderID)
	}
	// The raw markdown is deliberate: the server-rendered copy replaces it
	// when the send confirms.
	if body := echoText(t, echo); body != "**hello**" {
		t.Errorf("echo text = %q, want the text exactly as typed", body)
	}
}

// Two sends in flight at once — which is the normal case on the slow link
// that makes the echo worth having — must not collide on one ID, or the
// store would dedup the second into the first.
func TestConcurrentTextSubmitsGetDistinctEchoIds(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(composer.MessageSubmittedMsg{ChatId: 7, Text: "first"})
	updated, _ = updated.(Model).Update(composer.MessageSubmittedMsg{ChatId: 7, Text: "second"})
	got := updated.(Model)

	msgs := got.store.Messages.Get(7)
	if len(msgs) != 2 {
		t.Fatalf("stored %d messages, want 2", len(msgs))
	}
	if msgs[0].ID == msgs[1].ID {
		t.Fatalf("both echoes took ID %d", msgs[0].ID)
	}
	// Store order is insertion order, and that is what the thread draws.
	// Negative IDs must not have reordered anything.
	if echoText(t, msgs[0]) != "first" || echoText(t, msgs[1]) != "second" {
		t.Fatalf("echoes are out of order: %q then %q",
			echoText(t, msgs[0]), echoText(t, msgs[1]))
	}
}

// An edit changes a message that is already in the thread, and a submit with
// no chat open goes nowhere at all: neither draws an echo, so neither may
// consume a placeholder ID or put a row in the store.
func TestEditsAndChatlessSubmitsDoNotEcho(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  composer.MessageSubmittedMsg
	}{
		{"edit", composer.MessageSubmittedMsg{ChatId: 7, Text: "fixed", EditMessageId: 9}},
		{"no chat open", composer.MessageSubmittedMsg{Text: "nowhere"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			updated, _ := m.Update(tc.msg)
			got := updated.(Model)
			if n := got.store.Messages.Count(7); n != 0 {
				t.Fatalf("a %s send echoed %d messages into the thread", tc.name, n)
			}
			if got.lastLocalEchoID != 0 {
				t.Fatalf("a %s send consumed placeholder ID %d", tc.name, got.lastLocalEchoID)
			}
		})
	}
}

// A failed send still has to say so in words. The row marks itself in the
// thread; this is the other half, and it goes through the same notice path
// the plain ErrorMsg took before.
func TestSendFailureStillReachesTheNoticeRow(t *testing.T) {
	m := newTestModel(t)
	updated, cmd := m.Update(telegram.MessageSendFailedMsg{
		ChatId: 7, OldMessageId: -1, Err: errors.New("connection lost"),
	})
	if cmd == nil {
		t.Fatal("a send failure produced no follow-up command")
	}
	updated, _ = updated.(Model).Update(cmd())
	got := updated.(Model)

	if view := got.composer.View(); !strings.Contains(view, "connection lost") {
		t.Errorf("composer notice = %q, want the send error in it", view)
	}
}

// fakeUploads records the eager-upload calls the attachment path makes. The
// pairing is the whole point: a start with no cancel is a goroutine reading
// a spool file that is about to be deleted.
type fakeUploads struct {
	started   []string
	cancelled []string
}

func (f *fakeUploads) StartUpload(path string)  { f.started = append(f.started, path) }
func (f *fakeUploads) CancelUpload(path string) { f.cancelled = append(f.cancelled, path) }

// echoDocument digs the document card out of a stored message.
func echoDocument(t *testing.T, msg *telegram.Message) *telegram.Document {
	t.Helper()
	doc, ok := msg.Content.(*telegram.MessageDocument)
	if !ok || doc.Document == nil {
		t.Fatalf("message %d holds %T, want a document", msg.ID, msg.Content)
	}
	return doc.Document
}

// An attachment send waits on a whole file reaching Telegram, so it is the
// send that most needs to be visible the instant the composer clears.
func TestAttachmentSubmitEchoesIntoTheStoreImmediately(t *testing.T) {
	m := newTestModel(t)
	m.myUserId = 42

	updated, _ := m.Update(composer.MessageSubmittedMsg{
		ChatId: 7, Text: "the patch", Attachment: "/tmp/spool/fix.png", AsPhoto: true, ReplyToId: 3,
	})
	got := updated.(Model)

	msgs := got.store.Messages.Get(7)
	if len(msgs) != 1 {
		t.Fatalf("stored %d messages, want 1", len(msgs))
	}
	echo := msgs[0]
	if echo.ID >= 0 {
		t.Errorf("echo ID = %d, want a negative placeholder", echo.ID)
	}
	if !echo.IsOutgoing || echo.ChatID != 7 || echo.ReplyToMessageID != 3 {
		t.Errorf("echo = %+v, want an outgoing reply in chat 7", echo)
	}
	if echo.Date == 0 {
		t.Error("echo carries no date, so it would sort under the epoch day divider")
	}
	sender, ok := echo.SenderID.(*telegram.MessageSenderUser)
	if !ok || sender.UserID != 42 {
		t.Errorf("echo sender = %+v, want this account — the thread reads ownership off it", echo.SenderID)
	}
	doc := echoDocument(t, echo)
	if doc.FileName != "fix.png" {
		t.Errorf("echo names the file %q, want fix.png", doc.FileName)
	}
	// The mime type is what makes the renderer label a picture IMG rather
	// than DOC, which is the difference the reader actually sees.
	if !strings.HasPrefix(doc.MimeType, "image/") {
		t.Errorf("echo mime = %q, want an image type", doc.MimeType)
	}
	if doc.File != nil {
		t.Error("echo claims a downloadable file, which nothing has downloaded")
	}
	caption, ok := echo.Content.(*telegram.MessageDocument)
	if !ok || caption.Caption == nil || caption.Caption.Text != "the patch" {
		t.Errorf("echo caption = %+v, want the text as typed", caption.Caption)
	}
}

// Staging a file starts its upload, and every way of dropping one has to
// stop it again: the upload goroutine is reading a spool file the discard is
// about to delete.
func TestAttachingStartsAnUploadAndDiscardingCancelsIt(t *testing.T) {
	t.Run("attach then discard", func(t *testing.T) {
		m := newTestModel(t)
		uploads := &fakeUploads{}
		m.uploads = uploads
		m.composer.SetChatId(7)

		updated, _ := m.Update(ClipboardPastedMsg{ChatId: 7, Path: "/tmp/spool/one.png", IsImage: true})
		got := updated.(Model)
		if len(uploads.started) != 1 || uploads.started[0] != "/tmp/spool/one.png" {
			t.Fatalf("started %v, want the pasted file", uploads.started)
		}

		updated, _ = got.Update(composer.AttachmentDiscardedMsg{Path: "/tmp/spool/one.png"})
		got = updated.(Model)
		if len(uploads.cancelled) != 1 || uploads.cancelled[0] != "/tmp/spool/one.png" {
			t.Fatalf("cancelled %v, want the discarded file", uploads.cancelled)
		}
	})

	t.Run("attach then replace", func(t *testing.T) {
		m := newTestModel(t)
		uploads := &fakeUploads{}
		m.uploads = uploads
		m.composer.SetChatId(7)

		updated, _ := m.Update(ClipboardPastedMsg{ChatId: 7, Path: "/tmp/spool/one.png", IsImage: true})
		updated, _ = updated.(Model).Update(ClipboardPastedMsg{ChatId: 7, Path: "/tmp/spool/two.png", IsImage: true})
		_ = updated

		if len(uploads.started) != 2 || uploads.started[1] != "/tmp/spool/two.png" {
			t.Fatalf("started %v, want both files", uploads.started)
		}
		// The displaced file's spool copy is deleted here, so its upload
		// must not still be reading it.
		if len(uploads.cancelled) != 1 || uploads.cancelled[0] != "/tmp/spool/one.png" {
			t.Fatalf("cancelled %v, want the file that was replaced", uploads.cancelled)
		}
	})
}

// A failed attachment send has two halves, and both matter: the file comes
// back so it can be sent again, and the row it echoed stops claiming to be
// on its way.
func TestAttachmentSendFailureRestoresTheFileAndMarksTheEcho(t *testing.T) {
	m := newTestModel(t)
	m.composer.SetChatId(7)
	m.chatView.OpenChatAt(7, "somewhere", 0)

	updated, _ := m.Update(composer.MessageSubmittedMsg{
		ChatId: 7, Text: "caption", Attachment: "/tmp/spool/fix.png", AsPhoto: true,
	})
	got := updated.(Model)
	echoID := got.store.Messages.Get(7)[0].ID

	updated, _ = got.Update(SendFailedMsg{
		Err: errors.New("connection lost"), ChatId: 7,
		Attachment: "/tmp/spool/fix.png", AsPhoto: true, EchoId: echoID,
	})
	got = updated.(Model)

	if att := got.composer.Attachment(); att != "/tmp/spool/fix.png" {
		t.Errorf("composer attachment = %q, want the file back for a retry", att)
	}
	msgs := got.store.Messages.Get(7)
	if len(msgs) != 1 {
		t.Fatalf("stored %d messages, want the echo to still be there", len(msgs))
	}
	if !msgs[0].SendFailed {
		t.Error("the echoed row is still claiming to be on its way")
	}
	// The notice row is narrow enough here to cut the error text, so this
	// asserts the half that fits: that a notice was raised at all.
	if view := got.composer.View(); !strings.Contains(view, "send failed") {
		t.Errorf("composer notice = %q, want the send failure in it", view)
	}
}

// The chip above the prompt is where the reader is looking while the caption
// is typed, so that is where the upload has to report.
func TestUploadProgressReachesTheAttachmentChip(t *testing.T) {
	m := newTestModel(t)
	m.composer.SetChatId(7)
	m.composer.SetSize(60, 3)
	updated, _ := m.Update(ClipboardPastedMsg{ChatId: 7, Path: "/tmp/spool/one.png", IsImage: true})
	got := updated.(Model)

	updated, _ = got.Update(telegram.UploadProgressMsg{Path: "/tmp/spool/one.png", Uploaded: 30, Total: 100})
	got = updated.(Model)
	if view := got.composer.View(); !strings.Contains(view, "↑ 30%") {
		t.Errorf("composer view = %q, want the upload percentage on the chip", view)
	}

	updated, _ = got.Update(telegram.UploadProgressMsg{Path: "/tmp/spool/one.png", Uploaded: 100, Total: 100})
	got = updated.(Model)
	if view := got.composer.View(); !strings.Contains(view, "✓") {
		t.Errorf("composer view = %q, want the ready mark once the file is up", view)
	}
}
