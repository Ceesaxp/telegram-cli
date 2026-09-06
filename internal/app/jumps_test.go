package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatlist"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/hintbar"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/search"
)

// jumpModel is an app with one chat open, a message under the cursor, and a
// second chat the reader could be carried off to.
func jumpModel(t *testing.T) Model {
	t.Helper()
	m := mainModel(t, PanelChatView)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = updated.(Model)

	for _, chat := range []*telegram.Chat{
		{ID: 11, Title: "here"},
		{ID: 22, Title: "there"},
	} {
		m.store.Chats.Set(chat)
	}
	m.store.Messages.Activate(11)
	m.store.Messages.Append(11, &telegram.Message{ID: 501, ChatID: 11})
	m.openChatAt(11, 0)
	m.chatView.MarkLoadedForTest()
	return m
}

// A jump records where the reader WAS, message and all: coming back to the
// chat but not to the message would land them at the bottom of a buffer
// they were reading the middle of.
func TestAJumpRemembersTheMessageTheReaderWasOn(t *testing.T) {
	m := jumpModel(t)
	if got := m.chatView.CursorMessageId(); got != 501 {
		t.Fatalf("precondition: cursor is on %d, want 501", got)
	}

	m.pushJump()
	if len(m.jumps) != 1 {
		t.Fatalf("stack holds %d entries, want 1", len(m.jumps))
	}
	if want := (jumpPoint{ChatID: 11, MessageID: 501}); m.jumps[0] != want {
		t.Errorf("recorded %+v, want %+v", m.jumps[0], want)
	}

	m.openChatAt(22, 0)
	if cmd := m.jumpBack(); cmd == nil {
		t.Fatal("going back produced no command")
	}
	if got := m.chatView.ChatId(); got != 11 {
		t.Errorf("came back to chat %d, want 11", got)
	}
	if got := m.chatView.TargetMessageId(); got != 501 {
		t.Errorf("came back aiming at message %d, want 501", got)
	}
	if len(m.jumps) != 0 {
		t.Errorf("going back left %d entries on the stack, want 0", len(m.jumps))
	}
}

// The stack is bounded, and it is the OLDEST entry that goes. ctrl+o asks
// "where was I just now", so dropping the newest would answer a different
// question every time the cap was reached.
func TestTheJumpListIsBounded(t *testing.T) {
	m := jumpModel(t)
	for i := int64(1); i <= maxJumps+8; i++ {
		m.store.Chats.Set(&telegram.Chat{ID: 1000 + i, Title: "n"})
		m.openChatAt(1000+i, 0)
		m.pushJump()
	}

	if len(m.jumps) != maxJumps {
		t.Fatalf("stack holds %d entries, want the cap of %d", len(m.jumps), maxJumps)
	}
	if got := m.jumps[len(m.jumps)-1].ChatID; got != 1000+maxJumps+8 {
		t.Errorf("newest entry is chat %d, want %d", got, 1000+maxJumps+8)
	}
	if got := m.jumps[0].ChatID; got != 1000+9 {
		t.Errorf("oldest surviving entry is chat %d, want %d", got, 1000+9)
	}
}

// Going back from nowhere says so. A key that silently does nothing is
// indistinguishable from a key nothing is bound to, which is how a reader
// concludes a binding is broken.
func TestGoingBackWithNoJumpSaysSo(t *testing.T) {
	m := jumpModel(t)
	if cmd := m.jumpBack(); cmd != nil {
		t.Error("an empty jump list still produced a command")
	}
	if row := hintBarRow(t, m); !strings.Contains(row, "no jump to go back from") {
		t.Errorf("the empty stack said nothing:\n%s", row)
	}
}

// The same position twice is one entry. Two identical answers to "where was
// I" would make the first ctrl+o look like a key that did nothing.
func TestTheSamePositionIsNotRecordedTwice(t *testing.T) {
	m := jumpModel(t)
	m.pushJump()
	m.pushJump()
	if len(m.jumps) != 1 {
		t.Errorf("stack holds %d entries, want 1", len(m.jumps))
	}
}

// Nothing open is not a place to come back to.
func TestNothingOpenIsNotAJumpOrigin(t *testing.T) {
	m := newTestModel(t)
	m.pushJump()
	if len(m.jumps) != 0 {
		t.Errorf("stack holds %d entries with no chat open, want 0", len(m.jumps))
	}
}

// A private-channel link is a jump and needs no round trip: the ID is in
// the link.
func TestFollowingAPrivateChannelLinkJumps(t *testing.T) {
	m := jumpModel(t)
	cmd := m.followTelegramLink(chatview.TelegramLinkMsg{
		TmeLink: telegram.TmeLink{ChatID: 22, MessageID: 900},
		URI:     "https://t.me/c/22/900",
	})
	if cmd == nil {
		t.Fatal("following the link produced no command")
	}
	if got := m.chatView.ChatId(); got != 22 {
		t.Errorf("opened chat %d, want 22", got)
	}
	if got := m.chatView.TargetMessageId(); got != 900 {
		t.Errorf("aimed at message %d, want 900", got)
	}
	if len(m.jumps) != 1 || m.jumps[0].ChatID != 11 {
		t.Errorf("the way back was not recorded: %+v", m.jumps)
	}
}

