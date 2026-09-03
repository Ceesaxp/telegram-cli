package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

// The Telegram client can die without anything on screen changing: the run
// loop exits, no further updates arrive, and every panel keeps rendering the
// data it already had. These tests cover the app-side wiring that turns that
// silent death into something the user cannot miss.

// sizedMainModel is a main-screen model with a window size, so the views
// under test render something inspectable. Several components (the chat
// list's folder tab bar among them) render nothing at zero width, which
// would make view-based assertions pass vacuously.
func sizedMainModel(t *testing.T, focus ...FocusPanel) Model {
	t.Helper()
	panel := PanelChatList
	if len(focus) > 0 {
		panel = focus[0]
	}
	m := mainModel(t, panel)
	m.width, m.height = 100, 40
	m.updateLayout()
	return m
}

func send(t *testing.T, m Model, msg any) Model {
	t.Helper()
	out, _ := m.Update(msg)
	next, ok := out.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", out)
	}
	return next
}

func TestTerminalClientErrorIsUnmissable(t *testing.T) {
	m := sizedMainModel(t)
	m = send(t, m, telegram.ConnectionStateMsg{State: telegram.ConnectionStateReady})

	m = send(t, m, telegram.ClientErrorMsg{
		Err:      errors.New("session revoked from another device"),
		Terminal: true,
	})

	if m.fatalError == "" {
		t.Fatal("a terminal client error left fatalError empty")
	}

	// The error panel replaces the UI and names the cause.
	view := m.View().Content
	for _, want := range []string{
		"Disconnected from Telegram",
		"session revoked from another device",
		"Restart teletui",
		"Ctrl+Q",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("error panel does not mention %q", want)
		}
	}

	// Nothing left on screen may keep claiming the app is connected. The
	// error panel replaces the whole frame, so the top bar is not drawn at
	// all — but the state behind it must have moved too, or dismissing the
	// panel would reveal a bar still saying "connected".
	if m.topBar.View() != "" && strings.Contains(m.topBar.View(), "connected") &&
		!strings.Contains(m.topBar.View(), "disconnected") {
		t.Error("the top bar still reports connected after a terminal error")
	}

	// And the panels behind it stop taking input — acting on a key would
	// mutate state the user cannot see.
	after := send(t, m, telegram.ClientErrorMsg{}) // no-op message
	before := after.focus
	after = update(t, after, "\x1b2") // alt+2, normally focuses the chat view
	if after.focus != before {
		t.Errorf("alt+2 changed focus to %v while the client was dead", after.focus)
	}
	if after = update(t, after, "\x1bc"); after.contacts.IsVisible() {
		t.Error("alt+c opened the contacts overlay while the client was dead")
	}
}

func TestNonTerminalClientErrorIsANotice(t *testing.T) {
	m := sizedMainModel(t)
	m = send(t, m, telegram.ClientErrorMsg{Err: errors.New("rpc timed out")})

	if m.fatalError != "" {
		t.Errorf("a recoverable client error took the app down: %q", m.fatalError)
	}
	if view := m.View().Content; !strings.Contains(view, "rpc timed out") {
		t.Error("the recoverable error was not surfaced as a notice")
	}
	// The UI keeps working.
	if got := update(t, m, "\x1b2"); got.focus != PanelChatView {
		t.Errorf("focus = %v after alt+2, want PanelChatView", got.focus)
	}
}

func TestClientWarningIsANotice(t *testing.T) {
	m := sizedMainModel(t)
	m = send(t, m, telegram.ClientWarningMsg{Text: "update state cache unavailable"})

	if m.fatalError != "" {
		t.Errorf("a warning took the app down: %q", m.fatalError)
	}
	if view := m.View().Content; !strings.Contains(view, "update state cache unavailable") {
		t.Error("the warning was not surfaced as a notice")
	}
}

// TestAuthStateClosedIsTerminal covers the case that previously had no
// handler at all: the authorizer reported the session closed and the UI sat
// on whatever screen it happened to be showing.
func TestAuthStateClosedIsTerminal(t *testing.T) {
	t.Run("with a hint", func(t *testing.T) {
		m := send(t, sizedMainModel(t), AuthStateChangedMsg{
			State: int(telegram.AuthStateClosed),
			Hint:  "signed out elsewhere",
		})
		if m.fatalError != "signed out elsewhere" {
			t.Errorf("fatalError = %q, want the hint", m.fatalError)
		}
		if !strings.Contains(m.View().Content, "signed out elsewhere") {
			t.Error("the hint is not shown to the user")
		}
	})

	t.Run("without a hint", func(t *testing.T) {
		m := send(t, sizedMainModel(t), AuthStateChangedMsg{State: int(telegram.AuthStateClosed)})
		if m.fatalError == "" {
			t.Fatal("AuthStateClosed left fatalError empty")
		}
		if !strings.Contains(m.View().Content, "Disconnected from Telegram") {
			t.Error("the error panel was not rendered")
		}
	})

	// The states that are not terminal keep their existing behavior.
	for _, state := range []telegram.AuthState{
		telegram.AuthStateWaitPhone, telegram.AuthStateWaitCode,
		telegram.AuthStateWaitPassword, telegram.AuthStateReady,
	} {
		m := send(t, sizedMainModel(t), AuthStateChangedMsg{State: int(state)})
		if m.fatalError != "" {
			t.Errorf("auth state %v was treated as fatal: %q", state, m.fatalError)
		}
	}
}

func TestFatalErrorKeepsTheFirstCause(t *testing.T) {
	m := sizedMainModel(t)
	m = send(t, m, telegram.ClientErrorMsg{Err: errors.New("first"), Terminal: true})
	m = send(t, m, telegram.ClientErrorMsg{Err: errors.New("second"), Terminal: true})
	if m.fatalError != "first" {
		t.Errorf("fatalError = %q, want the first cause", m.fatalError)
	}
}

func TestTerminalClientErrorWithoutAnError(t *testing.T) {
	m := send(t, sizedMainModel(t), telegram.ClientErrorMsg{Terminal: true})
	if m.fatalError == "" {
		t.Fatal("a terminal error with a nil Err left fatalError empty")
	}
	if !strings.Contains(m.View().Content, "stopped unexpectedly") {
		t.Errorf("no readable reason rendered, fatalError = %q", m.fatalError)
	}
}

// TestTopBarRendersDisconnected: the connection dot is the only standing
// statement about whether this client is talking to Telegram, so a dropped
// connection has to reach it. This guarantee used to live on the status
// bar, which the frame replaced.
func TestTopBarRendersDisconnected(t *testing.T) {
	m := sizedMainModel(t)
	m = send(t, m, telegram.ConnectionStateMsg{State: telegram.ConnectionStateReady})
	if !strings.Contains(m.topBar.View(), "connected") {
		t.Fatalf("precondition: top bar does not render a connected state: %q", m.topBar.View())
	}

	m = send(t, m, telegram.ConnectionStateMsg{State: telegram.ConnectionStateDisconnected})
	bar := m.topBar.View()
	if !strings.Contains(bar, "disconnected") {
		t.Errorf("top bar does not render ConnectionStateDisconnected: %q", bar)
	}
	if !strings.Contains(m.View().Content, "disconnected") {
		t.Errorf("the disconnected state never reaches the screen")
	}
}
