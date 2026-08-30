package render

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile before any test in this package runs.
//
// lipgloss chooses its profile by probing the output for a terminal. Under
// `go test` there is none, so it resolves to Ascii, Style.Render becomes the
// identity function, and every colour and attribute this package emits
// disappears from the output. An assertion on styled text then passes
// whatever the style was — including no style at all, which is exactly the
// regression worth catching in a renderer.
//
// TrueColor rather than a narrower profile: the palette this package is
// handed may be either hex or 256-colour (see theme.RolesFor), and TrueColor
// renders both without quantising one into the other.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
