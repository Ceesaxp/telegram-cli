package app

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
)

// clientModel is mainModel with a client behind it, so a command that
// needs one is issued. The client has no connection and no test runs
// what it is handed.
func clientModel(t *testing.T) Model {
	t.Helper()
	cfg := &config.Config{}
	m := New(cfg, &telegram.Client{}, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	m.screen = ScreenMain
	return m
}

// :read-mentions is the palette's way to clear every @ in the open chat,
// and it takes no argument.
func TestReadMentionsIsACommand(t *testing.T) {
	c, ok := mainModel(t, PanelChatList).lookupCommand("read-mentions")
	if !ok {
		t.Fatal("no :read-mentions in the registry")
	}
	if c.Arg != ArgNone {
		t.Errorf(":read-mentions takes an argument (%v), want none", c.Arg)
	}
	if c.Description != "clear every unread mention in this chat" {
		t.Errorf("description = %q", c.Description)
	}
}

// With nothing open there is nothing to clear, and the palette closes on
// Enter, so the notice is the only answer the reader gets.
func TestReadMentionsNeedsAChat(t *testing.T) {
	m := clientModel(t)

	_, cmd, notice := m.runCommandLine("read-mentions")
	if cmd != nil {
		t.Error(":read-mentions tried to act with no chat open")
	}
	if notice != "no chat open" {
		t.Errorf("notice = %q, want %q", notice, "no chat open")
	}
}

// With a chat open it clears that chat's mentions.
func TestReadMentionsClearsTheOpenChat(t *testing.T) {
	m := clientModel(t)
	m.chatView.OpenChat(testChatID, "Test Chat")

	_, cmd, notice := m.runCommandLine("read-mentions")
	if cmd == nil {
		t.Fatal(":read-mentions issued no clear with a chat open")
	}
	if notice != "mentions cleared" {
		t.Errorf("notice = %q, want %q", notice, "mentions cleared")
	}
}

// g@ is a jump like a search hit: the chat view says where the mention is,
// and the app goes there through openChatAt, recording where the reader
// was so ctrl+o brings them back.
func TestAMentionJumpOpensTheMessageAndCanBeUndone(t *testing.T) {
	m := jumpModel(t)

	updated, _ := m.Update(chatview.MentionJumpMsg{ChatId: 11, MessageId: 400, Remaining: 2})
	got := updated.(Model)
	if id := got.chatView.ChatId(); id != 11 {
		t.Fatalf("the jump opened chat %d, want 11", id)
	}
	if id := got.chatView.TargetMessageId(); id != 400 {
		t.Errorf("the jump aimed at message %d, want the mention 400", id)
	}
	if len(got.jumps) != 1 || got.jumps[0] != (jumpPoint{ChatID: 11, MessageID: 501}) {
		t.Fatalf("the way back was not recorded: %+v", got.jumps)
	}

	back, _ := got.Update(chatview.JumpBackMsg{})
	if id := back.(Model).chatView.TargetMessageId(); id != 501 {
		t.Errorf("ctrl+o came back aiming at %d, want 501, where the reader was", id)
	}
}

// Arriving says how many are left, so the reader knows whether g@ again
// is worth pressing. The app says it rather than the chat view, because
// opening the chat resets the view's own notice.
func TestAMentionJumpSaysHowManyRemain(t *testing.T) {
	for remaining, want := range map[int]string{
		2: "mention · 2 more",
		0: "last mention",
	} {
		m := jumpModel(t)
		updated, _ := m.Update(chatview.MentionJumpMsg{ChatId: 11, MessageId: 400, Remaining: remaining})
		if row := hintBarRow(t, updated.(Model)); !strings.Contains(row, want) {
			t.Errorf("with %d remaining the hint bar does not say %q:\n%s", remaining, want, row)
		}
	}
}
