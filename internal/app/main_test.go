package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile so the frame assertions can see colour at
// all. Under the default `go test` profile lipgloss resolves to Ascii and
// Style.Render is the identity function, which makes every screen this
// package renders colourless — and a colourless screen cannot be checked for
// the one property the frame is responsible for, that no cell falls through
// to the terminal's own background.
//
// True colour rather than 256, because this package builds its roles the way
// the app does — from the environment — and the two darkest surfaces are
// three units apart: bg #0b0d10 and panel #0e1116 both quantise to 232, so
// under a 256 profile a test asking "is the thread on bg and the list on
// panel" cannot tell the two apart on a developer machine that reports
// truecolour, and can on one that does not.
//
// It also points the config file and the default config directory at a
// directory of the run's own. :theme lists the themes/ it finds there and
// writes the file back, and a test that ran it without setting both would
// otherwise read — and rewrite — the developer's real ~/.config/tele-tui.
// A test that wants a config sets its own over these.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	dir, err := os.MkdirTemp("", "tele-tui-app-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	os.Setenv("TELETUI_CONFIG", filepath.Join(dir, "config.toml"))

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
