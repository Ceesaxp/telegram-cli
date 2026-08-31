package topbar

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile.
//
// Without one lipgloss resolves a test binary to Ascii and Style.Render is
// the identity function, so every assertion this package makes about a colour
// passes because no colour was ever emitted. That is how "the active folder
// is bright and the others are dim" could be the only thing marking the open
// folder, be tested, and mark nothing on a row made of colour emoji — which
// ignore the foreground they are given.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	os.Exit(m.Run())
}
