package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/telegram"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/hintbar"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
)

// themeWorld is a config of the test's own, where the app would look for
// one: TELETUI_CONFIG names its config.toml, XDG_CONFIG_HOME is beside it,
// and its themes/ holds the theme files the test wrote. Never the
// developer's ~/.config.
type themeWorld struct {
	root, configPath, themesDir string
}

func newThemeWorld(t *testing.T, config string, themes map[string]string) themeWorld {
	t.Helper()
	root := t.TempDir()
	w := themeWorld{
		root:       root,
		configPath: filepath.Join(root, "profile", "config.toml"),
		themesDir:  filepath.Join(root, "profile", "themes"),
	}
	t.Setenv("TELETUI_CONFIG", w.configPath)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	// Depth comes from the environment, as it does for the real app, and
	// truecolour keeps a theme's hex values as written.
	t.Setenv("COLORTERM", "truecolor")
	writeFile(t, w.configPath, config)
	for name, body := range themes {
		writeFile(t, filepath.Join(w.themesDir, name+".toml"), body)
	}
	return w
}

// app is the app as main builds it — config.Load, then New — on the main
// screen.
func (w themeWorld) app(t *testing.T) Model {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return onMainScreen(New(cfg, nil, store.NewStore(), telegram.NewTUIAuthorizer(cfg)), PanelChatList)
}

