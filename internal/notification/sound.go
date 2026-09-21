package notification

import (
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// SoundPlayer plays notification sounds.
type SoundPlayer struct {
	enabled bool

	// play runs the platform's player once and returns when it has
	// finished. A field so a test can count what reaches it: the real
	// implementation is a process, gone before anything could ask. Nil
	// where there is no player installed.
	play func()
	// now is the clock the interval is measured on; a field so a test can
	// hold it still instead of sleeping through a real second.
	now func() time.Time
	// bells limits the bell rung where there is no player, on the same
	// clock.
	bells *bellLimiter

	mu     sync.Mutex
	busy   bool
	closed bool
	last   time.Time // when the last sound started
	player sync.WaitGroup
}

// minSoundInterval is the shortest time between the starts of two sounds.
//
// A burst — a busy group, or the backlog replayed after a reconnect —
// arrives within milliseconds, and a second is long enough for all of it to
// sound once. It is also short enough that a reply to something you have
// only just read still sounds. One-at-a-time already spaces afplay's Ping,
// which runs a second and a half; this is for a sound shorter than that, or
// a player that fails and returns at once.
const minSoundInterval = time.Second

// NewSoundPlayer creates a new sound player.
func NewSoundPlayer(enabled bool) *SoundPlayer {
	s := &SoundPlayer{
		enabled: enabled,
		play:    platformPlayer(runtime.GOOS, exec.LookPath),
		now:     time.Now,
	}
	// Through s rather than bound now, so the bell keeps to whatever clock
	// the player is on.
	s.bells = newBellLimiter(func() time.Time { return s.now() })
	return s
}

// Play plays the notification sound, unless one is already playing or the
// last started less than minSoundInterval ago. It never waits for the
// player: the caller is the event loop.
//
// It returns what the caller must write to the terminal: the bell, where
// there is no player to run, and "" otherwise. Like Notify's sequence, the
// caller hands it to tea.Raw rather than this writing it from a goroutine.
//
// A burst of messages used to start a player for each, all at once. The
// request that finds one playing is dropped rather than queued: the sound
// is about something having arrived, and the one playing already says so.
func (s *SoundPlayer) Play() string {
	if !s.enabled {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ""
	}
	if s.play == nil {
		// Nothing to run, so the terminal is asked to ring instead — by
		// the caller, like any other write to it.
		return s.bells.ring()
	}

	now := s.now()
	if s.busy || now.Sub(s.last) < minSoundInterval {
		return ""
	}
	s.busy, s.last = true, now
	s.player.Add(1)
	go s.run()
	return ""
}

// run plays the sound once, in the background: the player is a process,
// and waiting on it would stall the event loop for as long as it plays.
func (s *SoundPlayer) run() {
	defer s.player.Done()
	s.play()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.busy = false
}

// Close stops the player from starting anything new, and waits for one
// that is still running.
//
// Nothing needs it at exit, for the reason Notifier.Close gives: the
// goroutine lives only as long as the player does.
func (s *SoundPlayer) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	s.wait()
}

// wait returns once the player, if one is running, has finished.
func (s *SoundPlayer) wait() {
	s.player.Wait()
}

// soundPlayers are the commands that play the notification sound on each
// platform, in the order they are tried.
var soundPlayers = map[string][][]string{
	"linux": {
		{"paplay", "/usr/share/sounds/freedesktop/stereo/message.oga"},
		{"canberra-gtk-play", "-i", "message-new-instant"},
	},
	"darwin": {
		{"afplay", "/System/Library/Sounds/Ping.aiff"},
	},
}

// platformPlayer is the platform's sound player on goos: a function that
// plays the notification sound once, and nil where there is none to run.
//
// Whether there is one is looked up here, once, rather than found out by
// running it: by then the process is in the background, where the only
// fallback left is to print — to a terminal this process does not own.
func platformPlayer(goos string, lookPath func(string) (string, error)) func() {
	var installed [][]string
	for _, player := range soundPlayers[goos] {
		if _, err := lookPath(player[0]); err == nil {
			installed = append(installed, player)
		}
	}
	if len(installed) == 0 {
		return nil
	}
	return func() { playFirst(installed) }
}

// playFirst runs each player in turn until one succeeds, one at a time. If
// none does — installed, but no sound server to play through — nothing is
// said: the bell was the old answer to that, and the background is no place
// to ring it.
func playFirst(players [][]string) {
	for _, player := range players {
		if exec.Command(player[0], player[1:]...).Run() == nil {
			return
		}
	}
}
