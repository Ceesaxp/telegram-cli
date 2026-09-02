package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatlist"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatview"
	"github.com/imtaqin/telegram-cli/internal/ui/components/reactionpicker"
)

// reactModel is a sized client with one chat open and one message in it,
// carrying whatever reactions the caller asks for.
func reactModel(t *testing.T, reactions []*telegram.Reaction, pinned bool) Model {
	t.Helper()
	m := mainModel(t, PanelChatList)
	m.store.Chats.Set(&telegram.Chat{
		ID: 1, Title: "infra-oncall", Type: telegram.ChatTypeSupergroup, Order: 1,
	})
	m.store.Users.Set(&telegram.User{ID: 11, FirstName: "nadia"})
	m.store.Messages.Append(1, &telegram.Message{
		ID: 5, ChatID: 1, IsPinned: pinned,
		SenderID:  &telegram.MessageSenderUser{UserID: 11},
		Reactions: reactions,
		Content:   &telegram.MessageText{Text: &telegram.FormattedText{Text: "the offending query"}},
	})
	m.chatList.MarkLoadedForTest()

	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = sized.(Model)
	opened, _ := m.Update(chatlist.ChatSelectedMsg{ChatId: 1})
	m = opened.(Model)
	m.chatView.MarkLoadedForTest()
	return m
}

// TestPlusOpensTheReactionRow, over the message the cursor is on, without
// naming it a second time.
func TestPlusOpensTheReactionRow(t *testing.T) {
	m := reactModel(t, nil, false)

	acted, _ := m.Update(chatview.MessageActionMsg{Action: "react", ChatId: 1, MessageId: 5})
	m = acted.(Model)

	if !m.reactions.IsVisible() {
		t.Fatal("react did not open the picker")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, reactionpicker.Reactions[0]) {
		t.Fatalf("the row is not on screen:\n%s", view)
	}
	// It takes the hint bar's row rather than covering the thread, so the
	// message being reacted to is still there to look at.
	if !strings.Contains(view, "the offending query") {
		t.Fatalf("the picker covered the message it is asking about:\n%s", view)
	}
}

// TestThePickerOpensOnTheOneYouAlreadyLeft, so the press that opens it and
// the press that confirms take it back off.
func TestThePickerOpensOnTheOneYouAlreadyLeft(t *testing.T) {
	mine := reactionpicker.Reactions[3]
	m := reactModel(t, []*telegram.Reaction{
		{Emoji: reactionpicker.Reactions[0], Count: 2},
		{Emoji: mine, Count: 1, Chosen: true},
	}, false)

	acted, _ := m.Update(chatview.MessageActionMsg{Action: "react", ChatId: 1, MessageId: 5})
	m = acted.(Model)

	next, cmd := m.reactions.Update(decodeKey(t, "\r"))
	m.reactions = next
	if cmd == nil {
		t.Fatal("enter reported nothing")
	}
	chosen, ok := cmd().(reactionpicker.ChosenMsg)
	if !ok {
		t.Fatalf("got %T", cmd())
	}
	if chosen.Emoji != "" {
		t.Fatalf("emoji = %q, want empty — enter on your own reaction takes it off", chosen.Emoji)
	}
}

// TestTheReactionRowOwnsTheKeyboard. Twelve choices and two ways out; a key
// that fell through would act on the message the row is asking about.
func TestTheReactionRowOwnsTheKeyboard(t *testing.T) {
	m := reactModel(t, nil, false)
	acted, _ := m.Update(chatview.MessageActionMsg{Action: "react", ChatId: 1, MessageId: 5})
	m = acted.(Model)

	before := m.chatView.ChatId()
	// "q" quits from the chat list and the chat view. It must not here.
	typed := update(t, m, "q")
	if typed.chatView.ChatId() != before {
		t.Error("a key reached the thread while the picker was open")
	}
	if typed.reactions.IsVisible() {
		t.Error("q did not close the picker")
	}
}

