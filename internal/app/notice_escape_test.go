package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// runLine runs a command line the way a reader does: `:`, the line, Enter.
func runLine(t *testing.T, m Model, line string) Model {
	t.Helper()
	m = update(t, m, ":")
	m = typeKeys(t, m, line)
	return update(t, m, "\r")
}

// A notice quotes what it is about, and what it quotes can be anybody's: a
// theme file is made to be shared, and go-toml's complaint about a key
// defined twice prints the key as written. A key spelled as an escape
// sequence — a window title here, an OSC 52 clipboard write — must reach the
// hint bar and the composer as text, not as a sequence the terminal obeys.
// The filter is in notify, so every notice passes it, not only a theme's.
func TestANoticeCannotWriteAnEscapeSequence(t *testing.T) {
	const title, clipboard = "\\u001b]0;pwned\\u0007", "\\u001b]52;c;cHduZWQ=\\u0007"
	evil := func(key string) string {
		return "[colors]\n\"" + key + "\" = \"#000000\"\n\"" + key + "\" = \"#111111\"\n"
	}
	for name, key := range map[string]string{"a window title": title, "a clipboard write": clipboard} {
		t.Run(name, func(t *testing.T) {
			w := newThemeWorld(t, "[ui]\ntheme = \"dark\"\n", map[string]string{"evil": evil(key)})

			for _, how := range []struct {
				name string
				run  func(m Model) Model
			}{
				{":theme", func(m Model) Model { return runLine(t, m, "theme evil") }},
				{":reload-config", func(m Model) Model {
					writeFile(t, w.configPath, "[ui]\ntheme = \"evil\"\n")
					return runLine(t, m, "reload-config")
				}},
			} {
				m := how.run(w.app(t))
				// Wide enough to show the notice through to the key it quotes.
				m.hintBar.SetWidth(400)
				m.composer.SetSize(400, 3)
				for surface, view := range map[string]string{
					"hint bar": m.hintBar.View(), "composer": m.composer.View(),
				} {
					if strings.Contains(view, "\x1b]") || strings.Contains(view, "\a") {
						t.Errorf("%s: the %s draws an OSC sequence or a BEL:\n%s", how.name, surface,
							strings.NewReplacer("\x1b", "ESC", "\a", "BEL").Replace(view))
					}
					if !strings.Contains(ansi.Strip(view), "�]") {
						t.Errorf("%s: precondition: the %s does not show the notice quoting the key:\n%s",
							how.name, surface, ansi.Strip(view))
					}
				}
			}
		})
	}
}
