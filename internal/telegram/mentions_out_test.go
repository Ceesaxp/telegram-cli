package telegram

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// nadia is a group member without a username: the case that needs an
// explicit mention entity, because there is no @name for the server to find.
const nadia int64 = 7

// nadiasHash is the access hash the server hands out for nadia. An InputUser
// is only good with it, so a mention carrying any other hash is useless.
const nadiasHash int64 = 70

// sendInvoker stands in for the server: it knows the basic group 5 and the
// user nadia, knows no other user, and records every send and edit it is
// asked for. asked names every request, in order, by its TL name.
type sendInvoker struct {
	asked []string
	sends []*tg.MessagesSendMessageRequest
	edits []*tg.MessagesEditMessageRequest
	media []*tg.MessagesSendMediaRequest
}

func (f *sendInvoker) Invoke(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
	if named, ok := input.(interface{ TypeName() string }); ok {
		f.asked = append(f.asked, named.TypeName())
	}
	switch req := input.(type) {
	case *tg.UsersGetUsersRequest:
		if u, ok := req.ID[0].(*tg.InputUser); ok && u.UserID == nadia {
			output.(*tg.UserClassVector).Elems = []tg.UserClass{
				&tg.User{ID: nadia, AccessHash: nadiasHash, FirstName: "Nadia"},
			}
			return nil
		}
		return tgerr.New(400, "USER_ID_INVALID")
	case *tg.MessagesSendMessageRequest:
		f.sends = append(f.sends, req)
		output.(*tg.UpdatesBox).Updates = &tg.UpdateShortSentMessage{ID: 100, Date: 1}
		return nil
	case *tg.MessagesEditMessageRequest:
		f.edits = append(f.edits, req)
		edited := &tg.Message{ID: req.ID, PeerID: &tg.PeerChat{ChatID: 5}, Out: true, Message: req.Message}
		output.(*tg.UpdatesBox).Updates = &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateEditMessage{Message: edited}}}
		return nil
	case *tg.MessagesSendMediaRequest:
		f.media = append(f.media, req)
		sent := &tg.Message{ID: 101, PeerID: &tg.PeerChat{ChatID: 5}, Out: true, Message: req.Message}
		output.(*tg.UpdatesBox).Updates = &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewMessage{Message: sent}}}
		return nil
	default:
		return readHistoryInvoker{}.Invoke(ctx, input, output)
	}
}

// mentionClient is a client talking to a sendInvoker, with outgoing
// Markdown on or off.
func mentionClient(t *testing.T, markdown bool) (*Client, *sendInvoker) {
	t.Helper()
	inv := &sendInvoker{}
	api := tg.NewClient(inv)
	cfg := &config.Config{}
	cfg.UI.ParseMarkdown = markdown
	c := &Client{api: api, peers: peers.Options{}.Build(api), config: cfg, files: newFileRegistry()}
	c.setMsgSink(func(tea.Msg) {})
	return c, inv
}