// A private channel this account has never seen has no username to resolve
// and no access hash to fetch history with, so it is refused honestly
// rather than opened into a buffer that fails to load with the reader
// sitting in it.
func TestAnUnknownPrivateChannelLinkIsRefusedOutLoud(t *testing.T) {
	m := jumpModel(t)
	cmd := m.followTelegramLink(chatview.TelegramLinkMsg{
		TmeLink: telegram.TmeLink{ChatID: -1000000000000 - 999, MessageID: 5},
		URI:     "https://t.me/c/999/5",
	})
	if cmd != nil {
		t.Error("an unreachable chat still produced an open")
	}
	if got := m.chatView.ChatId(); got != 11 {
		t.Errorf("the reader was moved to chat %d anyway", got)
	}
	if len(m.jumps) != 0 {
		t.Errorf("a jump that did not happen was recorded: %+v", m.jumps)
	}
	if row := hintBarRow(t, m); !strings.Contains(row, "not in your chat list") {
		t.Errorf("the refusal was silent:\n%s", row)
	}
}

// A username link is a round trip: nothing moves and nothing is recorded
// until the server has answered, so a resolution that fails leaves the jump
// list exactly as it was.
func TestAUsernameLinkOnlyJumpsOnceItResolves(t *testing.T) {
	m := jumpModel(t)
	cmd := m.followTelegramLink(chatview.TelegramLinkMsg{
		TmeLink: telegram.TmeLink{Username: "elsewhere", MessageID: 42},
		URI:     "https://t.me/elsewhere/42",
	})
	if cmd == nil {
		t.Fatal("a username link produced no resolution command")
	}
	if got := m.chatView.ChatId(); got != 11 {
		t.Errorf("the reader moved before the server answered, to chat %d", got)
	}
	if len(m.jumps) != 0 {
		t.Errorf("a jump was recorded before the server answered: %+v", m.jumps)
	}

	// The answer, carrying a chat this client had never heard of.
	resolved := &telegram.Chat{ID: -1000000000000 - 77, Title: "Elsewhere"}
	if cmd := m.openResolvedLink(telegramLinkResolvedMsg{
		chat: resolved, username: "elsewhere", messageID: 42,
	}); cmd == nil {
		t.Fatal("the resolved link produced no open")
	}
	if got := m.chatView.ChatId(); got != resolved.ID {
		t.Errorf("opened chat %d, want %d", got, resolved.ID)
	}
	if got := m.chatView.TargetMessageId(); got != 42 {
		t.Errorf("aimed at message %d, want 42", got)
	}
	if len(m.jumps) != 1 || m.jumps[0].ChatID != 11 {
		t.Errorf("the way back was not recorded: %+v", m.jumps)
	}
	// The store learns the chat here rather than from the announcement
	// racing this message, or the header opens with no title.
	entry, ok := m.store.Chats.Get(resolved.ID)
	if !ok || entry.Chat == nil || entry.Chat.Title != "Elsewhere" {
		t.Error("the resolved chat did not reach the store before the open")
	}
}

// The full path, through Update: a chat view asking to follow a link ends
// with the app somewhere else and a way back.
func TestTheLinkMessageIsWiredThroughUpdate(t *testing.T) {
	m := jumpModel(t)
	updated, _ := m.Update(chatview.TelegramLinkMsg{
		TmeLink: telegram.TmeLink{ChatID: 22, MessageID: 900},
		URI:     "https://t.me/c/22/900",
	})
	got := updated.(Model)
	if got.chatView.ChatId() != 22 {
		t.Fatalf("TelegramLinkMsg did not open chat 22, got %d", got.chatView.ChatId())
	}

	back, _ := got.Update(chatview.JumpBackMsg{})
	if id := back.(Model).chatView.ChatId(); id != 11 {
		t.Errorf("JumpBackMsg landed on chat %d, want 11", id)
	}
}

// Which moves are jumps, asserted where the decision was made. A search hit
// is a teleport and gets a way back; opening a chat from the list is not,
// because the list is still there holding the cursor the reader left.
func TestWhichMovesAreJumps(t *testing.T) {
	t.Run("a search hit is", func(t *testing.T) {
		m := jumpModel(t)
		updated, _ := m.Update(search.SearchResultMsg{ChatId: 22, MessageId: 7})
		if got := updated.(Model); len(got.jumps) != 1 {
			t.Errorf("a search hit recorded %d jumps, want 1", len(got.jumps))
		}
	})

	t.Run("a discussion is", func(t *testing.T) {
		m := jumpModel(t)
		updated, _ := m.Update(openDiscussionMsg{ChatId: 22, MessageId: 7})
		if got := updated.(Model); len(got.jumps) != 1 {
			t.Errorf("a discussion jump recorded %d jumps, want 1", len(got.jumps))
		}
	})

	t.Run("the chat list is not", func(t *testing.T) {
		m := jumpModel(t)
		updated, _ := m.Update(chatlist.ChatSelectedMsg{ChatId: 22})
		if got := updated.(Model); len(got.jumps) != 0 {
			t.Errorf("opening a chat from the list recorded %d jumps, want 0", len(got.jumps))
		}
	})
}

// The hint leads only while there is a way back, for the reason the
// contacts filter's "esc clear" does: a row that named it the rest of the
// time would advertise a key answering "no jump to go back from".
func TestTheBackHintLeadsOnlyWhileThereIsAJump(t *testing.T) {
	m := jumpModel(t)
	if named := namesKey(m.hintsFor(SurfaceChatView), "ctrl+o"); named {
		t.Error("the chat view offers a way back with nothing on the stack")
	}
	m.pushJump()
	if named := namesKey(m.hintsFor(SurfaceChatView), "ctrl+o"); !named {
		t.Error("the chat view does not name the way back after a jump")
	}
}

func namesKey(hints []hintbar.Hint, key string) bool {
	for _, h := range hints {
		if h.Key == key {
			return true
		}
	}
	return false
}
