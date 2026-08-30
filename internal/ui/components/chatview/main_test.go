package chatview

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
// identity function, and every colour this panel emits disappears from the
// output — an assertion on styled text then passes whatever the style was,
// including no style at all.
//
// It matters here beyond checking that colours are right: a hidden spoiler
// IS its colour. Without a profile, the assertion that a spoiler is drawn in
// its own background can only ever pass, on a screen showing the text in
// plain sight.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
