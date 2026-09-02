package chatview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// headerModel is a thread panel with a chat open and nothing else.
func headerModel(t *testing.T, kind telegram.ChatType) Model {
	t.Helper()
	m := newTestModel()
	m.SetSize(100, 20)
	m.store.Chats.Set(&telegram.Chat{ID: testChatID, Type: kind, Title: "infra-oncall"})
	m.chatTitle = "infra-oncall"
	return m
}

// TestTheHeaderSaysHowManyPeopleAreInTheChat, once somebody has asked.
func TestTheHeaderSaysHowManyPeopleAreInTheChat(t *testing.T) {
	m := headerModel(t, telegram.ChatTypeSupergroup)

	if got := ansi.Strip(m.renderHeader()); !strings.Contains(got, "group") ||
		strings.Contains(got, "member") {
		t.Fatalf("a chat nobody has counted said %q", got)
	}

	m.store.Chats.SetMemberCount(testChatID, 24)
	if got := ansi.Strip(m.renderHeader()); !strings.Contains(got, "group · 24 members") {
		t.Fatalf("got %q, want the member count", got)
	}

	// One member is one member. A renderer inventing plurals for a number
	// it was handed is a renderer that will get some other number wrong.
	m.store.Chats.SetMemberCount(testChatID, 1)
	if got := ansi.Strip(m.renderHeader()); !strings.Contains(got, "group · 1 member ") {
		t.Fatalf("got %q, want the singular", got)
	}
}

// TestOnlyAChatWithMembersIsCounted: "2 members" under somebody's name is
// not a fact anyone needed, so the question is never asked for a private
// chat — which is also why the subtitle does not have to re-check the type.
func TestOnlyAChatWithMembersIsCounted(t *testing.T) {
	private := headerModel(t, telegram.ChatTypePrivate)
	if cmd := private.memberCountCmd(testChatID); cmd != nil {
		t.Error("a private chat was asked how many members it has")
	}

	group := headerModel(t, telegram.ChatTypeSupergroup)
	if cmd := group.memberCountCmd(testChatID); cmd == nil {
		t.Error("a group was not asked how many members it has")
	}

	// And it is asked once. A membership does not change often enough to
	// be worth a request on every open, and the number is on screen anyway.
	group.store.Chats.SetMemberCount(testChatID, 24)
	if cmd := group.memberCountCmd(testChatID); cmd != nil {
		t.Error("a group whose count is already known was asked again")
	}

	if got := group.chatKindSubtitle(""); got != "" {
		t.Fatalf("a chat with no kind said %q", got)
	}
}

// TestTheBufferNumberIsToldNotGuessed. The panel cannot work out which row
// of the chat list it is, so it says nothing until the app says.
func TestTheBufferNumberIsToldNotGuessed(t *testing.T) {
	m := headerModel(t, telegram.ChatTypeSupergroup)

	if got := ansi.Strip(m.renderHeader()); strings.Contains(got, "buf") {
		t.Fatalf("got %q, want no buffer number", got)
	}
	m.SetBufferIndex(2)
	if got := ansi.Strip(m.renderHeader()); !strings.Contains(got, "buf 2 │ ln") {
		t.Fatalf("got %q, want the buffer number before the position", got)
	}
	m.SetBufferIndex(0)
	if got := ansi.Strip(m.renderHeader()); strings.Contains(got, "buf") {
		t.Fatalf("got %q, want it cleared", got)
	}
}

// TestTheScrollMarkerNamesOnlyTheEnds. "ln 214/214" already says where the
// reader is exactly; the marker answers the question they were actually
// asking, which is whether there is more below.
func TestTheScrollMarkerNamesOnlyTheEnds(t *testing.T) {
	m := headerModel(t, telegram.ChatTypeSupergroup)
	if got := m.scrollMarker(); got != "" {
		t.Errorf("an empty thread said %q", got)
	}

	// One short message: the whole history is on screen at once.
	m.store.Messages.Append(testChatID, textMessage(1, 1, "hello"))
	m.cache.clear()
	if got := m.scrollMarker(); got != "all" {
		t.Errorf("a history that fits said %q, want all", got)
	}

	// A history taller than the pane, read from the bottom and then the top.
	for i := range 60 {
		m.store.Messages.Append(testChatID, textMessage(int64(10+i), 1, "line"))
	}
	m.cache.clear()
	if got := m.scrollMarker(); got != "bot" {
		t.Errorf("at the newest message the marker said %q, want bot", got)
	}

	m.scrollOffset = m.maxScrollOffset()
	if got := m.scrollMarker(); got != "top" {
		t.Errorf("at the oldest message the marker said %q, want top", got)
	}

	m.scrollOffset = 5
	if got := m.scrollMarker(); got != "" {
		t.Errorf("in the middle the marker said %q, want nothing", got)
	}
}

// TestTheScrollMarkerIsInTheRightGroup, which the header measures first so
// that widening it cannot cost the position cell.
func TestTheScrollMarkerIsInTheRightGroup(t *testing.T) {
	m := headerModel(t, telegram.ChatTypeSupergroup)
	m.store.Messages.Append(testChatID, textMessage(1, 1, "hello"))
	m.cache.clear()

	got := ansi.Strip(m.renderHeader())
	if !strings.Contains(got, "  all") || !strings.Contains(got, "ln ") ||
		strings.Index(got, "ln ") > strings.Index(got, "  all") {
		t.Fatalf("got %q, want the marker after the position", got)
	}
	if !strings.HasSuffix(strings.TrimRight(got, " "), "all") {
		t.Fatalf("got %q, want the right group last", got)
	}
}