// TestPinTogglesOnWhatTheMessageSays. A pin key that cannot tell says
// "pinned" to something already pinned, and the place to check is the rail —
// which may not even be open.
func TestPinTogglesOnWhatTheMessageSays(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		m := reactModel(t, nil, pinned)
		_, cmd := m.Update(chatview.MessageActionMsg{Action: "pin", ChatId: 1, MessageId: 5})
		if cmd == nil {
			t.Fatalf("pinned=%v: pin produced no command", pinned)
		}
		// The command reaches for a nil client, so only its decision is
		// checkable here — which is the half this test is about.
		if m.messagePinned(1, 5) != pinned {
			t.Errorf("pinned=%v: the app read the flag as %v", pinned, m.messagePinned(1, 5))
		}
	}
}

// TestMyReactionIsReadOffTheMessage, not remembered: a client keeping its
// own copy disagrees with the server the first time you react from a phone.
func TestMyReactionIsReadOffTheMessage(t *testing.T) {
	mine := reactionpicker.Reactions[5]
	m := reactModel(t, []*telegram.Reaction{
		{Emoji: reactionpicker.Reactions[0], Count: 9},
		{Emoji: mine, Count: 1, Chosen: true},
	}, false)

	if got := m.myReactionOn(1, 5); got != mine {
		t.Errorf("myReactionOn = %q, want %q", got, mine)
	}
	// Nobody's own reaction, and a message that is not there at all.
	plain := reactModel(t, []*telegram.Reaction{{Emoji: "👍", Count: 3}}, false)
	if got := plain.myReactionOn(1, 5); got != "" {
		t.Errorf("myReactionOn = %q, want empty", got)
	}
	if got := plain.myReactionOn(1, 999); got != "" {
		t.Errorf("a message that is not loaded reported %q", got)
	}
}

// TestTOnAChannelPostGoesToTheDiscussion — or tries to: the lookup is a
// round trip, so what is checkable here is that the key produces one.
func TestTOnAChannelPostGoesToTheDiscussion(t *testing.T) {
	m := reactModel(t, nil, false)
	m.store.Messages.Append(1, &telegram.Message{
		ID: 6, ChatID: 1, IsChannelPost: true,
		SenderID: &telegram.MessageSenderChat{ChatID: 1},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "shipped"}},
		Comments: &telegram.Comments{Count: 12, ChatID: -100777},
	})

	_, cmd := m.Update(chatview.MessageActionMsg{Action: "thread", ChatId: 1, MessageId: 6})
	if cmd == nil {
		t.Fatal("thread produced no command")
	}
}

// TestTheDiscussionOpensAtThePostsOwnCopy, not at the top of the linked
// group: the comments hang off that message, and the top of the group is not
// where the reader was going.
func TestTheDiscussionOpensAtThePostsOwnCopy(t *testing.T) {
	m := reactModel(t, nil, false)
	m.store.Chats.Set(&telegram.Chat{
		ID: -100777, Title: "infra-oncall chat", Type: telegram.ChatTypeSupergroup, Order: 2,
	})
	m.chatList.MarkLoadedForTest()
	_ = m.chatList.View()

	opened, _ := m.Update(openDiscussionMsg{ChatId: -100777, MessageId: 4412})
	m = opened.(Model)

	if got := m.chatView.ChatId(); got != -100777 {
		t.Fatalf("opened chat %d, want the linked group", got)
	}
	if got := ansi.Strip(m.chatView.View()); !strings.Contains(got, "infra-oncall chat") {
		t.Fatalf("the header does not name the group:\n%s", got)
	}
	// At the post's own copy. The comments hang off that message, and the
	// top of the group is not where the reader was going.
	if got := m.chatView.TargetMessageId(); got != 4412 {
		t.Fatalf("aimed at message %d, want the post's copy 4412", got)
	}
}
