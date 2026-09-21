package app

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// A reconnect reloads the chat list, even with no chat open. gotd drops a
// read receipt that comes after a new message in the same difference, so a
// message read on the phone while the laptop slept came back counted and
// never cleared. Only the dialog list says what is really unread.
//
// The client is never called: what is under test is whether a command is
// issued at all, and with no chat open the chat list is the only one that
// could be.
func TestAReconnectReloadsTheChatList(t *testing.T) {
	cfg := &config.Config{}
	m := New(cfg, &telegram.Client{}, store.NewStore(), telegram.NewTUIAuthorizer(cfg))
	m.screen = ScreenMain

	steps := []struct {
		state telegram.ConnectionState
		want  bool
	}{
		{telegram.ConnectionStateReady, false}, // first connect: the list is loading anyway
		{telegram.ConnectionStateConnecting, false},
		{telegram.ConnectionStateReady, true}, // back after a drop
	}
	for i, step := range steps {
		out, cmd := m.Update(telegram.ConnectionStateMsg{State: step.state})
		m = out.(Model)
		if got := cmd != nil; got != step.want {
			t.Fatalf("step %d (%v): issued a command = %v, want %v", i, step.state, got, step.want)
		}
	}
}

func TestConnTrackerReportsOnlyAReturnToReady(t *testing.T) {
	var c connTracker
	steps := []struct {
		state telegram.ConnectionState
		want  bool
	}{
		{telegram.ConnectionStateConnecting, false},
		{telegram.ConnectionStateReady, false}, // first connect: the first page is loading anyway
		{telegram.ConnectionStateReady, false}, // repeated Ready is not a reconnect
		{telegram.ConnectionStateConnecting, false},
		{telegram.ConnectionStateReady, true}, // back after a drop
		{telegram.ConnectionStateDisconnected, false},
		{telegram.ConnectionStateReady, true},
	}
	for i, step := range steps {
		if got := c.observe(step.state); got != step.want {
			t.Fatalf("step %d (%v): reconnected = %v, want %v", i, step.state, got, step.want)
		}
	}
}

// The client saying it cannot replay a gap is the one moment the reader
// should be told the screen may be behind; the refetch itself is silent.
func TestResyncNeededIsAnnounced(t *testing.T) {
	m := sizedMainModel(t, PanelChatView)

	m = send(t, m, telegram.ResyncNeededMsg{Reason: "difference too long"})

	if view := m.View().Content; !strings.Contains(view, "resyncing: difference too long") {
		t.Fatalf("view after ResyncNeededMsg lacks the notice:\n%s", view)
	}
}
