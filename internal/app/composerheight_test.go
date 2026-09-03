package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
	"github.com/charmbracelet/x/ansi"
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

// TestAReplyHasSomewhereToTypeOnAShortTerminal.
//
// Below twenty rows the layout refuses the composer its expanded form, and
// used to refuse it every row but one — so a reply drew its quote bar into
// the single row budgeted and the frame clipped the prompt underneath it.
// The original defect, surviving at the sizes it was least visible at.
func TestAReplyHasSomewhereToTypeOnAShortTerminal(t *testing.T) {
	for _, height := range []int{12, 14, 16, 19, 20, 24, 40} {
		m := replyModel(t)
		sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
		m = sized.(Model)

		replied, _ := m.Update(chatview.MessageActionMsg{Action: "reply", ChatId: 1, MessageId: 5})
		m = replied.(Model)

		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "INSERT ›") {
			t.Errorf("at %d rows there is nowhere to type:\n%s", height, view)
		}
		if !strings.Contains(view, "reply ↳") {
			t.Errorf("at %d rows the quote is gone:\n%s", height, view)
		}
	}
}

// TestThePromptOutlivesTheQuote. When there really is only one row, the
// context gives way and the prompt stays: a composer showing what you are
// replying to and no way to reply is worse than one that shows only the
// reply.
func TestThePromptOutlivesTheQuote(t *testing.T) {
	m := replyModel(t)
	// Eleven rows: below MinTopBarHeight, so the frame drops its chrome and
	// the body has almost nothing to give.
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 5})
	m = sized.(Model)
	replied, _ := m.Update(chatview.MessageActionMsg{Action: "reply", ChatId: 1, MessageId: 5})
	m = replied.(Model)

	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "INSERT ›") {
		t.Fatalf("the prompt gave way before the quote:\n%s", view)
	}
}

// TestTheRelayoutSettles. The reconciliation compares against the layout the
// current state COMPUTES to, not against what the composer asked for — a
// short terminal grants fewer rows than the ask, and comparing ask to grant
// finds a difference no relayout can close, then recomputes on every message
// for the rest of the session.
func TestTheRelayoutSettles(t *testing.T) {
	for _, height := range []int{5, 12, 19, 20, 40} {
		m := replyModel(t)
		sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
		m = sized.(Model)
		replied, _ := m.Update(chatview.MessageActionMsg{Action: "reply", ChatId: 1, MessageId: 5})
		m = replied.(Model)

		settled := m.layout
		// Any message at all, twice: if the guard could not converge, the
		// layout would be recomputed here and the model would differ.
		for range 2 {
			next, _ := m.Update(chromeTickMsg(sceneNow))
			m = next.(Model)
		}
		if m.layout != settled {
			t.Errorf("at %d rows the layout is still moving:\n got  %+v\n want %+v",
				height, m.layout, settled)
		}
	}
}

// TestTheReconciliationConvergesWhereTheGrantIsSmall.
//
// A terminal short enough that the composer cannot have every row it asks
// for is the case the naive guard cannot close: it compares the ask to the
// grant, finds them different, recomputes a layout that comes out the same,
// and does it again on the next message and every message after. Nothing on
// screen moves, so only the predicate can say it has settled.
func TestTheReconciliationConvergesWhereTheGrantIsSmall(t *testing.T) {
	var squeezed bool

	for height := 3; height <= 40; height++ {
		m := replyModel(t)
		sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: height})
		m = sized.(Model)
		replied, _ := m.Update(chatview.MessageActionMsg{Action: "reply", ChatId: 1, MessageId: 5})
		m = replied.(Model)
		// A reply AND an attachment: three rows wanted, and a short
		// terminal will not grant three. A reply alone is granted its two
		// at every height, which is the whole point of the cap — and
		// therefore proves nothing about convergence.
		m.composer.SetAttachment("/tmp/a.png", true)
		// Through an update, as an arriving attachment would be: setting
		// it on the component by hand leaves the frame legitimately stale
		// until something reconciles it.
		ticked, _ := m.Update(chromeTickMsg(sceneNow))
		m = ticked.(Model)

		if m.composer.Rows() > m.layout.ComposerHeight {
			squeezed = true
		}
		if m.layoutStale() {
			t.Errorf("at %d rows the frame never settles: composer wants %d, "+
				"the layout grants %d", height, m.composer.Rows(), m.layout.ComposerHeight)
		}
	}

	if !squeezed {
		t.Fatal("no height in this sweep actually squeezed the composer, " +
			"so this test proved nothing")
	}
}

// TestAnUnsizedFrameIsNeverStale, because there is no frame yet to be stale
// against.
func TestAnUnsizedFrameIsNeverStale(t *testing.T) {
	if mainModel(t, PanelComposer).layoutStale() {
		t.Error("a client that has not been told its size wants a relayout")
	}
}
