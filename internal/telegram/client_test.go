package telegram

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestConnectionStateValues pins the constant numbering. The values are
// compared (not switched) by the status bar, so renumbering them by
// inserting a new state in the middle would silently change meaning.
func TestConnectionStateValues(t *testing.T) {
	if ConnectionStateConnecting != 0 {
		t.Errorf("ConnectionStateConnecting = %d, want 0", ConnectionStateConnecting)
	}
	if ConnectionStateReady != 1 {
		t.Errorf("ConnectionStateReady = %d, want 1", ConnectionStateReady)
	}
	if ConnectionStateDisconnected != 2 {
		t.Errorf("ConnectionStateDisconnected = %d, want 2", ConnectionStateDisconnected)
	}
}

// TestNotifyBuffersUntilSinkRegisters covers the reason notify exists:
// every startup degradation is detected during construction, long before
// the listener wires a sink, and the nil-safe send would drop it.
func TestNotifyBuffersUntilSinkRegisters(t *testing.T) {
	var c Client

	boom := errors.New("boom")
	c.notify(ClientWarningMsg{Text: "first"})
	c.notify(ClientErrorMsg{Err: boom, Terminal: true})

	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })

	if len(got) != 2 {
		t.Fatalf("got %d replayed messages, want 2: %#v", len(got), got)
	}
	w, ok := got[0].(ClientWarningMsg)
	if !ok || w.Text != "first" {
		t.Errorf("got[0] = %#v, want ClientWarningMsg{first}", got[0])
	}
	e, ok := got[1].(ClientErrorMsg)
	if !ok || !errors.Is(e.Err, boom) || !e.Terminal {
		t.Errorf("got[1] = %#v, want terminal ClientErrorMsg{boom}", got[1])
	}

	// The buffer must drain exactly once, not replay to every sink.
	var second []tea.Msg
	c.setMsgSink(func(m tea.Msg) { second = append(second, m) })
	if len(second) != 0 {
		t.Errorf("second sink got %d messages, want 0: %#v", len(second), second)
	}
}

// TestNotifyDeliversDirectlyOnceWired checks the non-buffered path.
func TestNotifyDeliversDirectlyOnceWired(t *testing.T) {
	var c Client

	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })

	c.notify(ClientWarningMsg{Text: "live"})
	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1: %#v", len(got), got)
	}
	if w, ok := got[0].(ClientWarningMsg); !ok || w.Text != "live" {
		t.Errorf("got %#v, want ClientWarningMsg{live}", got[0])
	}
}

// TestSetMsgSinkNilKeepsBuffer guards against a nil sink silently
// discarding pending notices.
func TestSetMsgSinkNilKeepsBuffer(t *testing.T) {
	var c Client
	c.notify(ClientWarningMsg{Text: "kept"})

	c.setMsgSink(nil)

	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })
	if len(got) != 1 {
		t.Fatalf("got %d messages after a nil sink, want 1: %#v", len(got), got)
	}
}

// TestSetMsgSinkReplaysConnectionStateFirst checks ordering: the state
// must land before the notices explaining it.
func TestSetMsgSinkReplaysConnectionStateFirst(t *testing.T) {
	var c Client
	c.lastConnState = ConnectionStateDisconnected
	c.hasConnState = true
	c.notify(ClientErrorMsg{Err: errors.New("died"), Terminal: true})

	var got []tea.Msg
	c.setMsgSink(func(m tea.Msg) { got = append(got, m) })

	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2: %#v", len(got), got)
	}
	cs, ok := got[0].(ConnectionStateMsg)
	if !ok || cs.State != ConnectionStateDisconnected {
		t.Errorf("got[0] = %#v, want ConnectionStateMsg{Disconnected}", got[0])
	}
	if _, ok := got[1].(ClientErrorMsg); !ok {
		t.Errorf("got[1] = %#v, want ClientErrorMsg", got[1])
	}
}
