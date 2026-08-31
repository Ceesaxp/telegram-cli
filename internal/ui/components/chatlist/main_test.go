package chatlist

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile so this package's styling assertions mean
// something. Without it lipgloss resolves a test binary to Ascii and
// Style.Render is the identity function — which is how a chat row shipped
// for four phases with a background that died at column two, under a test
// suite that could not see colour at all.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	os.Exit(m.Run())
}
