package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
)

// The functions without mentions keep every caller outside this package
// working unchanged, so what they put on the wire must not move: the text
// and entities formatOutgoing makes, and nothing else.
func TestSendTextMessageSendsWhatItAlwaysSent(t *testing.T) {
	c, inv := mentionClient(t, true)

	if _, err := c.SendTextMessage(basicGroupID, "a **b** c", 0, 0); err != nil {
		t.Fatalf("SendTextMessage: %v", err)
	}

	if len(inv.sends) != 1 {
		t.Fatalf("sent %d messages, want 1", len(inv.sends))
	}
	got := inv.sends[0]
	if got.Message != "a b c" {
		t.Errorf("message = %q, want %q", got.Message, "a b c")
	}
	if want := []tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 2, Length: 1}}; !reflect.DeepEqual(got.Entities, want) {
		t.Errorf("entities = %s, want %s", describe(got.Entities), describe(want))
	}
	if got.ReplyTo != nil {
		t.Errorf("reply to = %#v, want none", got.ReplyTo)
	}
}

// The mention reaches the server as an inputMessageEntityMentionName with
// the user's InputUser, built from the stored hash: the only requests are
// resolving the chat, which every send makes, and the send itself.
func TestSendTextMessageWithMentionsPutsTheMentionOnTheWire(t *testing.T) {
	c, inv := storedMentionClient(t)

	_, dropped, err := c.SendTextMessageWithMentions(basicGroupID, "hi Nadia",
		[]MentionSpan{{Offset: 3, Length: 5, UserID: nadia}}, 0, 0)
	if err != nil {
		t.Fatalf("SendTextMessageWithMentions: %v", err)
	}

	if dropped != 0 {
		t.Errorf("dropped %d mentions, want none", dropped)
	}
	if len(inv.sends) != 1 {
		t.Fatalf("sent %d messages, want 1", len(inv.sends))
	}
	if got := inv.sends[0].Message; got != "hi Nadia" {
		t.Errorf("message = %q, want %q", got, "hi Nadia")
	}
	if got, want := inv.sends[0].Entities, []tg.MessageEntityClass{mentionOf(3, 5)}; !reflect.DeepEqual(got, want) {
		t.Errorf("entities = %s, want %s", describe(got), describe(want))
	}
	if want := []string{"messages.getChats", "messages.sendMessage"}; !reflect.DeepEqual(inv.asked, want) {
		t.Errorf("asked the server %v, want %v", inv.asked, want)
	}
}

// When the server answers with updateShortSentMessage, the sent message is
// rebuilt from what went on the wire. The server's own copy will carry the
// mention as a messageEntityMentionName, so the local one must too, or the
// thread shows an entity of no kind until that copy arrives.
func TestTheLocalCopyOfASentMentionNamesTheUser(t *testing.T) {
	c, _ := storedMentionClient(t)

	msg, _, err := c.SendTextMessageWithMentions(basicGroupID, "hi Nadia",
		[]MentionSpan{{Offset: 3, Length: 5, UserID: nadia}}, 0, 0)
	if err != nil {
		t.Fatalf("SendTextMessageWithMentions: %v", err)
	}

	content, ok := msg.Content.(*MessageText)
	if !ok {
		t.Fatalf("content = %T, want text", msg.Content)
	}
	want := []*TextEntity{{Offset: 3, Length: 5, Type: &TextEntityTypeMentionName{UserID: nadia}}}
	if got := content.Text.Entities; !reflect.DeepEqual(got, want) {
		t.Errorf("entities = %s, want %s", describeText(got), describeText(want))
	}
}

// Editing without mentions sends what it always sent, for the same reason.
func TestEditTextMessageSendsWhatItAlwaysSent(t *testing.T) {
	c, inv := mentionClient(t, true)

	if _, err := c.EditTextMessage(basicGroupID, 42, "a **b** c"); err != nil {
		t.Fatalf("EditTextMessage: %v", err)
	}

	if len(inv.edits) != 1 {
		t.Fatalf("sent %d edits, want 1", len(inv.edits))
	}
	got := inv.edits[0]
	if got.ID != 42 || got.Message != "a b c" {
		t.Errorf("edit = message %d to %q, want message 42 to %q", got.ID, got.Message, "a b c")
	}
	if want := []tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 2, Length: 1}}; !reflect.DeepEqual(got.Entities, want) {
		t.Errorf("entities = %s, want %s", describe(got.Entities), describe(want))
	}
}

