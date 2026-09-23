package app

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The path a value takes from the palette to a command's Run, driven by
// :theme, the command that offers values: the themes a world of the test's
// own holds (see newThemeWorld), with dark, the one on screen, first.

// typeKeys sends each rune of s to the app as its own keypress.
func typeKeys(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = update(t, m, string(r))
	}
	return m
}

// listedValues is what the open palette lists for an argument, in order.
func listedValues(m Model) []string {
	var out []string
	for _, a := range m.palette.ArgMatches() {
		out = append(out, a.Value)
	}
	return out
}

// TestPaletteItemsCarryTheCandidates: the projection hands the palette a
// command's values, and nothing to a command that has none.
func TestPaletteItemsCarryTheCandidates(t *testing.T) {
	m := newThemeWorld(t, "", map[string]string{"nord": nordTheme}).app(t)

	for _, it := range m.paletteItems() {
		switch it.Name {
		case "theme":
			if want := m.themeCandidates(); !reflect.DeepEqual(it.Candidates, want) {
				t.Errorf("theme offers %+v, want %+v", it.Candidates, want)
			}
		default:
			if it.Candidates != nil {
				t.Errorf("%q offers %+v, but has no candidates", it.Name, it.Candidates)
			}
		}
	}
}

// TestACommandRunsWithTheChosenValue is the whole path: `:` opens the
// palette, a partial argument filters the values, Enter closes it, and the
// command's Run hears the value that was highlighted — not the letters that
// were typed, which name no theme.
func TestACommandRunsWithTheChosenValue(t *testing.T) {
	m := newThemeWorld(t, "", map[string]string{"nord": nordTheme, "gruvbox": nordTheme}).app(t)

	m = update(t, m, ":")
	m = typeKeys(t, m, "theme gr")
	m = update(t, m, "\r")

	if m.palette.IsVisible() {
		t.Error("the palette stayed open after running the command")
	}
	if m.themeName != "gruvbox" {
		t.Errorf("the theme is %q, want the highlighted gruvbox", m.themeName)
	}
}

// TestTheArrowsChooseTheValueThatRuns: moving the highlight changes what
// Enter runs, since what runs is what is highlighted.
func TestTheArrowsChooseTheValueThatRuns(t *testing.T) {
	m := newThemeWorld(t, "", map[string]string{"nord": nordTheme}).app(t)

	m = update(t, m, ":")
	m = typeKeys(t, m, "theme ")
	m = update(t, m, "\x1b[B") // down from dark, to light
	m = update(t, m, "\r")

	if m.themeName != "light" {
		t.Errorf("the theme is %q, want light", m.themeName)
	}
}

// TestAnUnmatchedValueReachesTheCommandAsTyped: the command is the one that
// knows what a bad value is, so it has to hear one to say so.
func TestAnUnmatchedValueReachesTheCommandAsTyped(t *testing.T) {
	m := newThemeWorld(t, "", map[string]string{"nord": nordTheme}).app(t)

	m = update(t, m, ":")
	m = typeKeys(t, m, "theme durian")
	m = update(t, m, "\r")

	m.hintBar.SetWidth(200)
	if !strings.Contains(m.hintBar.View(), `"durian"`) {
		t.Errorf("the notice does not name the typed durian:\n%s", m.hintBar.View())
	}
}

// TestTheValuesAreReadWhenThePaletteOpens: the list reflects the world as
// it is when `:` is pressed — a theme file dropped in since startup, or
// since the palette was last open, is there to choose.
func TestTheValuesAreReadWhenThePaletteOpens(t *testing.T) {
	w := newThemeWorld(t, "", map[string]string{"nord": nordTheme})
	m := w.app(t)

	m = update(t, m, ":")
	m = update(t, m, "\x1b") // close it again

	writeFile(t, filepath.Join(w.themesDir, "quince.toml"), nordTheme)
	m = update(t, m, ":")
	m = typeKeys(t, m, "theme ")

	if got, want := listedValues(m), []string{"dark", "light", "nord", "quince"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the reopened palette lists %v, want %v", got, want)
	}
}
