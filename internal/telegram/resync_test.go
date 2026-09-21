package telegram

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// When gotd gives up on replaying a gap, the UI has to hear about it: a
// thread that silently stays where it was is the bug this exists to fix.
func TestResyncNeededReachesTheSink(t *testing.T) {
	c := &Client{}
	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })

	c.resyncNeeded("difference too long")

	if len(got) != 1 {
		t.Fatalf("sink received %d messages, want 1", len(got))
	}
	msg, ok := got[0].(ResyncNeededMsg)
	if !ok || msg.Reason != "difference too long" {
		t.Fatalf("sink received %#v, want ResyncNeededMsg with the reason", got[0])
	}
}