// storedMentionClient is mentionClient with the persistent peer cache the
// real client runs on, holding nadia's access hash the way the member
// search leaves it: applied through the peers manager.
func storedMentionClient(t *testing.T) (*Client, *sendInvoker) {
	t.Helper()
	c, inv := mentionClient(t, false)
	path := filepath.Join(t.TempDir(), "state.db")
	stores, err := openStateStores(path, path)
	if err != nil {
		t.Fatalf("openStateStores: %v", err)
	}
	t.Cleanup(func() { stores.Close() })
	c.stores = stores
	c.peers = peers.Options{Storage: stores.peerStorage()}.Build(c.api)

	seen := []tg.UserClass{&tg.User{ID: nadia, AccessHash: nadiasHash, FirstName: "Nadia"}}
	if err := c.peers.Apply(context.Background(), seen, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return c, inv
}

// mentionOf is the entity a resolved mention of nadia becomes on the wire.
func mentionOf(offset, length int) tg.MessageEntityClass {
	return &tg.InputMessageEntityMentionName{
		Offset: offset,
		Length: length,
		UserID: &tg.InputUser{UserID: nadia, AccessHash: nadiasHash},
	}
}

// describe renders entities for a failure message: pointers say nothing,
// and what a reader needs is each entity's kind, range and user.
func describe(entities []tg.MessageEntityClass) string {
	parts := make([]string, 0, len(entities))
	for _, e := range entities {
		part := fmt.Sprintf("%s %d+%d", e.TypeName(), e.GetOffset(), e.GetLength())
		if m, ok := e.(*tg.InputMessageEntityMentionName); ok {
			part += fmt.Sprintf(" of %#v", m.UserID)
		}
		parts = append(parts, part)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// The composer counts runes; the wire counts UTF-16 units. They part ways
// at every character outside the Basic Multilingual Plane, so a mention
// after one has to move by two units per such character, not one.
func TestAMentionLandsOnItsLabelInUTF16(t *testing.T) {
	for name, tt := range map[string]struct {
		text   string
		span   MentionSpan
		wantAt tg.MessageEntityClass
	}{
		"after an emoji": {
			text:   "\U0001F600 Nadia hi",
			span:   MentionSpan{Offset: 2, Length: 5, UserID: nadia},
			wantAt: mentionOf(3, 5),
		},
		// A combining mark is a rune of its own but a single BMP unit, so
		// it counts once: offsets are in code units, never in graphemes.
		"around combining marks": {
			text:   "Zoe\U00000301 Noe\U00000308l",
			span:   MentionSpan{Offset: 5, Length: 5, UserID: nadia},
			wantAt: mentionOf(5, 5),
		},
		"with a non-BMP character inside the label": {
			text:   "hi \U0001D4DDadia!",
			span:   MentionSpan{Offset: 3, Length: 5, UserID: nadia},
			wantAt: mentionOf(3, 6),
		},
		// Everyday CJK is BMP and one unit a rune; Extension B is not.
		"in CJK text": {
			text:   "\U00020000你好 娜迪亚 ok",
			span:   MentionSpan{Offset: 4, Length: 3, UserID: nadia},
			wantAt: mentionOf(5, 3),
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := mentionClient(t, false)

			text, entities, dropped := c.formatOutgoingWithMentions(context.Background(), tt.text, []MentionSpan{tt.span})

			if text != tt.text {
				t.Errorf("text = %q, want it sent as typed", text)
			}
			if dropped != 0 {
				t.Errorf("dropped %d mentions, want none", dropped)
			}
			if want := []tg.MessageEntityClass{tt.wantAt}; !reflect.DeepEqual(entities, want) {
				t.Errorf("entities = %s, want %s", describe(entities), describe(want))
			}
		})
	}
}

// wantFormatted checks what formatOutgoingWithMentions makes of text and
// spans: the text to send, every entity in wire order, and the drop count.
func wantFormatted(t *testing.T, c *Client, text string, spans []MentionSpan,
	wantText string, wantEntities []tg.MessageEntityClass, wantDropped int) {
	t.Helper()
	gotText, entities, dropped := c.formatOutgoingWithMentions(context.Background(), text, spans)
	if gotText != wantText {
		t.Errorf("text = %q, want %q", gotText, wantText)
	}
	if !reflect.DeepEqual(entities, wantEntities) {
		t.Errorf("entities = %s, want %s", describe(entities), describe(wantEntities))
	}
	if dropped != wantDropped {
		t.Errorf("dropped %d mentions, want %d", dropped, wantDropped)
	}
}

// Markdown removes its markers before the text goes out, so a mention after
// a bold run sits four units earlier on the wire than in the draft. The
// bold run and the mention are both sent, side by side.
func TestAMentionAfterMarkdownMovesWithTheRemovedMarkers(t *testing.T) {
	c, _ := mentionClient(t, true)

	wantFormatted(t, c, "**bold** @Nadia rest",
		[]MentionSpan{{Offset: 10, Length: 5, UserID: nadia}},
		"bold @Nadia rest",
		[]tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 0, Length: 4}, mentionOf(6, 5)},
		0)
}

// Telegram lets a mention sit inside bold: bold is formatting that splits
// around anything, so both entities go out, the mention nested in the run.
// The span ends where the closing marker starts, which must map to the end
// of the label, not past it.
func TestAMentionInsideBoldKeepsBoth(t *testing.T) {
	c, _ := mentionClient(t, true)

	wantFormatted(t, c, "**hi Nadia**",
		[]MentionSpan{{Offset: 5, Length: 5, UserID: nadia}},
		"hi Nadia",
		[]tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 0, Length: 8}, mentionOf(3, 5)},
		0)
}

// Entities go out sorted by offset, as the Markdown parser already emits
// its own, so a mention ahead of the formatting has to be merged in ahead
// of it rather than tacked on after.
func TestEntitiesGoOutInOffsetOrder(t *testing.T) {
	c, _ := mentionClient(t, true)

	wantFormatted(t, c, "Nadia, **look**",
		[]MentionSpan{{Offset: 0, Length: 5, UserID: nadia}},
		"Nadia, look",
		[]tg.MessageEntityClass{mentionOf(0, 5), &tg.MessageEntityBold{Offset: 7, Length: 4}},
		0)
}

// A span that straddles a marker has lost its shape: what it covers on the
// wire is no longer what it covered in the draft. Guessing a range for the
// user would risk naming them on the wrong text, so the mention is dropped
// and the text goes out without it.
func TestAMentionStraddlingAMarkerIsDropped(t *testing.T) {
	c, _ := mentionClient(t, true)

	wantFormatted(t, c, "**Nad**ia",
		[]MentionSpan{{Offset: 2, Length: 7, UserID: nadia}},
		"Nadia",
		[]tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 0, Length: 3}},
		1)
}

