package notification

import (
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