// An edit replaces the message's entities wholesale, so a mention kept
// through edit mode has to be sent again with the new text or it is lost.
func TestEditTextMessageWithMentionsPutsTheMentionOnTheWire(t *testing.T) {
	c, inv := storedMentionClient(t)

	_, dropped, err := c.EditTextMessageWithMentions(basicGroupID, 42, "hi Nadia",
		[]MentionSpan{{Offset: 3, Length: 5, UserID: nadia}})
	if err != nil {
		t.Fatalf("EditTextMessageWithMentions: %v", err)
	}

	if dropped != 0 {
		t.Errorf("dropped %d mentions, want none", dropped)
	}
	if len(inv.edits) != 1 {
		t.Fatalf("sent %d edits, want 1", len(inv.edits))
	}
	if got, want := inv.edits[0].Entities, []tg.MessageEntityClass{mentionOf(3, 5)}; !reflect.DeepEqual(got, want) {
		t.Errorf("entities = %s, want %s", describe(got), describe(want))
	}
}

// uploaded is a small file on disk whose upload c already holds, the way
// an attachment is uploaded when it is attached: the send then goes
// straight to messages.sendMedia, which is the request under test.
func uploaded(t *testing.T, c *Client) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	c.uploads.start(path, func(context.Context, string, uint64) (tg.InputFileClass, error) {
		return uploadedFile(7), nil
	})
	return path
}

// captionSenders are the two ways an attachment goes out, without mentions.
var captionSenders = map[string]func(c *Client, path, caption string) error{
	"a file": func(c *Client, path, caption string) error {
		_, err := c.SendFileMessage(basicGroupID, path, caption, 0, 0)
		return err
	},
	"a photo": func(c *Client, path, caption string) error {
		_, err := c.SendPhotoMessage(basicGroupID, path, caption, 0, 0)
		return err
	},
}

// A caption without mentions goes out as it always did.
func TestACaptionIsSentAsItAlwaysWas(t *testing.T) {
	for name, send := range captionSenders {
		t.Run(name, func(t *testing.T) {
			c, inv := mentionClient(t, true)

			if err := send(c, uploaded(t, c), "a **b** c"); err != nil {
				t.Fatalf("send: %v", err)
			}

			if len(inv.media) != 1 {
				t.Fatalf("sent %d media, want 1", len(inv.media))
			}
			if got := inv.media[0].Message; got != "a b c" {
				t.Errorf("caption = %q, want %q", got, "a b c")
			}
			want := []tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 2, Length: 1}}
			if got := inv.media[0].Entities; !reflect.DeepEqual(got, want) {
				t.Errorf("entities = %s, want %s", describe(got), describe(want))
			}
		})
	}
}

// A caption is message text like any other, so a mention in it goes out
// the same way, for a file and for a photo alike.
func TestACaptionCarriesItsMentions(t *testing.T) {
	for name, send := range map[string]func(c *Client, path string, mentions []MentionSpan) (int, error){
		"a file": func(c *Client, path string, mentions []MentionSpan) (int, error) {
			_, dropped, err := c.SendFileMessageWithMentions(basicGroupID, path, "hi Nadia", mentions, 0, 0)
			return dropped, err
		},
		"a photo": func(c *Client, path string, mentions []MentionSpan) (int, error) {
			_, dropped, err := c.SendPhotoMessageWithMentions(basicGroupID, path, "hi Nadia", mentions, 0, 0)
			return dropped, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, inv := storedMentionClient(t)

			dropped, err := send(c, uploaded(t, c), []MentionSpan{{Offset: 3, Length: 5, UserID: nadia}})
			if err != nil {
				t.Fatalf("send: %v", err)
			}

			if dropped != 0 {
				t.Errorf("dropped %d mentions, want none", dropped)
			}
			if len(inv.media) != 1 {
				t.Fatalf("sent %d media, want 1", len(inv.media))
			}
			if got, want := inv.media[0].Entities, []tg.MessageEntityClass{mentionOf(3, 5)}; !reflect.DeepEqual(got, want) {
				t.Errorf("entities = %s, want %s", describe(got), describe(want))
			}
		})
	}
}

// describeText renders domain entities for a failure message, by value.
func describeText(entities []*TextEntity) string {
	parts := make([]string, 0, len(entities))
	for _, e := range entities {
		parts = append(parts, fmt.Sprintf("%d+%d %#v", e.Offset, e.Length, e.Type))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