// file is what config.toml holds now.
func (w themeWorld) file(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(w.configPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// backedUp reports whether anything wrote config.toml.bak.
func (w themeWorld) backedUp() bool {
	_, err := os.Stat(w.configPath + ".bak")
	return err == nil
}

// A theme that sets one colour, so it can be told from its base.
const nordTheme = "[colors]\ncyan = \"#123456\"\n"

// TestThemeWithNoArgumentSaysWhichIsOn: `:theme` alone answers the question
// before anybody switches anything.
func TestThemeWithNoArgumentSaysWhichIsOn(t *testing.T) {
	tests := []struct {
		name, config, want string
	}{
		{"a theme file", "[ui]\ntheme = \"nord\"\n", "theme: nord"},
		{"a builtin", "[ui]\ntheme = \"light\"\n", "theme: light"},
		{"nothing configured", "", "theme: dark"},
		{"a theme that could not be used", "[ui]\ntheme = \"nosuch\"\n",
			`theme: dark (ui.theme "nosuch" could not be used)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newThemeWorld(t, tt.config, map[string]string{"nord": nordTheme}).app(t)
			if _, _, notice := m.runCommandLine("theme"); notice != tt.want {
				t.Errorf("notice = %q, want %q", notice, tt.want)
			}
		})
	}
}

// TestThemeAppliesAThemeFileAndSavesIt is the whole switch: the palette on
// screen is the theme's, config.toml says so in the one line that changed,
// the file as it was is backed up, and :theme reports the new theme.
func TestThemeAppliesAThemeFileAndSavesIt(t *testing.T) {
	original := "# mine\n[ui]\nrail = true\ntheme = \"dark\"  # the palette\n"
	w := newThemeWorld(t, original, map[string]string{"nord": nordTheme})
	m := w.app(t)
	if string(m.roles.Cyan) == "#123456" {
		t.Fatal("precondition: the app starts in the theme it is about to switch to")
	}

	m, _, notice := m.runCommandLine("theme nord")

	if notice != "theme: nord" {
		t.Errorf("notice = %q, want %q", notice, "theme: nord")
	}
	if string(m.roles.Cyan) != "#123456" {
		t.Errorf("the app's cyan is %q, want the theme's #123456", m.roles.Cyan)
	}
	// And a component draws it: the hint bar's keys are cyan.
	m.hintBar.SetWidth(80)
	m.hintBar.SetHints([]hintbar.Hint{{Key: "q", Label: "quit"}})
	if bar := m.hintBar.View(); !strings.Contains(bar, foregroundSeq(m.roles.Cyan)) {
		t.Errorf("the hint bar does not draw the theme's cyan:\n%s", strings.ReplaceAll(bar, "\x1b", "ESC"))
	}

	if got, want := w.file(t), "# mine\n[ui]\nrail = true\ntheme = 'nord'  # the palette\n"; got != want {
		t.Errorf("config.toml is\n%q\nwant\n%q", got, want)
	}
	if data, err := os.ReadFile(w.configPath + ".bak"); err != nil || string(data) != original {
		t.Errorf("the backup holds %q (err %v), want the original", data, err)
	}
	if m.config.UI.Theme != "nord" {
		t.Errorf("the running config's ui.theme is %q, want nord", m.config.UI.Theme)
	}
	if _, _, notice := m.runCommandLine("theme"); notice != "theme: nord" {
		t.Errorf(":theme afterwards says %q, want %q", notice, "theme: nord")
	}
}

// TestThemeAppliesABuiltin: the builtins switch the same way as files do.
func TestThemeAppliesABuiltin(t *testing.T) {
	w := newThemeWorld(t, "[ui]\ntheme = \"dark\"\n", nil)
	m := w.app(t)

	m, _, notice := m.runCommandLine("theme light")

	if notice != "theme: light" {
		t.Errorf("notice = %q, want %q", notice, "theme: light")
	}
	if want, _, _ := theme.RolesForSpec(nil, config.ThemeLight, true); m.roles != want {
		t.Errorf("the app's palette is not light:\n got %+v\nwant %+v", m.roles, want)
	}
	if got, want := w.file(t), "[ui]\ntheme = 'light'\n"; got != want {
		t.Errorf("config.toml is %q, want %q", got, want)
	}
}

// TestThemeKeepsTheThemeWhenTheNameIsNotOne: the loader's answer to a name
// it cannot use is dark, which is right at startup — there is nothing else
// to draw — and wrong here, where the theme on screen is working fine. So
// nothing is applied, nothing is written, and the notice says why.
func TestThemeKeepsTheThemeWhenTheNameIsNotOne(t *testing.T) {
	tests := []struct {
		name, arg, mentions string
	}{
		{"no such theme", "nosuch", "nosuch"},
		{"not a theme name", "a:b", "a:b"},
		{"a theme file that is not TOML", "broken", "broken.toml"},
		{"a path to nothing", "/nowhere/theme.toml", "/nowhere/theme.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := "[ui]\ntheme = \"nord\"\n"
			w := newThemeWorld(t, original, map[string]string{"nord": nordTheme, "broken": "[colors\n"})
			m := w.app(t)
			before := m.roles

			m, _, notice := m.runCommandLine("theme " + tt.arg)

			if !strings.HasPrefix(notice, "⚠") || !strings.Contains(notice, tt.mentions) ||
				!strings.HasSuffix(notice, "keeping nord") {
				t.Errorf("notice = %q, want a warning naming %q that keeps nord", notice, tt.mentions)
			}
			if strings.Contains(notice, "using dark") {
				t.Errorf("notice = %q says it is using dark, and it is not", notice)
			}
			if m.roles != before {
				t.Error("the palette changed")
			}
			if got := w.file(t); got != original || w.backedUp() {
				t.Errorf("config.toml was written: %q", got)
			}
			if _, _, notice := m.runCommandLine("theme"); notice != "theme: nord" {
				t.Errorf(":theme afterwards says %q, want nord still", notice)
			}
		})
	}
}

// TestThemeCountsItsWarnings: a theme that loads with problems is applied —
// most of it is fine — and the notice says how many problems there were and
// what the first one is. Both halves of the loader have their say: the
// file's (a key it does not know) and the palette's (a value that is not a
// colour).
func TestThemeCountsItsWarnings(t *testing.T) {
	body := "stray = 1\n[colors]\ncyan = \"#123456\"\nred = \"not a colour\"\nnope = \"#000000\"\n"
	w := newThemeWorld(t, "[ui]\ntheme = \"dark\"\n", map[string]string{"warm": body})
	m := w.app(t)

	configDir, defaultDir := m.config.ThemeDirs()
	spec, builtin, loadWarnings := config.LoadTheme("warm", configDir, defaultDir)
	_, _, paletteWarnings := theme.RolesForSpec(spec, builtin, true)
	if len(loadWarnings) == 0 || len(paletteWarnings) == 0 {
		t.Fatalf("precondition: want warnings from both halves, got %q and %q", loadWarnings, paletteWarnings)
	}
	all := append(loadWarnings, paletteWarnings...)

	m, _, notice := m.runCommandLine("theme warm")

	want := fmt.Sprintf("⚠ theme: warm · %d warnings: %s", len(all), all[0])
	if notice != want {
		t.Errorf("notice = %q\nwant %q", notice, want)
	}
	if string(m.roles.Cyan) != "#123456" {
		t.Error("a theme with warnings was not applied")
	}
	if got := w.file(t); got != "[ui]\ntheme = 'warm'\n" {
		t.Errorf("config.toml is %q, want the theme saved", got)
	}
}

// TestThemeSaysWhenItCouldNotSave: the theme is on screen either way — the
// file is the part that failed, and the notice says so, and why.
func TestThemeSaysWhenItCouldNotSave(t *testing.T) {
	original := "ui.theme = \"dark\"\n"
	w := newThemeWorld(t, original, map[string]string{"nord": nordTheme})
	m := w.app(t)

	m, _, notice := m.runCommandLine("theme nord")

	if !strings.HasPrefix(notice, "⚠ theme: nord (not saved: ") || !strings.Contains(notice, "by hand") {
		t.Errorf("notice = %q, want it to say nord is on but not saved, and why", notice)
	}
	if string(m.roles.Cyan) != "#123456" {
		t.Error("the theme was not applied")
	}
	if got := w.file(t); got != original {
		t.Errorf("config.toml was written: %q", got)
	}
}

// TestThemeOnTheCurrentThemeIsUnchanged: choosing what is already on is not
// a switch, so nothing is written — not even a backup.
func TestThemeOnTheCurrentThemeIsUnchanged(t *testing.T) {
	original := "[ui]\ntheme = \"nord\"\n"
	w := newThemeWorld(t, original, map[string]string{"nord": nordTheme})
	m := w.app(t)

	for _, arg := range []string{"nord", "NORD"} {
		if _, _, notice := m.runCommandLine("theme " + arg); notice != "theme: nord (unchanged)" {
			t.Errorf(":theme %s says %q, want %q", arg, notice, "theme: nord (unchanged)")
		}
	}
	if got := w.file(t); got != original || w.backedUp() {
		t.Errorf("config.toml was written: %q", got)
	}
}

// TestThePaletteOpensOnTheCurrentTheme: `:theme ` lists the themes with the
// one on screen highlighted and marked, so Enter straight away is the
// no-op it looks like, not a switch to whatever is listed first.
func TestThePaletteOpensOnTheCurrentTheme(t *testing.T) {
	original := "[ui]\ntheme = \"nord\"\n"
	w := newThemeWorld(t, original, map[string]string{"nord": nordTheme, "amber": nordTheme})
	m := w.app(t)

	m = update(t, m, ":")
	m = typeKeys(t, m, "theme ")

	if got, ok := m.palette.SelectedArg(); !ok || got.Value != "nord" {
		t.Fatalf("the highlight is on %q (ok=%v), want the current nord", got.Value, ok)
	}
	for _, a := range m.palette.ArgMatches() {
		if a.Current != (a.Value == "nord") {
			t.Errorf("%s is marked current = %v", a.Value, a.Current)
		}
	}

	m = update(t, m, "\r")

	m.hintBar.SetWidth(120)
	if !strings.Contains(m.hintBar.View(), "theme: nord (unchanged)") {
		t.Errorf("the hint bar does not say the theme is unchanged:\n%s", m.hintBar.View())
	}
	if got := w.file(t); got != original || w.backedUp() {
		t.Errorf("config.toml was written: %q", got)
	}
}

// TestThemeListsEveryThemeWithWhereItLives: the builtins say so, and a file
// says which directory it is in, home shortened to ~.
func TestThemeListsEveryThemeWithWhereItLives(t *testing.T) {
	w := newThemeWorld(t, "", map[string]string{"nord": nordTheme})
	t.Setenv("HOME", w.root)
	t.Setenv("USERPROFILE", w.root)
	m := w.app(t)

	m = update(t, m, ":")
	m = typeKeys(t, m, "theme ")

	got := map[string]string{}
	for _, a := range m.palette.ArgMatches() {
		got[a.Value] = a.Description
	}
	want := map[string]string{
		"dark":  "built in",
		"light": "built in",
		"nord":  "~" + string(filepath.Separator) + filepath.Join("profile", "themes"),
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("listed %v, want %v", got, want)
	}
}

// backups is every backup of config.toml there is, first and timestamped.
func (w themeWorld) backups(t *testing.T) []string {
	t.Helper()
	found, err := filepath.Glob(w.configPath + ".bak*")
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// readBackup is what a backup holds.
func readBackup(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestThemeBacksUpOnceASession: the backup is there to keep the file as it
// was before tele-tui touched it. Trying themes one after another rewrites
// a line tele-tui already wrote, and each of those needs no copy of its
// own — nine themes tried used to be nine timestamped backups.
func TestThemeBacksUpOnceASession(t *testing.T) {
	original := "# mine\n[ui]\ntheme = \"dark\"\n"
	w := newThemeWorld(t, original, map[string]string{"nord": nordTheme, "amber": nordTheme})
	m := w.app(t)

	for _, name := range []string{"nord", "amber", "light"} {
		var notice string
		if m, _, notice = m.runCommandLine("theme " + name); notice != "theme: "+name {
			t.Fatalf(":theme %s says %q", name, notice)
		}
	}

	if got := w.file(t); got != "# mine\n[ui]\ntheme = 'light'\n" {
		t.Errorf("config.toml is %q, want the last theme saved", got)
	}
	backups := w.backups(t)
	if len(backups) != 1 || backups[0] != w.configPath+".bak" {
		t.Fatalf("three switches left the backups %v, want config.toml.bak alone", backups)
	}
	if got := readBackup(t, backups[0]); got != original {
		t.Errorf("the backup holds %q, want the file as it was before the session, %q", got, original)
	}
}

// TestLaterSessionsMakeNoBackup: config.toml.bak is the file as it was
// before tele-tui first edited it, and once it is there no session adds
// another — a timestamped copy a session was one more file holding the
// api_hash, and none of them the user's own.
func TestLaterSessionsMakeNoBackup(t *testing.T) {
	original := "[ui]\ntheme = \"dark\"\n"
	w := newThemeWorld(t, original, map[string]string{"nord": nordTheme, "amber": nordTheme})

	for session := range 3 {
		m := w.app(t)
		for _, name := range []string{"nord", "amber", "light"} {
			var notice string
			if m, _, notice = m.runCommandLine("theme " + name); notice != "theme: "+name {
				t.Fatalf("session %d, :theme %s says %q", session, name, notice)
			}
		}
	}

	if backups := w.backups(t); len(backups) != 1 || backups[0] != w.configPath+".bak" {
		t.Fatalf("three sessions left the backups %v, want config.toml.bak alone", backups)
	}
	if got := readBackup(t, w.configPath+".bak"); got != original {
		t.Errorf("the backup holds %q, want the original %q", got, original)
	}
}

// TestAFailedSaveStillCountsItsBackup: a save that backed up and then did
// not read back — restored — has kept the original all the same, so the
// session does not back up again: not on a retry, and not after the backup
// is deleted, when the file is no longer the user's untouched one.
func TestAFailedSaveStillCountsItsBackup(t *testing.T) {
	w := newThemeWorld(t, "[ui]\ntheme = \"dark\"\n", map[string]string{"nord": nordTheme, "amber": nordTheme})
	m := w.app(t)
	// Valid TOML with a theme line to edit, and a setting the loader
	// cannot read: the edit goes in, the read-back fails, it is restored.
	unloadable := "[ui]\ntheme = \"dark\"\nrail = \"yes\"\n"
	writeFile(t, w.configPath, unloadable)

	m, _, notice := m.runCommandLine("theme nord")
	if !strings.Contains(notice, "not saved") {
		t.Fatalf("precondition: the save went through: %q", notice)
	}
	if got := w.file(t); got != unloadable || readBackup(t, w.configPath+".bak") != unloadable {
		t.Fatalf("precondition: the file %q, the backup %q; want both the original", got, readBackup(t, w.configPath+".bak"))
	}

	if err := os.Remove(w.configPath + ".bak"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.configPath, "[ui]\ntheme = \"dark\"\n")
	m, _, _ = m.runCommandLine("theme amber")

	if backups := w.backups(t); len(backups) != 0 {
		t.Errorf("the session backed up again after a save that had: %v", backups)
	}
}

// TestASaveThatDidNotHappenDoesNotUseUpTheBackup: a refused save leaves the
// file as the user wrote it, so the first save that does go through is
// still the first touch, and backs up.
func TestASaveThatDidNotHappenDoesNotUseUpTheBackup(t *testing.T) {
	w := newThemeWorld(t, "ui.theme = \"dark\"\n", map[string]string{"nord": nordTheme})
	m := w.app(t)
	m, _, notice := m.runCommandLine("theme nord")
	if !strings.Contains(notice, "not saved") {
		t.Fatalf("precondition: the dotted key was saved over: %q", notice)
	}

	fixed := "[ui]\ntheme = \"nord\"\n"
	writeFile(t, w.configPath, fixed)
	m, _, _ = m.runCommandLine("theme light")

	if backups := w.backups(t); len(backups) != 1 || readBackup(t, backups[0]) != fixed {
		t.Errorf("backups %v; want one, holding the file the save found", backups)
	}
}

// TestAThemeTheListCannotNameIsListedAsCurrent: a theme set by its path is
// not a themes/ file, and neither is one whose file has gone since it was
// loaded, so neither is among the names the list is built from. It is
// listed anyway, first, as the current theme — otherwise the highlight
// opens on dark, and Enter straight away switches to it and saves it.
func TestAThemeTheListCannotNameIsListedAsCurrent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mine.toml")
	writeFile(t, path, nordTheme)

	tests := []struct {
		name, config, current, where string
		after                        func(w themeWorld)
	}{
		{name: "a path", config: "[ui]\ntheme = \"" + path + "\"  # mine\n", current: path, where: "by path"},
		{name: "a theme file removed since", config: "[ui]\ntheme = \"nord\"\n", current: "nord",
			where: "not in themes/",
			after: func(w themeWorld) {
				if err := os.Remove(filepath.Join(w.themesDir, "nord.toml")); err != nil {
					t.Fatal(err)
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newThemeWorld(t, tt.config, map[string]string{"nord": nordTheme, "amber": nordTheme})
			m := w.app(t)
			if tt.after != nil {
				tt.after(w)
			}
			original := w.file(t)

			m = update(t, m, ":")
			m = typeKeys(t, m, "theme ")
			listed := m.palette.ArgMatches()
			if len(listed) == 0 || listed[0].Value != tt.current || !listed[0].Current || listed[0].Description != tt.where {
				t.Fatalf("the list starts %+v, want %q, current, %q", listed, tt.current, tt.where)
			}
			if got, _ := m.palette.SelectedArg(); got.Value != tt.current {
				t.Errorf("the highlight is on %q, want %q", got.Value, tt.current)
			}

			m = update(t, m, "\r")

			m.hintBar.SetWidth(400)
			if !strings.Contains(m.hintBar.View(), "(unchanged)") {
				t.Errorf("Enter straight away was not a no-op:\n%s", m.hintBar.View())
			}
			if m.themeName != tt.current {
				t.Errorf("the theme is %q now, want %q still", m.themeName, tt.current)
			}
			if got := w.file(t); got != original || w.backedUp() {
				t.Errorf("config.toml was written: %q", got)
			}
		})
	}
}
