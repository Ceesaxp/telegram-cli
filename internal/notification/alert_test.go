package notification

import (
	"strings"
	"testing"
	"time"
)

// bothFallBack is a notifier and a player on a machine with neither
// installed, on one clock that only moves when the test moves it.
func bothFallBack() (*Notifier, *SoundPlayer, *time.Time) {
	s, clock := soundPlayer(newProcess())
	s.play = nil

	n := NewNotifier(true, true, MethodSystem)
	n.system = nil
	n.bells = newBellLimiter(func() time.Time { return *clock })
	return n, s, clock
}

// Where the notifier and the player both fall back to the bell, one message
// rings it once, and so does a burst of them.
func TestABurstWhereBothFallBackRingsOnce(t *testing.T) {
	n, s, clock := bothFallBack()

	var written strings.Builder
	for range 50 {
		written.WriteString(Alert(n, s, "Ana", "hi"))
	}
	if got := strings.Count(written.String(), "\a"); got != 1 {
		t.Errorf("a burst of 50 rang the bell %d times, want 1", got)
	}

	*clock = clock.Add(minSoundInterval)
	if got := Alert(n, s, "Ana", "later"); got != "\a" {
		t.Errorf("a message the interval later was handed %q, want one bell", got)
	}
}

// The terminal has one bell, so the two fallbacks keep one limit between
// them. With a limit each, they only agree while they are asked at the same
// instants; once one has rung without the other, each rings once an
// interval on its own, and a burst rings twice.
func TestTheFallbacksKeepOneLimitBetweenThem(t *testing.T) {
	n, s, clock := bothFallBack()

	if got := n.Notify("Ana", "first"); got != "\a" {
		t.Fatalf("precondition: the notifier's fallback did not ring: %q", got)
	}
	*clock = clock.Add(minSoundInterval / 2)

	if got := Alert(n, s, "Ana", "second"); got != "" {
		t.Errorf("half an interval after the bell the terminal was handed %q, want nothing", got)
	}
}
