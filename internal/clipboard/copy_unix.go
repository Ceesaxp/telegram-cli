//go:build linux || freebsd || openbsd || netbsd || dragonfly

package clipboard

import (
	"os"
	"os/exec"
)

// writeText picks a helper the same way pickReader does, and in the same
// order: the Wayland tool when the session is Wayland, then xclip, then
// wl-copy anyway. XWayland means xclip often works under Wayland too, so
// "have I got a display server of this kind" is a better question than
// "which binary exists".
func writeText(text string) error {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wl-copy"); err == nil {
			return runCopy(text, "wl-copy")
		}
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		return runCopy(text, "xclip", "-selection", "clipboard")
	}
	if _, err := exec.LookPath("xsel"); err == nil {
		return runCopy(text, "xsel", "--clipboard", "--input")
	}
	if _, err := exec.LookPath("wl-copy"); err == nil {
		return runCopy(text, "wl-copy")
	}
	return ErrNoWriter
}
