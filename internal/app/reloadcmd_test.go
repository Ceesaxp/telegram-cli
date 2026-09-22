package app

import (
	"strings"
	"testing"
)

// TestReloadConfigAppliesTheThemeAndListsTheRest: the theme is the one
// setting a running app can take on, so it is applied; everything else that
// changed is named, by its key in the file, as waiting for a restart. The
// file is the source here, so nothing writes it — not even a backup.
func TestReloadConfigAppliesTheThemeAndListsTheRest(t *testing.T) {
	w := newThemeWorld(t, "[ui]\ntheme = \"dark\"\nparse_markdown = false\n", map[string]string{"nord": nordTheme})
	m := w.app(t)
	edited := "[ui]\ntheme = \"nord\"\nparse_markdown = true\n\n[keys]\ncompose = \"a\"\n"
	writeFile(t, w.configPath, edited)

	m, _, notice := m.runCommandLine("reload-config")

	want := "config reloaded · theme: nord · restart to apply: ui.parse_markdown, keys.compose"
	if notice != want {
		t.Errorf("notice = %q\nwant %q", notice, want)
	}
	if string(m.roles.Cyan) != "#123456" {
		t.Errorf("the app's cyan is %q, want the reloaded theme's #123456", m.roles.Cyan)
	}
	if got := w.file(t); got != edited || w.backedUp() {
		t.Errorf("config.toml was written: %q", got)
	}
	// What was not applied keeps its running value, so the app goes on
	// behaving as one config rather than half of two.
	if m.config.UI.ParseMarkdown || m.config.Keys.Compose == "a" {
		t.Errorf("the running config took settings it did not apply: parse_markdown %v, compose %q",
			m.config.UI.ParseMarkdown, m.config.Keys.Compose)
	}
	if _, _, notice := m.runCommandLine("theme"); notice != "theme: nord" {
		t.Errorf(":theme afterwards says %q, want %q", notice, "theme: nord")
	}
}

// TestReloadConfigRereadsTheThemeFile: a theme being tweaked keeps its
// name, and reloading is how the tweak reaches the screen.
func TestReloadConfigRereadsTheThemeFile(t *testing.T) {
	w := newThemeWorld(t, "[ui]\ntheme = \"nord\"\n", map[string]string{"nord": nordTheme})
	m := w.app(t)
	writeFile(t, w.themesDir+"/nord.toml", "[colors]\ncyan = \"#654321\"\n")

	m, _, notice := m.runCommandLine("reload-config")

	if notice != "config reloaded · theme: nord" {
		t.Errorf("notice = %q", notice)
	}
	if string(m.roles.Cyan) != "#654321" {
		t.Errorf("the app's cyan is %q, want the edited theme's #654321", m.roles.Cyan)
	}
}

// TestReloadConfigKeepsTheThemeWhenTheFileNamesNoUsableOne: as :theme does,
// and for its reason — the theme on screen is working.
func TestReloadConfigKeepsTheThemeWhenTheFileNamesNoUsableOne(t *testing.T) {
	w := newThemeWorld(t, "[ui]\ntheme = \"nord\"\n", map[string]string{"nord": nordTheme})
	m := w.app(t)
	before := m.roles
	writeFile(t, w.configPath, "[ui]\ntheme = \"nosuch\"\n")

	m, _, notice := m.runCommandLine("reload-config")

	if !strings.HasPrefix(notice, "⚠ config reloaded · ") || !strings.Contains(notice, `"nosuch"`) ||
		!strings.Contains(notice, "keeping nord") {
		t.Errorf("notice = %q, want the reload to say nosuch is not usable and nord stays", notice)
	}
	if m.roles != before {
		t.Error("the palette changed")
	}
	if m.config.UI.Theme != "nord" {
		t.Errorf("the running config's ui.theme is %q, want nord still", m.config.UI.Theme)
	}
}

// TestReloadConfigWithABrokenFileChangesNothing: a file that does not load
// is reported, and the app goes on exactly as it was.
func TestReloadConfigWithABrokenFileChangesNothing(t *testing.T) {
	w := newThemeWorld(t, "[ui]\ntheme = \"dark\"\n", map[string]string{"nord": nordTheme})
	m := w.app(t)
	before := m.roles
	broken := "[ui\ntheme = \"nord\"\n"
	writeFile(t, w.configPath, broken)

	m, _, notice := m.runCommandLine("reload-config")

	if !strings.HasPrefix(notice, "⚠ config not reloaded: ") {
		t.Errorf("notice = %q, want it to say the config was not reloaded, and why", notice)
	}
	if m.roles != before || m.config.UI.Theme != "dark" {
		t.Errorf("a failed reload changed the app: theme %q", m.config.UI.Theme)
	}
	if got := w.file(t); got != broken || w.backedUp() {
		t.Errorf("config.toml was written: %q", got)
	}
}
