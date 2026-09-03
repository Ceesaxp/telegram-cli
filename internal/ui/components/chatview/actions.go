package chatview

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/clipboard"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// errNoClient is what an action reports when there is no Telegram client —
// which happens in tests, and would otherwise be a nil dereference inside a
// goroutine rather than a message the host can show.
var errNoClient = errors.New("not connected")

// YankMsg reports what a copy did. The host phrases the notice, because the
// hint bar is the host's row and the wording belongs with the other notices
// rather than beside the clipboard call.
type YankMsg struct {
	// Runes is how much was copied, so the notice can say so. A reader who
	// yanks a long message and gets a silent confirmation has no way to
	// tell it from a yank that copied an empty caption.
	Runes int
	Err   error
}

// OpenPhotoMsg asks the host to raise the media overlay. It carries the
// caption for the overlay's header; the download follows in [OpenedPhotoMsg].
type OpenPhotoMsg struct {
	Caption string
}

// OpenedPhotoMsg is the downloaded file for the overlay, or why there is
// none.
type OpenedPhotoMsg struct {
	Path string
	Err  error
}

// YankCmd copies the cursored message's text to the system clipboard.
//
// The message's TEXT, exactly as Telegram sent it — not the rendered body.
// The render has a gutter, wraps at the pane width, and draws a code block
// inside a frame; none of that is wanted in a paste buffer. Taking the
// source also means a message that is nothing but a code block yanks as the
// code, which is what "copy a message or a code block" comes down to once
// there is no second binding for it.
//
// Returns nil when there is nothing to copy, which the host reads as "say
// so" rather than as success.
func (m Model) YankCmd() tea.Cmd {
	msg := m.cursorMessage()
	if msg == nil {
		return nil
	}
	text := messageText(msg)
	if text == "" {
		return nil
	}

	return func() tea.Msg {
		if err := clipboard.Copy(text); err != nil {
			return YankMsg{Err: err}
		}
		return YankMsg{Runes: len([]rune(text))}
	}
}

// messageText is a message's own words: its text, or an attachment's
// caption. Empty for a photo nobody captioned, which is a fact about the
// message rather than a failure.
func messageText(msg *telegram.Message) string {
	if msg == nil || msg.Content == nil {
		return ""
	}
	var ft *telegram.FormattedText
	switch c := msg.Content.(type) {
	case *telegram.MessageText:
		ft = c.Text
	case *telegram.MessagePhoto:
		ft = c.Caption
	case *telegram.MessageVideo:
		ft = c.Caption
	case *telegram.MessageDocument:
		ft = c.Caption
	case *telegram.MessageAudio:
		ft = c.Caption
	case *telegram.MessageAnimation:
		ft = c.Caption
	}
	if ft == nil {
		return ""
	}
	return strings.TrimRight(ft.Text, "\n")
}

// PlayVoiceCmd plays the cursored voice note or audio message.
//
// Voice only, deliberately. Space is a big, easy key and the point of it is
// that a voice note is one press away; making it also open documents and
// spawn video players would make "the big key" mean "do whatever this
// message implies", which is what enter is for.
func (m Model) PlayVoiceCmd() tea.Cmd {
	msg := m.cursorMessage()
	if msg == nil || msg.Content == nil {
		return nil
	}
	switch c := msg.Content.(type) {
	case *telegram.MessageVoiceNote:
		if c.VoiceNote != nil {
			return m.downloadAndPlay(fileKey(c.VoiceNote.File), "voice", "▶ playing voice note")
		}
	case *telegram.MessageAudio:
		if c.Audio != nil {
			return m.downloadAndPlay(fileKey(c.Audio.File), "audio", "▶ playing audio")
		}
	}
	return nil
}

