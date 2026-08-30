package chatview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/telegram"
)

// spoilerModel is n messages, each ending in a spoiler, with the cursor on
// the newest.
func spoilerModel(t *testing.T, n int) Model {
	t.Helper()
	m := newTestModel()
	m.SetSize(60, 20)
	m.focused = true
	m.chatID = testChatID

	for id := int64(1); id <= int64(n); id++ {
		const text = "the answer is 42"
		m.store.Messages.Append(testChatID, &telegram.Message{
			ID: id, ChatID: testChatID, Date: fixedDate,
			SenderID: &telegram.MessageSenderUser{UserID: 200},
			Content: &telegram.MessageText{Text: &telegram.FormattedText{
				Text: text,
				Entities: []*telegram.TextEntity{{
					Offset: 14, Length: 2, Type: &telegram.TextEntityTypeSpoiler{},
				}},
			}},
		})
	}
	m.syncCursor()
	return m
}

// hiddenMark is what a hidden spoiler looks like in the output: the same
// colour set as both foreground and background, so the text occupies its
// cells and none of it is legible.
//
// Matching the pair rather than the foreground alone is not fussiness. In
// the 256-colour palette several roles share a value — Sel and RuleSoft are
// both 235 — so a foreground-only match counts the day divider's rule as a
// hidden spoiler, and the test passes on a screen with no spoiler on it.
func hiddenMark(m Model) string {
	c := string(m.roles.Sel)
	return "38;5;" + c + ";48;5;" + c
}

// hiddenSpoilers counts the rows drawing a spoiler in its own background.
func hiddenSpoilers(m Model) int {
	mark := hiddenMark(m)
	n := 0
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, mark) {
			n++
		}
	}
	return n
}

// TestSpoilersAreHiddenUntilX. A spoiler that opened on its own would be
// revealed to whoever is looking at the screen, which is the one thing it
// exists to prevent.
func TestSpoilersAreHiddenUntilX(t *testing.T) {
	m := spoilerModel(t, 2)

	if got := hiddenSpoilers(m); got != 2 {
		t.Fatalf("expected both spoilers hidden on open, %d are", got)
	}

	m, _ = m.handleKey(key('x'))
	if got := hiddenSpoilers(m); got != 1 {
		t.Fatalf("expected exactly one spoiler open after x, %d are still hidden", got)
	}
	if m.revealedID != m.cursorID {
		t.Fatalf("x opened message %d, cursor is on %d", m.revealedID, m.cursorID)
	}
}

// TestXOpensOnlyTheCursorMessage: the reveal is scoped to one message, so a
// reader can look at the one they meant without exposing the rest of the
// thread.
func TestXOpensOnlyTheCursorMessage(t *testing.T) {
	m := spoilerModel(t, 2)
	m, _ = m.handleKey(key('x'))

	view := strings.Split(m.View(), "\n")
	mark := hiddenMark(m)

	var openRows, hiddenRows int
	for _, line := range view {
		if !strings.Contains(ansi.Strip(line), "the answer is") {
			continue
		}
		if strings.Contains(line, mark) {
			hiddenRows++
		} else {
			openRows++
		}
	}
	if openRows != 1 || hiddenRows != 1 {
		t.Fatalf("expected one open and one hidden message, got %d open and %d hidden",
			openRows, hiddenRows)
	}
}

// TestXTogglesTheReveal. Having looked, the reader may well want the screen
// safe again, and there is otherwise no way back short of leaving the chat.
func TestXTogglesTheReveal(t *testing.T) {
	m := spoilerModel(t, 2)

	m, _ = m.handleKey(key('x'))
	if m.revealedID == 0 {
		t.Fatal("x did not open anything")
	}
	m, _ = m.handleKey(key('x'))
	if m.revealedID != 0 {
		t.Fatalf("a second x left message %d open", m.revealedID)
	}
	if got := hiddenSpoilers(m); got != 2 {
		t.Fatalf("expected both spoilers hidden again, %d are", got)
	}
}

// TestMovingTheCursorClosesSpoilers: a spoiler left open after the reader
// scrolled away is revealed to whoever looks at the screen next.
func TestMovingTheCursorClosesSpoilers(t *testing.T) {
	// Tall enough that the cursor can be scrolled out of the window; two
	// messages in a twenty-row panel have nowhere to go.
	m := spoilerModel(t, 40)
	m.scrollOffset = 2
	m.syncCursor()
	m, _ = m.handleKey(key('x'))
	if m.revealedID == 0 {
		t.Fatal("x did not open anything")
	}

	// Anything that moves the cursor: a scroll that carries it off, and a
	// jump that claims it outright.
	scrolled := m
	scrollUntilCursorMoves(t, &scrolled, 3)
	if scrolled.revealedID != 0 {
		t.Errorf("scrolling the cursor away left message %d open", scrolled.revealedID)
	}

	jumped := m
	jumped.scrollToMessage(1)
	if jumped.revealedID != 0 {
		t.Errorf("jumping left message %d open", jumped.revealedID)
	}
}

// TestOpeningAChatClosesSpoilers, so a spoiler cannot survive into a chat
// the reader opened in front of somebody.
func TestOpeningAChatClosesSpoilers(t *testing.T) {
	m := spoilerModel(t, 2)
	m, _ = m.handleKey(key('x'))
	if m.revealedID == 0 {
		t.Fatal("x did not open anything")
	}

	m.OpenChatAt(testChatID, "again", 0)
	if m.revealedID != 0 {
		t.Fatalf("chat open left message %d revealed", m.revealedID)
	}
}

// TestXIsRefusedAsAConfiguredMnemonic. x belongs to this package now, and a
// configuration that points reply or delete at it must be refused rather
// than silently shadowing the reveal — and ActiveKeys has to report the
// refusal, or the help card advertises a binding that does nothing.
func TestXIsRefusedAsAConfiguredMnemonic(t *testing.T) {
	m := keysTestModel()
	m.SetKeys(Keys{Delete: "x"})

	if got := m.ActiveKeys().Delete; got == "x" {
		t.Fatalf("delete accepted x, which is the spoiler reveal")
	}
	if _, cmd := m.handleKey(key('x')); cmd != nil {
		t.Fatalf("x dispatched a message action instead of revealing spoilers")
	}
}
