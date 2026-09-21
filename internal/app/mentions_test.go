package app

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
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
