package help

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile.
//
// Without one lipgloss resolves a test binary to Ascii, Style.Render is the
// identity function, and every assertion this package makes about a colour
// passes because none was emitted. That is how this card came to draw its
// binding rows with the panel surface dying at the first styled span — the
// same defect the panels were fixed for — under a green suite.
//
// The sixth package to need this. The pattern is now familiar enough to be
// worth stating: a component package that renders anything needs one of
// these on the day it is created.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	os.Exit(m.Run())
}
