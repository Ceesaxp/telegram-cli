package cell

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile for the whole package.
//
// Without it lipgloss detects a test binary's non-tty stdout, resolves to
// Ascii, and Style.Render becomes the identity function — every assertion
// about a rendered colour then passes because nothing was ever emitted. The
// styling tests in this package are about escape sequences specifically, so
// with no profile they are not weak, they are empty.
//
// 256 rather than true colour because the sequences are shorter to read in a
// failure message and the ones under test are the same either way.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	os.Exit(m.Run())
}
