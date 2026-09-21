package notification

import (
	"fmt"
	"os/exec"
	"runtime"
)

// SoundPlayer plays notification sounds.
type SoundPlayer struct {
	enabled bool

	// play runs the platform's player once and returns when it has
	// finished. A field so a test can count what reaches it: the real
	// implementation is a process, gone before anything could ask.
	play func()
}

// NewSoundPlayer creates a new sound player.
func NewSoundPlayer(enabled bool) *SoundPlayer {
	return &SoundPlayer{enabled: enabled, play: platformPlayer(runtime.GOOS)}
}

// Play plays the notification sound.
func (s *SoundPlayer) Play() {
	if !s.enabled {
		return
	}
	go s.play()
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
