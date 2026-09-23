package palette

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile. lipgloss probes the output for a terminal,
// and under `go test` there is none, so it resolves to Ascii and
// Style.Render is the identity function — an assertion that a row is drawn
// in some role's colour, or that SetRoles changed what is drawn, would then
// pass on a palette drawn in no colour at all.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
