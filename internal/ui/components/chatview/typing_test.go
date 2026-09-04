package chatview

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Ceesaxp/telegram-cli/internal/telegram"
)

func typingAction(user int64) telegram.ChatActionMsg {
	return telegram.ChatActionMsg{ChatId: testChatID, UserId: user, Action: &telegram.ChatActionTyping{}}
}

func cancelAction(user int64) telegram.ChatActionMsg {
	return telegram.ChatActionMsg{ChatId: testChatID, UserId: user, Action: &telegram.ChatActionCancel{}}
}

// The first action arms the marker; the row it draws is still the resting
// frame, so nothing jumps at the moment somebody starts typing.
func TestTypingActionArmsTheAnimation(t *testing.T) {
	m := gridModel(t, 67)

	m, cmd := m.Update(typingAction(200))
	if cmd == nil {
		t.Fatal("a first typing action did not arm the marker")
	}
	if m.typingFrame != 0 {
		t.Errorf("typingFrame = %d, want the resting frame", m.typingFrame)
	}
	if !strings.Contains(ansi.Strip(m.gridTypingRow()), typingFrames[0]) {
		t.Errorf("row does not draw the resting frame: %q", ansi.Strip(m.gridTypingRow()))
	}
}

// One chain, however many typists. A second timer would halve the frame
// time, and a third would halve it again.
func TestTypingAnimationRunsOneChain(t *testing.T) {
	m := gridModel(t, 67)

	m, first := m.Update(typingAction(200))
	m, second := m.Update(typingAction(201))
	m, repeat := m.Update(typingAction(200))
	if first == nil {
		t.Fatal("the first action did not arm the marker")
	}
	if second != nil || repeat != nil {
		t.Errorf("a further typist armed a second chain: second=%v repeat=%v", second != nil, repeat != nil)
	}
}

func TestTypingTickAdvancesAndRearms(t *testing.T) {
	m := gridModel(t, 67)
	m, _ = m.Update(typingAction(200))

	now := time.Now()
	for i := 1; i <= len(typingFrames); i++ {
		var cmd tea.Cmd
		m, cmd = m.Update(typingTickMsg{at: now, gen: m.typingGen})
		if cmd == nil {
			t.Fatalf("frame %d: the chain stopped while somebody was still typing", i)
		}
		if want := i % len(typingFrames); m.typingFrame != want {
			t.Fatalf("after %d ticks typingFrame = %d, want %d", i, m.typingFrame, want)
		}
	}
}

// A tick from a chain that was stopped must not re-arm: it would run
// alongside whatever chain replaced it.
func TestStaleTypingTickIsIgnored(t *testing.T) {
	m := gridModel(t, 67)
	m, _ = m.Update(typingAction(200))
	stale := typingTickMsg{at: time.Now(), gen: m.typingGen}

	m.stopTypingAnim()
	m, cmd := m.Update(stale)
	if cmd != nil {
		t.Fatal("a tick from a stopped chain re-armed the timer")
	}
}

// The set used to be emptied only by an explicit cancel, so a cancel lost
// to a connection blip left a phantom typist on screen for the rest of the
// session. Telegram's own rule is that an action stands for six seconds.
func TestTypingActionLapses(t *testing.T) {
	m := gridModel(t, 67)
	m, _ = m.Update(typingAction(200))

	m, cmd := m.Update(typingTickMsg{at: time.Now().Add(typingTTL + time.Second), gen: m.typingGen})
	if len(m.typing) != 0 {
		t.Fatalf("a lapsed action survived: %v", m.typing)
	}
	if cmd != nil {
		t.Error("the chain kept ticking with nobody typing")
	}
	if got := m.gridTypingRow(); got != "" {
		t.Errorf("a lapsed action still draws a row: %q", got)
	}
}

// A repeat is a refresh: Telegram re-sends every few seconds precisely so
// the action does not lapse while somebody is still typing.
func TestRepeatedActionRefreshesTheDeadline(t *testing.T) {
	start := time.Now()
	set := applyChatAction(nil, typingAction(200), start)
	first := set[0].until

	set = applyChatAction(set, typingAction(200), start.Add(3*time.Second))
	if len(set) != 1 {
		t.Fatalf("a repeat doubled the typist up: %v", set)
	}
	if !set[0].until.After(first) {
		t.Errorf("deadline %v was not refreshed past %v", set[0].until, first)
	}
}

// Cancelling the last typist stops the chain rather than leaving a timer
// ticking over an empty set.
func TestCancelStopsTheChain(t *testing.T) {
	m := gridModel(t, 67)
	m, _ = m.Update(typingAction(200))
	gen := m.typingGen

	m, cmd := m.Update(cancelAction(200))
	if cmd != nil {
		t.Error("cancelling the last typist armed a timer")
	}
	if m.typingGen == gen {
		t.Error("cancelling the last typist left the chain live")
	}
	if m.typingFrame != 0 {
		t.Errorf("typingFrame = %d, want the marker back at rest", m.typingFrame)
	}
}

// Switching chats drops the set and the chain with it: the new thread has
// its own typists, and the old chain's next tick must not re-arm over them.
func TestOpeningAnotherChatStopsTheChain(t *testing.T) {
	m := gridModel(t, 67)
	m, _ = m.Update(typingAction(200))
	if len(m.typing) != 1 {
		t.Fatal("precondition: nobody is typing")
	}
	stale := typingTickMsg{at: time.Now(), gen: m.typingGen}

	m.OpenChatAt(testChatID+1, "elsewhere", 0)
	if len(m.typing) != 0 {
		t.Fatalf("the previous chat's typists came along: %v", m.typing)
	}

	m, cmd := m.Update(stale)
	if cmd != nil {
		t.Fatal("the previous chat's chain re-armed after the switch")
	}
}
