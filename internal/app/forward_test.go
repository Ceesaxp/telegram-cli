package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/forward"
)

// forwardModel is an app with a chat open and the destination picker
// raised on one message.
func forwardModel(t *testing.T) Model {
	t.Helper()
	m := openChatModel(t, PanelChatView)
	m.store.Chats.Set(&telegram.Chat{ID: 11, Type: telegram.ChatTypePrivate, Title: "Nadia", Username: "nadia"})
	m.store.Chats.Set(&telegram.Chat{ID: 12, Type: telegram.ChatTypeBasicGroup, Title: "Release team"})

	next, _ := m.Update(chatview.MessageActionMsg{Action: "forward", ChatId: testChatID, MessageId: 7})
	return next.(Model)
}

// 'f' on the cursored message asks for a forward. The binding was removed
// by the keymap cut for being inert (I-13); this is the test that it is
// not inert any more.
func TestForwardKeyDispatchesTheAction(t *testing.T) {
	m := openChatModel(t, PanelChatView)
	m.store.Messages.Activate(testChatID)
	m.store.Messages.Append(testChatID, &telegram.Message{ID: 7, ChatID: testChatID,
		Content: &telegram.MessageText{Text: &telegram.FormattedText{Text: "the patch landed"}}})
	m.chatView.MarkLoadedForTest()

	_, cmd := updateCmd(t, m, "f")
	act, ok := findMessageAction(cmd)
	if !ok {
		t.Fatal("'f' dispatched no message action")
	}
	if act.Action != "forward" || act.MessageId != 7 {
		t.Fatalf("action = %+v, want a forward of message 7", act)
	}
}

func TestForwardActionOpensThePicker(t *testing.T) {
	m := forwardModel(t)

	if !m.forward.IsVisible() {
		t.Fatal("the forward action did not open the picker")
	}
	if src := m.forward.Source(); src.ChatID != testChatID || src.MessageID != 7 {
		t.Fatalf("source = %+v, want the cursored message", src)
	}
	// The chats already loaded are the destinations, so the common case
	// needs no server round trip at all.
	if len(m.forward.Matches()) == 0 {
		t.Fatal("the picker opened on an empty destination list")
	}
}

// The picker owns the keyboard while it is up. A printable falling through
// to the thread underneath would act on the message the picker is asking
// about — and 'd' underneath is delete.
func TestPickerOwnsTheKeyboard(t *testing.T) {
	m := forwardModel(t)

	next, cmd := updateCmd(t, m, "d")
	if _, ok := findMessageAction(cmd); ok {
		t.Fatal("a keystroke reached the thread while the picker was open")
	}
	if next.forward.Query() != "d" {
		t.Fatalf("query = %q, want the keystroke to have gone into it", next.forward.Query())
	}
	if !next.keyboardOwnedByOverlay() {
		t.Error("keyboardOwnedByOverlay does not account for the picker")
	}
}

// A result for a query the reader has typed past must not repopulate the
// list. The generation is what makes that true rather than hoped for.
func TestStaleSearchResultsAreDropped(t *testing.T) {
	m := forwardModel(t)

	m, _ = updateCmd(t, m, "n")
	stale := m.forwardGen
	m, _ = updateCmd(t, m, "a") // the query moved on; stale is now old
	before := len(m.forward.Matches())

	next, _ := m.handleForwardResults(forwardResultsMsg{
		gen:   stale,
		chats: []*telegram.Chat{{ID: 99, Title: "Somebody Else"}},
	})
	if got := len(next.(Model).forward.Matches()); got != before {
		t.Fatalf("matches = %d, want the %d already on screen — a stale answer repopulated the list", got, before)
	}
}

func TestCurrentSearchResultsAreApplied(t *testing.T) {
	m := forwardModel(t)
	m, _ = updateCmd(t, m, "n")

	next, _ := m.handleForwardResults(forwardResultsMsg{
		gen:   m.forwardGen,
		chats: []*telegram.Chat{{ID: 99, Type: telegram.ChatTypePrivate, Title: "Nadia Support", Username: "nadia_support"}},
	})
	found := false
	for _, c := range next.(Model).forward.Matches() {
		if c.ID == 99 && c.Note == "not in your chats" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the server's match is not in the list: %+v", next.(Model).forward.Matches())
	}
}

// A search failure must not empty the list: the loaded chats are still
// destinations, and taking them away is the feature failing at the moment
// it is least able to explain itself.
func TestSearchFailureLeavesTheLoadedChats(t *testing.T) {
	m := forwardModel(t)
	m, _ = updateCmd(t, m, "n")
	before := len(m.forward.Matches())

	next, _ := m.handleForwardResults(forwardResultsMsg{gen: m.forwardGen, err: errors.New("network is down")})
	if got := len(next.(Model).forward.Matches()); got != before {
		t.Fatalf("matches = %d after a failed search, want the %d local ones kept", got, before)
	}
}

// Escape closes the picker without forwarding, and leaves nothing behind
// that a later reopen could inherit.
func TestEscapeClosesThePicker(t *testing.T) {
	m := forwardModel(t)

	next, _ := updateCmd(t, m, "\x1b")
	if next.forward.IsVisible() {
		t.Fatal("escape did not close the picker")
	}
	if next.forward.Source() != (forward.Source{}) {
		t.Errorf("the closed picker still holds a source: %+v", next.forward.Source())
	}
}

// The destination picker draws over the frame, so the frame must know it
// is there — otherwise the background around the card is the terminal's
// own rather than the app's.
func TestPickerCountsAsAnOverlay(t *testing.T) {
	m := forwardModel(t)
	if !m.overlayOpen() {
		t.Fatal("overlayOpen does not account for the destination picker")
	}
}

func TestForwardErrorsAreExplained(t *testing.T) {
	cases := map[string]string{
		"forward messages: rpc error: CHAT_FORWARDS_RESTRICTED (400)": "does not allow forwarding",
		"forward messages: rpc error: CHAT_WRITE_FORBIDDEN (403)":     "cannot post",
		"forward messages: rpc error: CHAT_ADMIN_REQUIRED (400)":      "only admins",
		"forward messages: rpc error: MESSAGE_ID_INVALID (400)":       "no longer be forwarded",
	}
	for input, want := range cases {
		if got := forwardErrorText(errors.New(input)); !strings.Contains(got, want) {
			t.Errorf("forwardErrorText(%q) = %q, want it to mention %q", input, got, want)
		}
	}
	// An error nobody anticipated is more useful verbatim than flattened
	// into "could not forward".
	unknown := "forward messages: rpc error: SOMETHING_NEW (400)"
	if got := forwardErrorText(errors.New(unknown)); got != unknown {
		t.Errorf("forwardErrorText passed through as %q, want %q", got, unknown)
	}
}