// Code is literal: a name inside it is text about a user, not a mention of
// one, and Telegram would not render it as a link there anyway. The code
// goes out; the mention does not.
func TestAMentionInsideCodeIsDropped(t *testing.T) {
	for name, tt := range map[string]struct {
		text     string
		span     MentionSpan
		wantText string
		wantCode tg.MessageEntityClass
	}{
		"inline code": {
			text:     "`hi Nadia`",
			span:     MentionSpan{Offset: 4, Length: 5, UserID: nadia},
			wantText: "hi Nadia",
			wantCode: &tg.MessageEntityCode{Offset: 0, Length: 8},
		},
		"a code fence": {
			text:     "```\nhi Nadia\n```",
			span:     MentionSpan{Offset: 7, Length: 5, UserID: nadia},
			wantText: "hi Nadia\n",
			wantCode: &tg.MessageEntityPre{Offset: 0, Length: 9},
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := mentionClient(t, true)

			wantFormatted(t, c, tt.text, []MentionSpan{tt.span},
				tt.wantText, []tg.MessageEntityClass{tt.wantCode}, 1)
		})
	}
}

// A mention is only as good as the InputUser behind it, and that needs the
// access hash the peers manager holds. A user it cannot resolve cannot be
// named: that mention is dropped, and the message still goes out with the
// others intact.
func TestAMentionOfAnUnresolvableUserIsDropped(t *testing.T) {
	c, _ := mentionClient(t, false)
	const stranger int64 = 99

	wantFormatted(t, c, "Nadia and Bob",
		[]MentionSpan{{Offset: 0, Length: 5, UserID: nadia}, {Offset: 10, Length: 3, UserID: stranger}},
		"Nadia and Bob",
		[]tg.MessageEntityClass{mentionOf(0, 5)},
		1)
}

// The peers manager keeps no user objects, only their access hashes, so
// resolving a user through it is a users.getUsers every time: one request
// per mention per send. The member search that offered the user already
// stored the hash, and the hash is all an InputUser needs.
func TestAMentionOfAStoredUserAsksTheServerNothing(t *testing.T) {
	c, inv := storedMentionClient(t)

	wantFormatted(t, c, "hi Nadia",
		[]MentionSpan{{Offset: 3, Length: 5, UserID: nadia}},
		"hi Nadia",
		[]tg.MessageEntityClass{mentionOf(3, 5)},
		0)
	if len(inv.asked) != 0 {
		t.Errorf("formatting a mention of a stored user asked the server %v, want nothing", inv.asked)
	}
}

// A user the cache has no hash for is still worth one lookup before the
// mention is given up on.
func TestAMentionOfAnUnstoredUserIsLookedUpBeforeItIsDropped(t *testing.T) {
	c, inv := storedMentionClient(t)
	const stranger int64 = 99

	wantFormatted(t, c, "hi Bob",
		[]MentionSpan{{Offset: 3, Length: 3, UserID: stranger}},
		"hi Bob", nil, 1)
	if want := []string{"users.getUsers"}; !reflect.DeepEqual(inv.asked, want) {
		t.Errorf("asked the server %v, want %v", inv.asked, want)
	}
}

// A span that is empty or does not fit the text belongs to some other
// draft. It is dropped, never clamped: a clamped range would name the user
// on text they were not picked for.
func TestAMentionOutsideTheTextOrEmptyIsDropped(t *testing.T) {
	for name, span := range map[string]MentionSpan{
		"zero length":          {Offset: 3, Length: 0},
		"negative length":      {Offset: 3, Length: -1},
		"negative offset":      {Offset: -1, Length: 3},
		"running past the end": {Offset: 3, Length: 6},
		"starting at the end":  {Offset: 8, Length: 1},
		"overflowing":          {Offset: 3, Length: math.MaxInt},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := mentionClient(t, false)
			span.UserID = nadia

			wantFormatted(t, c, "hi Nadia", []MentionSpan{span}, "hi Nadia", nil, 1)
		})
	}
}

// Two mentions over the same text cannot both be right, and Telegram does
// not let mention entities overlap. The earlier one in the text keeps its
// place, whatever order the spans came in; the other is dropped.
func TestOverlappingMentionsKeepTheFirst(t *testing.T) {
	c, _ := mentionClient(t, false)

	wantFormatted(t, c, "Nadia Petrova",
		[]MentionSpan{{Offset: 6, Length: 7, UserID: nadia}, {Offset: 0, Length: 13, UserID: nadia}},
		"Nadia Petrova",
		[]tg.MessageEntityClass{mentionOf(0, 13)},
		1)
}
