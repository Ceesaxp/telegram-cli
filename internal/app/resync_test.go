package app

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

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
