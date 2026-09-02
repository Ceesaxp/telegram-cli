package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatlist"
	"github.com/imtaqin/telegram-cli/internal/ui/components/chatview"
)

// replyModel is a sized client with one chat open and one message in it.
func replyModel(t *testing.T) Model {
	t.Helper()
	m := mainModel(t, PanelChatList)
	m.store.Chats.Set(&telegram.Chat{
		ID: 1, Title: "infra-oncall", Type: telegram.ChatTypeSupergroup, Order: 1,
	})
	m.store.Users.Set(&telegram.User{ID: 11, FirstName: "nadia"})
	m.store.Messages.Append(1, &telegram.Message{
		ID: 5, ChatID: 1,
		SenderID: &telegram.MessageSenderUser{UserID: 11},
		Content:  &telegram.MessageText{Text: &telegram.FormattedText{Text: "the offending query"}},
	})
	m.chatList.MarkLoadedForTest()

	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = sized.(Model)
	opened, _ := m.Update(chatlist.ChatSelectedMsg{ChatId: 1})
	m = opened.(Model)
	m.chatView.MarkLoadedForTest()
	return m
}

// TestReplyOpensARowToTypeInto.
//
// r grew the composer to two rows — the reply bar and the line you type into
// — and nothing recomputed the layout, so the frame went on budgeting one
// row and drew the bar over the input. The line was there and off the
// bottom of its column. It came back on the next resize, which is why going
// out to $EDITOR and returning appeared to fix it.
func TestReplyOpensARowToTypeInto(t *testing.T) {
	m := replyModel(t)
	if m.layout.ComposerHeight != 1 {
		t.Fatalf("precondition: composer starts at %d rows", m.layout.ComposerHeight)
	}

	replied, _ := m.Update(chatview.MessageActionMsg{Action: "reply", ChatId: 1, MessageId: 5})
	m = replied.(Model)

	// Immediately, on the frame r produced — not after a keystroke, and
	// not after a resize.
	if m.layout.ComposerHeight != m.composer.Rows() {
		t.Fatalf("composer wants %d rows, the frame budgets %d",
			m.composer.Rows(), m.layout.ComposerHeight)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "reply ↳") {
		t.Fatalf("no reply bar:\n%s", view)
	}
	if !strings.Contains(view, "INSERT ›") {
		t.Fatalf("the reply bar is there and the row to type into is not:\n%s", view)
	}
}

// TestTypingIntoAReplyShows, which is what the reader is actually checking
// when they press a key and wonder whether it went anywhere.
func TestTypingIntoAReplyShows(t *testing.T) {
	m := replyModel(t)
	replied, _ := m.Update(chatview.MessageActionMsg{Action: "reply", ChatId: 1, MessageId: 5})
	m = update(t, replied.(Model), "h")

	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "INSERT › h") {
		t.Fatalf("typed h and the frame does not show it:\n%s", view)
	}
}

// TestTheFrameFollowsTheComposerBackDown. The row is given back when the
// reply is dropped, or the thread stays a row short for the rest of the
// session.
func TestTheFrameFollowsTheComposerBackDown(t *testing.T) {
	m := replyModel(t)
	thread := m.layout.ThreadHeight

	replied, _ := m.Update(chatview.MessageActionMsg{Action: "reply", ChatId: 1, MessageId: 5})
	m = replied.(Model)
	if m.layout.ThreadHeight >= thread {
		t.Fatalf("the reply bar cost the thread nothing: %d then %d",
			thread, m.layout.ThreadHeight)
	}

	m = update(t, m, "\x1b") // esc drops the reply
	if m.layout.ThreadHeight != thread {
		t.Fatalf("dropping the reply left the thread at %d, want %d back",
			m.layout.ThreadHeight, thread)
	}
	if m.layout.ComposerHeight != m.composer.Rows() {
		t.Fatalf("composer wants %d rows, the frame budgets %d",
			m.composer.Rows(), m.layout.ComposerHeight)
	}
}

// TestAnUnsizedClientIsLeftAlone. Before the first WindowSizeMsg there is
// no frame to reconcile with, and computing one from a zero terminal hands
// every panel a zero region.
func TestAnUnsizedClientIsLeftAlone(t *testing.T) {
	m := mainModel(t, PanelComposer)
	m.composer.SetSize(60, 1)
	m.composer.SetChatId(1)

	typed := update(t, m, "h")
	if view := typed.composer.View(); view == "" {
		t.Fatal("an unsized client had its composer resized to nothing")
	}
}
