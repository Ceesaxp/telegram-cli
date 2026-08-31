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
//
// True colour rather than 256, so a test can hand this package a hex palette
// and read back exactly what it gave. The roles this package's other tests
// use are 256-colour codes, which lipgloss emits as 38;5;N under either
// profile, so nothing else changes.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
