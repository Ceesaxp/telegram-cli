package notification

import (
	"os/exec"
	"testing"
	"time"
)

// soundPlayer builds an enabled player whose player process is p, on a clock
// that only moves when the test moves it.
func soundPlayer(p *process) (*SoundPlayer, *time.Time) {
	clock := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	s := NewSoundPlayer(true)
	s.play = p.play
	s.now = func() time.Time { return clock }
	return s, &clock
}

// A burst of messages used to start a player per message, all at once: the
// same sound fifty times over itself. One plays now, and the rest are
// dropped rather than queued — a sound late is a sound about nothing.
func TestABurstPlaysOneSound(t *testing.T) {
	p := newProcess()
	s, _ := soundPlayer(p)

	for range 50 {
		s.Play()
	}
	p.awaitStart(t)
	p.exit()
	s.Close()

	runs, peak := p.report()
	if peak != 1 {
		t.Errorf("%d players ran at once, want 1", peak)
	}
	if len(runs) != 1 {
		t.Errorf("50 messages started %d players, want 1", len(runs))
	}
}

// One at a time is not enough on its own: a player that returns at once — a
// short sound, or one that failed — would let a burst through one sound
// after another. So a sound that starts within minSoundInterval of the last
// one is dropped too, whether or not that one is still playing.
func TestASoundSoonAfterTheLastIsDropped(t *testing.T) {
	p := newProcess()
	p.exit()
	s, clock := soundPlayer(p)

	s.Play()
	s.wait()
	*clock = clock.Add(minSoundInterval - time.Millisecond)
	s.Play()
	s.Close()

	if runs, _ := p.report(); len(runs) != 1 {
		t.Errorf("two sounds %v apart started %d players, want 1",
			minSoundInterval-time.Millisecond, len(runs))
	}
}

// And the interval is not enough on its own either: a sound longer than it —
// afplay's Ping runs a second and a half — would overlap the next one.
func TestASoundStillPlayingDropsTheNext(t *testing.T) {
	p := newProcess()
	s, clock := soundPlayer(p)

	s.Play()
	p.awaitStart(t)
	*clock = clock.Add(10 * minSoundInterval)
	s.Play()
	p.exit()
	s.Close()

	runs, peak := p.report()
	if len(runs) != 1 || peak != 1 {
		t.Errorf("a sound over a playing one started %d players, %d at once; want 1, 1", len(runs), peak)
	}
}

// Close is for shutting down, and a player started after it would outlive
// whatever asked for it.
func TestAClosedPlayerStartsNothing(t *testing.T) {
	p := newProcess()
	p.exit()
	s, _ := soundPlayer(p)

	s.Close()
	s.Play()
	s.Close()

	if runs, _ := p.report(); len(runs) != 0 {
		t.Errorf("a closed player started %d players", len(runs))
	}
}

// Where there is no player to run, the bell stands in for the sound, and it
// goes back to the caller for tea.Raw. Printed from the background, it
// landed wherever the renderer happened to be mid-frame.
func TestAPlatformWithoutAPlayerHandsBackTheBell(t *testing.T) {
	s := NewSoundPlayer(true)
	s.play = platformPlayer("plan9", installed("paplay", "afplay"))

	if got := s.Play(); got != "\a" {
		t.Errorf("Play = %q, want the bell handed back", got)
	}
}

// Whether there is a player is decided up front, by looking, for the reason
// the notifier's is: found out by running it, the only fallback left is to
// print.
func TestThePlatformPlayerIsTheOneInstalled(t *testing.T) {
	tests := []struct {
		goos      string
		installed []string
		want      bool
	}{
		{"linux", []string{"paplay"}, true},
		{"linux", []string{"canberra-gtk-play"}, true},
		{"linux", nil, false},
		{"darwin", []string{"afplay"}, true},
		{"darwin", nil, false},
	}

	for _, tt := range tests {
		got := platformPlayer(tt.goos, installed(tt.installed...)) != nil
		if got != tt.want {
			t.Errorf("on %s with %q installed, a player: %v, want %v",
				tt.goos, tt.installed, got, tt.want)
		}
	}
}

// Players that are installed but fail — no sound server — are found out in
// the background, which has no business writing to the terminal. The last
// of them used to ring the bell there, mid-frame.
func TestFailingPlayersPrintNothing(t *testing.T) {
	failingPrograms(t, "paplay", "canberra-gtk-play")
	play := platformPlayer("linux", exec.LookPath)
	if play == nil {
		t.Fatal("precondition: the stand-in players were not found")
	}

	if out := stdout(t, play); out != "" {
		t.Errorf("failing players wrote %q to the terminal", out)
	}
}

// The bell is the sound, degraded, and is limited the same way: a burst
// rings once, and the interval after, it rings again.
func TestTheBellIsLimitedLikeTheSound(t *testing.T) {
	s, clock := soundPlayer(newProcess())
	s.play = nil

	var rang int
	for range 50 {
		if s.Play() == "\a" {
			rang++
		}
	}
	if rang != 1 {
		t.Errorf("a burst of 50 rang the bell %d times, want 1", rang)
	}

	*clock = clock.Add(minSoundInterval)
	if got := s.Play(); got != "\a" {
		t.Errorf("a message the interval later got %q, want the bell", got)
	}
}

// The limit is on bursts, not on conversation: a message the interval
// after the last one sounds again.
func TestASoundAfterTheIntervalPlays(t *testing.T) {
	p := newProcess()
	p.exit()
	s, clock := soundPlayer(p)

	s.Play()
	s.wait()
	*clock = clock.Add(minSoundInterval)
	s.Play()
	s.Close()

	if runs, _ := p.report(); len(runs) != 2 {
		t.Errorf("two sounds %v apart started %d players, want 2", minSoundInterval, len(runs))
	}
}
