package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/composer"
	"github.com/charmbracelet/x/ansi"
)

// fakeSender stands in for the client's send path and records every call.
// plain is how many mentions each call reports sending as plain text.
type fakeSender struct {
	sent  []sentDraft
	plain int
}

// sentDraft is one call to fakeSender: kind is text, edit, file or photo.
type sentDraft struct {
	kind      string
	chatID    int64
	messageID int64
	text      string
	path      string
	mentions  []telegram.MentionSpan
}

func (f *fakeSender) record(d sentDraft) (*telegram.Message, int, error) {
	f.sent = append(f.sent, d)
	return &telegram.Message{ID: 1, ChatID: d.chatID}, f.plain, nil
}

func (f *fakeSender) SendTextMessageWithMentions(chatID int64, text string, mentions []telegram.MentionSpan, _, _ int64) (*telegram.Message, int, error) {
	return f.record(sentDraft{kind: "text", chatID: chatID, text: text, mentions: mentions})
}

func (f *fakeSender) EditTextMessageWithMentions(chatID, messageID int64, text string, mentions []telegram.MentionSpan) (*telegram.Message, int, error) {
	return f.record(sentDraft{kind: "edit", chatID: chatID, messageID: messageID, text: text, mentions: mentions})
}

func (f *fakeSender) SendFileMessageWithMentions(chatID int64, path, caption string, mentions []telegram.MentionSpan, _, _ int64) (*telegram.Message, int, error) {
	return f.record(sentDraft{kind: "file", chatID: chatID, text: caption, path: path, mentions: mentions})
}

func (f *fakeSender) SendPhotoMessageWithMentions(chatID int64, path, caption string, mentions []telegram.MentionSpan, _, _ int64) (*telegram.Message, int, error) {
	return f.record(sentDraft{kind: "photo", chatID: chatID, text: caption, path: path, mentions: mentions})
}

// The composer counts a mention in runes of the draft, and so does the send
// path: the conversion is a change of shape, not of unit. A label of several
// runes, some of them wider than a byte and one outside the BMP, is where a
// byte or UTF-16 count would show.
func TestMentionSpansKeepTheirRunes(t *testing.T) {
	label := "Ёжик 🦔"
	text := "hi " + label + " ok"
	start := len([]rune("hi "))
	spans := []composer.MentionSpan{
		{Start: start, End: start + len([]rune(label)), UserID: 8, Label: label},
		{Start: 0, End: 2, UserID: 9, Label: "hi"},
	}

	got := mentionSpans(spans)
	want := []telegram.MentionSpan{
		{Offset: 3, Length: 6, UserID: 8},
		{Offset: 0, Length: 2, UserID: 9},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mentionSpans(%q) = %+v, want %+v", text, got, want)
	}
	if got := mentionSpans(nil); got != nil {
		t.Fatalf("no spans converted to %+v, want nil", got)
	}
}

// submitsOfEveryKind are one draft of each kind a submit can be, all
// carrying the same text.
func submitsOfEveryKind(mentions []composer.MentionSpan) map[string]composer.MessageSubmittedMsg {
	base := composer.MessageSubmittedMsg{ChatId: basicGroupID, Text: "ask Ivo", Mentions: mentions}
	edit, file, photo := base, base, base
	edit.EditMessageId = 77
	file.Attachment = "/tmp/spool/notes.txt"
	photo.Attachment, photo.AsPhoto = "/tmp/spool/fix.png", true
	return map[string]composer.MessageSubmittedMsg{
		"text": base, "edit": edit, "file": file, "photo": photo,
	}
}

// submit hands msg to the app, runs the send it starts, and returns the
// model and whatever the send reported back.
func submit(t *testing.T, sender *fakeSender, msg composer.MessageSubmittedMsg) (Model, []any) {
	t.Helper()
	m := sizedMainModel(t)
	m.sends = sender
	next, cmd := m.Update(msg)
	var out []any
	for _, reply := range flattenCmd(cmd) {
		if reply != nil {
			out = append(out, reply)
		}
	}
	return next.(Model), out
}

// A mention picked for a member without a username reaches the wire from
// every kind of send: a message, an edit, a file's caption, a photo's.
func TestEverySendCarriesTheDraftsMentions(t *testing.T) {
	spans := []composer.MentionSpan{{Start: 4, End: 7, UserID: 8, Label: "Ivo"}}
	want := []telegram.MentionSpan{{Offset: 4, Length: 3, UserID: 8}}

	for kind, msg := range submitsOfEveryKind(spans) {
		t.Run(kind, func(t *testing.T) {
			sender := &fakeSender{}
			_, replies := submit(t, sender, msg)
			if len(sender.sent) != 1 {
				t.Fatalf("sent %+v, want one %s", sender.sent, kind)
			}
			got := sender.sent[0]
			if got.kind != kind || got.text != "ask Ivo" {
				t.Fatalf("sent %+v, want a %s of the draft", got, kind)
			}
			if !reflect.DeepEqual(got.mentions, want) {
				t.Fatalf("mentions = %+v, want %+v", got.mentions, want)
			}
			if len(replies) != 0 {
				t.Fatalf("a send with every mention delivered reported %+v", replies)
			}
		})
	}
}

// A draft with no mentions sends as it always has: no spans, and nothing
// said afterwards.
func TestASendWithoutMentionsIsUnchanged(t *testing.T) {
	for kind, msg := range submitsOfEveryKind(nil) {
		t.Run(kind, func(t *testing.T) {
			sender := &fakeSender{}
			_, replies := submit(t, sender, msg)
			if len(sender.sent) != 1 || sender.sent[0].kind != kind {
				t.Fatalf("sent %+v, want one %s", sender.sent, kind)
			}
			if sender.sent[0].mentions != nil {
				t.Fatalf("mentions = %+v, want none", sender.sent[0].mentions)
			}
			if len(replies) != 0 {
				t.Fatalf("reported %+v, want nothing", replies)
			}
		})
	}
}

// A mention the send path could not resolve still goes out, as the name
// alone. That is not a failure — the message is sent — but the reader
// picked somebody, and has to hear that the pick did not stick.
func TestAMentionSentAsPlainTextIsReported(t *testing.T) {
	spans := []composer.MentionSpan{{Start: 4, End: 7, UserID: 8, Label: "Ivo"}}
	for kind, msg := range submitsOfEveryKind(spans) {
		t.Run(kind, func(t *testing.T) {
			m, replies := submit(t, &fakeSender{plain: 1}, msg)
			if len(replies) != 1 {
				t.Fatalf("reported %+v, want the plain mention", replies)
			}
			m = send(t, m, replies[0])
			if bar := ansi.Strip(m.hintBar.View()); !strings.Contains(bar, "1 mention sent as plain text") {
				t.Fatalf("hint bar = %q, want the plain mention reported", bar)
			}
		})
	}
}

// And more than one is counted as more than one.
func TestMentionsSentAsPlainTextAreCounted(t *testing.T) {
	m := sizedMainModel(t)
	m = send(t, m, mentionsSentPlainMsg(2))
	if bar := ansi.Strip(m.hintBar.View()); !strings.Contains(bar, "2 mentions sent as plain text") {
		t.Fatalf("hint bar = %q, want both counted", bar)
	}
}
