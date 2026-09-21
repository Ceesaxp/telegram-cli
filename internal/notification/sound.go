package notification

import (
	"fmt"
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
	// implementation is a process, gone before anything could ask.
	play func()
	// now is the clock the interval is measured on; a field so a test can
	// hold it still instead of sleeping through a real second.
	now func() time.Time

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
	return &SoundPlayer{
		enabled: enabled,
		play:    platformPlayer(runtime.GOOS),
		now:     time.Now,
	}
}

// Play plays the notification sound, unless one is already playing or the
// last started less than minSoundInterval ago. It never waits for the
// player: the caller is the event loop.
//
// A burst of messages used to start a player for each, all at once. The
// request that finds one playing is dropped rather than queued: the sound
// is about something having arrived, and the one playing already says so.
func (s *SoundPlayer) Play() {
	if !s.enabled {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.closed || s.busy || now.Sub(s.last) < minSoundInterval {
		return
	}
	s.busy, s.last = true, now
	s.player.Add(1)
	go s.run()
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

// platformPlayer is the platform's sound player on goos: a function that
// plays the notification sound once.
func platformPlayer(goos string) func() {
	switch goos {
	case "linux":
		return playLinux
	case "darwin":
		return playMacOS
	default:
		return func() { fmt.Print("\a") }
	}
}

func playLinux() {
	// Try paplay with system sound.
	cmd := exec.Command("paplay", "/usr/share/sounds/freedesktop/stereo/message.oga")
	if err := cmd.Run(); err != nil {
		// Fallback: canberra-gtk-play.
		cmd = exec.Command("canberra-gtk-play", "-i", "message-new-instant")
		if err := cmd.Run(); err != nil {
			// Last resort: terminal bell.
			fmt.Print("\a")
		}
	}
}

func playMacOS() {
	cmd := exec.Command("afplay", "/System/Library/Sounds/Ping.aiff")
	cmd.Run()
}
