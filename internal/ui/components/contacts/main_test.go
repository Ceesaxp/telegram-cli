package contacts

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile so this package's styling assertions mean
// something. Without it lipgloss resolves a test binary to Ascii and
// Style.Render is the identity function — a row would then assert "paints
// nothing" and pass whether or not it painted, which is the trap the chat
// list next door documents having fallen into for four phases.
//
// The same profile as the chat list's, because these two surfaces draw the
// same grid into the same column and their tests have to be able to compare.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
