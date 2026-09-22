package palette

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
)

// truecolourSeq finds every 24-bit foreground or background in a rendered
// string, capturing it as "r;g;b".
var truecolourSeq = regexp.MustCompile(`[34]8;2;(\d+;\d+;\d+)`)

// TestSetRolesRedrawsInTheNewPalette: a theme applied while running reaches
// the palette through SetRoles, and everything it draws afterwards — the
// frame, the prompt, command rows and value rows alike — has to come from
// the palette it was handed last, not the one it was built with.
func TestSetRolesRedrawsInTheNewPalette(t *testing.T) {
	marker, known := theme.MarkerRoles()

	for _, typed := range []string{"", "theme "} {
		m := New(theme.DarkRoles(true))
		m.SetItems(themeItems())
		m.Open()
		m = typeString(t, m, typed)

		m.SetRoles(marker)
		view := m.View()

		found := truecolourSeq.FindAllStringSubmatch(view, -1)
		if len(found) == 0 {
			t.Fatalf("query %q: the palette drew no colour at all after SetRoles:\n%s",
				typed, strings.ReplaceAll(view, "\x1b", "ESC"))
		}
		for _, f := range found {
			if _, ok := known[f[1]]; !ok {
				t.Errorf("query %q: the palette drew rgb(%s), which is not in the palette "+
					"SetRoles gave it", typed, f[1])
			}
		}
	}
}
