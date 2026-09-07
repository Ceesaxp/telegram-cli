package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// connTracker turns the stream of connection states into the one event the
// thread cares about: coming back. A reconnect is where updates go missing
// — gotd replays what it can from the sync point, but not what the server
// declines to replay — so it is where the open chat is worth a second look.
//
// The first Ready of a session is not a reconnect: the first page is loading
// then and is the newest page already.
type connTracker struct {
	ready     bool
	everReady bool
}

// observe records a state and reports whether it completed a reconnect.
func (c *connTracker) observe(state telegram.ConnectionState) (reconnected bool) {
	ready := state == telegram.ConnectionStateReady
	reconnected = ready && !c.ready && c.everReady
	c.ready = ready
	if ready {
		c.everReady = true
	}
	return reconnected
}

// resync refetches what a gap in the update stream may have left stale:
// the dialog list, which carries every preview, unread count and read
// mark, and the open chat's newest page.
func (m *Model) resync(reason string) tea.Cmd {
	m.notify("⟳ resyncing: " + reason)
	return tea.Batch(m.chatList.ReloadCmd(), m.chatView.CatchUpCmd())
}