// OverlayPhotoCmd raises the media overlay on the cursored message and
// downloads the picture for it.
//
// Photos only. A video, a document or a voice note has no in-terminal
// representation this client can draw, so those keep the existing behaviour
// of handing the file to the platform — an overlay that says "cannot draw
// this" is worse than the thing that already works.
//
// Returns nil when the cursor is not on a photo, which is the host's signal
// to fall through to that existing behaviour.
func (m Model) OverlayPhotoCmd() tea.Cmd {
	msg := m.cursorMessage()
	if msg == nil {
		return nil
	}
	key, caption, ok := m.overlayPicture(msg)
	if !ok {
		return nil
	}
	tg := m.tg

	return tea.Batch(
		func() tea.Msg { return OpenPhotoMsg{Caption: caption} },
		func() tea.Msg {
			if tg == nil {
				return OpenedPhotoMsg{Err: errNoClient}
			}
			file, err := tg.DownloadFileSync(key)
			if err != nil {
				return OpenedPhotoMsg{Err: err}
			}
			return OpenedPhotoMsg{Path: file.Path}
		},
	)
}

// overlayPicture is the file a message's picture is in and the line the
// overlay names it with, or reports that the message has no picture.
//
// Two kinds of message qualify, because two kinds of message are a picture.
// A photo is the obvious one. A DOCUMENT whose type is image/* is the one
// that used to fall through: a screenshot dragged into a chat, or anything
// sent with "send as file" to keep it from being recompressed, arrives as a
// document and is a picture all the same.
//
// The card already says so — an image-typed document gets the IMG badge and
// the ▣ mark rather than the DOC ones — and a badge that says "this draws
// in your terminal" over a file that opens in Preview is the kind of false
// fact in fixed-width type this design rejects everywhere else. So the
// badge is kept and enter made true, rather than the other way round.
func (m Model) overlayPicture(msg *telegram.Message) (key, caption string, ok bool) {
	switch c := msg.Content.(type) {
	case *telegram.MessagePhoto:
		if c.Photo == nil {
			return "", "", false
		}
		if key = fileKey(bestPhotoSizeFile(c.Photo)); key == "" {
			return "", "", false
		}
		return key, m.photoCaption(msg, c.Photo), true

	case *telegram.MessageDocument:
		if c.Document == nil || !strings.HasPrefix(c.Document.MimeType, "image/") {
			return "", "", false
		}
		if key = fileKey(c.Document.File); key == "" {
			return "", "", false
		}
		return key, m.documentPictureCaption(msg, c.Document), true
	}
	return "", "", false
}

// documentPictureCaption names a picture that arrived as a file: its own
// name, which a photo does not have, and who sent it.
//
// No dimensions, where photoCaption has them — a document does not carry
// them, and the overlay is about to draw the picture anyway.
func (m Model) documentPictureCaption(msg *telegram.Message, doc *telegram.Document) string {
	name, _ := m.senderFor(msg)
	if doc.FileName == "" {
		return strings.Join([]string{"image", name}, " · ")
	}
	return strings.Join([]string{doc.FileName, name}, " · ")
}

// photoCaption names the picture in the overlay's header: who sent it, when,
// and how big it is.
func (m Model) photoCaption(msg *telegram.Message, photo *telegram.Photo) string {
	name, _ := m.senderFor(msg)
	parts := []string{"photo", name}
	if sz := bestPhotoSize(photo); sz != nil && sz.Width > 0 && sz.Height > 0 {
		parts = append(parts, itoa(int(sz.Width))+"×"+itoa(int(sz.Height)))
	}
	return strings.Join(parts, " · ")
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// DownloadCmd and OpenExternallyCmd are the media overlay's two other keys,
// exported because while the overlay is up the host owns the keyboard and
// the chat view never sees the press. They are the same actions s and o
// perform in the thread, deliberately: the overlay's hint row advertises the
// same letters, and two spellings of "save this" would be two things to keep
// in agreement.
func (m Model) DownloadCmd() tea.Cmd { return m.downloadFile() }

func (m Model) OpenExternallyCmd() tea.Cmd { return m.playMedia() }