// TestAPictureSentAsAFileOpensInTheTerminal.
//
// The card gives an image-typed document the IMG badge and the ▣ mark, on
// the stated grounds that the badge says whether enter draws it here or
// hands it to your system. That was only half true: the overlay took a
// MessagePhoto and nothing else, so enter on a screenshot sent with "send
// as file" fell through and opened Preview.
func TestAPictureSentAsAFileOpensInTheTerminal(t *testing.T) {
	m := newTestModel()

	cases := map[string]struct {
		content telegram.MessageContent
		want    bool
	}{
		"a photo": {&telegram.MessagePhoto{Photo: &telegram.Photo{
			ID: 1, Sizes: []*telegram.PhotoSize{{Type: "y", Width: 8, Height: 8,
				File: &telegram.File{ID: "photo:1:y"}}},
		}}, true},
		"a png sent as a file": {&telegram.MessageDocument{Document: &telegram.Document{
			FileName: "auth-p95-2608.png", MimeType: "image/png",
			File: &telegram.File{ID: "doc:1"},
		}}, true},
		"a patch": {&telegram.MessageDocument{Document: &telegram.Document{
			FileName: "backoff.patch", MimeType: "text/x-patch",
			File: &telegram.File{ID: "doc:2"},
		}}, false},
		"a voice note": {&telegram.MessageVoiceNote{
			VoiceNote: &telegram.VoiceNote{File: &telegram.File{ID: "v"}},
		}, false},
		"an image with no file behind it": {&telegram.MessageDocument{Document: &telegram.Document{
			FileName: "gone.png", MimeType: "image/png",
		}}, false},
	}

	for name, tc := range cases {
		msg := &telegram.Message{
			ID: 1, ChatID: testChatID,
			SenderID: &telegram.MessageSenderUser{UserID: 11},
			Content:  tc.content,
		}
		_, caption, ok := m.overlayPicture(msg)
		if ok != tc.want {
			t.Errorf("%s: overlayPicture ok = %v, want %v", name, ok, tc.want)
			continue
		}
		if ok && caption == "" {
			t.Errorf("%s: the overlay would open with no caption", name)
		}
	}
}

// TestAFilesPictureIsNamedByItsFilename, which is the thing a photo does
// not have and the reason it was sent as a file in the first place.
func TestAFilesPictureIsNamedByItsFilename(t *testing.T) {
	m := newTestModel()
	msg := &telegram.Message{
		ID: 1, ChatID: testChatID,
		SenderID: &telegram.MessageSenderUser{UserID: 11},
		Content: &telegram.MessageDocument{Document: &telegram.Document{
			FileName: "auth-p95-2608.png", MimeType: "image/png",
			File: &telegram.File{ID: "doc:1"},
		}},
	}

	_, caption, ok := m.overlayPicture(msg)
	if !ok {
		t.Fatal("no picture")
	}
	if !strings.Contains(caption, "auth-p95-2608.png") {
		t.Fatalf("caption = %q, want the filename in it", caption)
	}
}

// TestTOnAMessageWithNoDiscussionSaysSo. A key that does nothing on nine
// messages out of ten teaches people it is broken, so it says why instead.
func TestTOnAMessageWithNoDiscussionSaysSo(t *testing.T) {
	m := headerModel(t, telegram.ChatTypeChannel)
	m.SetFocused(true)
	m.store.Messages.Append(testChatID, textMessage(1, 11, "shipped"))
	m.cache.clear()

	_, cmd := m.handleKey(key('t'))
	if cmd == nil {
		t.Fatal("t produced nothing at all")
	}
	notice, ok := cmd().(MediaPlayMsg)
	if !ok {
		t.Fatalf("got %T, want a notice", cmd())
	}
	if !strings.Contains(notice.Info, "no discussion") {
		t.Errorf("notice = %q", notice.Info)
	}
}

// TestTOnAChannelPostAsksForItsDiscussion.
func TestTOnAChannelPostAsksForItsDiscussion(t *testing.T) {
	m := headerModel(t, telegram.ChatTypeChannel)
	m.SetFocused(true)

	post := textMessage(1, 11, "shipped")
	post.IsChannelPost = true
	post.Comments = &telegram.Comments{Count: 12, ChatID: -100777}
	m.store.Messages.Append(testChatID, post)
	m.cache.clear()

	_, cmd := m.handleKey(key('t'))
	if cmd == nil {
		t.Fatal("t produced nothing")
	}
	action, ok := cmd().(MessageActionMsg)
	if !ok {
		t.Fatalf("got %T, want a MessageActionMsg", cmd())
	}
	if action.Action != "thread" || action.MessageId != 1 {
		t.Errorf("got %+v", action)
	}
}

// TestADiscussionWithNowhereToGoIsRefused. Telegram sometimes reports a
// discussion without naming the group it is in; the row says so and the key
// declines rather than opening chat zero.
func TestADiscussionWithNowhereToGoIsRefused(t *testing.T) {
	m := headerModel(t, telegram.ChatTypeChannel)
	m.SetFocused(true)

	post := textMessage(1, 11, "shipped")
	post.Comments = &telegram.Comments{Count: 3}
	m.store.Messages.Append(testChatID, post)
	m.cache.clear()

	_, cmd := m.handleKey(key('t'))
	if _, ok := cmd().(MediaPlayMsg); !ok {
		t.Fatalf("got %T, want the refusal", cmd())
	}
}
