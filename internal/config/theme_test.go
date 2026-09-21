package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestResolveThemeNameMatchesTheBuiltinsInAnyCase: `theme = "Light"`
// silently meant dark, because the match was case-sensitive. The builtin
// names are matched after trimming and lowercasing, like every other
// enumerated setting in this file.
func TestResolveThemeNameMatchesTheBuiltinsInAnyCase(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  ThemeResolution
	}{
		{"dark", ThemeResolution{Form: ThemeFormBuiltin, Name: "dark"}},
		{"light", ThemeResolution{Form: ThemeFormBuiltin, Name: "light"}},
		{"Light", ThemeResolution{Form: ThemeFormBuiltin, Name: "light"}},
		{"  DARK ", ThemeResolution{Form: ThemeFormBuiltin, Name: "dark"}},
	} {
		got := ResolveThemeName(tc.value, "", "")
		if got.Form != tc.want.Form || got.Name != tc.want.Name || got.Path != "" {
			t.Errorf("ResolveThemeName(%q) = %+v, want %+v", tc.value, got, tc.want)
		}
	}
}

// TestResolveThemeNameTakesAPathShapedValueAsAPath: a separator or a .toml
// suffix makes the value a path, resolved like every other path in
// config.toml — `~` expanded, anything relative left to the OS and the
// working directory. The case is kept: it is part of a file's name.
func TestResolveThemeNameTakesAPathShapedValueAsAPath(t *testing.T) {
	t.Setenv("HOME", "/home/reader")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		value, wantName, wantPath string
	}{
		{"/etc/tele-tui/Gruvbox.toml", "/etc/tele-tui/Gruvbox.toml", "/etc/tele-tui/Gruvbox.toml"},
		{" ~/themes/nord.toml ", "~/themes/nord.toml", filepath.Join(home, "themes/nord.toml")},
		{"Gruvbox.TOML", "Gruvbox.TOML", "Gruvbox.TOML"},
		{"themes/solarized", "themes/solarized", "themes/solarized"},
		// The escape hatch past a builtin name: rule 1 matches two exact
		// strings, so this one reaches the file.
		{"./themes/light.toml", "./themes/light.toml", "./themes/light.toml"},
	} {
		got := ResolveThemeName(tc.value, "/cfg", "/def")
		want := ThemeResolution{Form: ThemeFormPath, Name: tc.wantName, Path: tc.wantPath}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ResolveThemeName(%q) = %+v, want %+v", tc.value, got, want)
		}
	}
}

// TestResolveThemeNameSearchesTheConfigDirThenTheDefault: a plain name is
// themes/<name>.toml, next to the loaded config first and in the default
// config directory second — so a TELETUI_CONFIG=~/work.toml profile still
// finds the shared collection instead of looking in ~/themes/ alone.
func TestResolveThemeNameSearchesTheConfigDirThenTheDefault(t *testing.T) {
	got := ResolveThemeName(" Gruvbox ", "/profiles", "/xdg/tele-tui")
	want := ThemeResolution{
		Form: ThemeFormStem,
		Name: "gruvbox",
		Candidates: []string{
			filepath.Join("/profiles", "themes", "gruvbox.toml"),
			filepath.Join("/xdg/tele-tui", "themes", "gruvbox.toml"),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveThemeName = %+v, want %+v", got, want)
	}
}

// TestResolveThemeNameSearchesOneDirectoryOnce: with no TELETUI_CONFIG the
// loaded config IS the default one, and listing its themes directory twice
// would read as two places searched in the not-found warning.
func TestResolveThemeNameSearchesOneDirectoryOnce(t *testing.T) {
	got := ResolveThemeName("nord", "/xdg/tele-tui", "/xdg/./tele-tui/")
	want := []string{filepath.Join("/xdg/tele-tui", "themes", "nord.toml")}
	if !reflect.DeepEqual(got.Candidates, want) {
		t.Errorf("Candidates = %q, want %q", got.Candidates, want)
	}
}

// TestResolveThemeNameSkipsADirectoryItWasNotGiven: an empty directory is
// "no directory", not the working directory — filepath.Join would quietly
// make it the latter.
func TestResolveThemeNameSkipsADirectoryItWasNotGiven(t *testing.T) {
	got := ResolveThemeName("nord", "", "/def")
	want := []string{filepath.Join("/def", "themes", "nord.toml")}
	if !reflect.DeepEqual(got.Candidates, want) {
		t.Errorf("Candidates = %q, want %q", got.Candidates, want)
	}
}

// TestResolveThemeNameRejectsAStemThatIsNotAPlainName: a stem is spliced
// into a path, so only letters, digits, "-", "_" and "." pass, and not a
// leading "." — which rules out ".." and hidden files along with every
// character some filesystem gives a meaning to. Nothing is searched for a
// rejected stem, so nothing outside themes/ can be reached through one.
func TestResolveThemeNameRejectsAStemThatIsNotAPlainName(t *testing.T) {
	for _, value := range []string{"..", ".", ".hidden", "~", "solarized dark", "c:gruvbox", "nord\x00", "a*"} {
		got := ResolveThemeName(value, "/cfg", "/def")
		if got.Form != ThemeFormInvalid || got.Candidates != nil {
			t.Errorf("ResolveThemeName(%q) = %+v, want it rejected with nothing to search", value, got)
		}
	}
	// And the ones that are plain names, including non-ASCII letters.
	for _, value := range []string{"gruvbox-dark", "solarized_light", "tokyo.night", "тёмная", "base16"} {
		if got := ResolveThemeName(value, "/cfg", "/def"); got.Form != ThemeFormStem {
			t.Errorf("ResolveThemeName(%q) = %+v, want a stem", value, got)
		}
	}
}

// TestResolveThemeNameListsTheFilesABuiltinShadows: a builtin name always
// means the builtin, but a themes/dark.toml the reader dropped in expecting
// it to win deserves a warning — so a builtin carries the same candidates a
// stem would, for the one stat that warning costs.
func TestResolveThemeNameListsTheFilesABuiltinShadows(t *testing.T) {
	got := ResolveThemeName("Light", "/profiles", "/xdg/tele-tui")
	want := ThemeResolution{
		Form: ThemeFormBuiltin,
		Name: ThemeLight,
		Candidates: []string{
			filepath.Join("/profiles", "themes", "light.toml"),
			filepath.Join("/xdg/tele-tui", "themes", "light.toml"),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ResolveThemeName = %+v, want %+v", got, want)
	}
}

// TestResolveThemeNameReadsEmptyAsDark: an empty value is an older config,
// not a mistake (the ResolveComposeEditing precedent), so it is dark with
// nothing to look for — and therefore nothing to warn about.
func TestResolveThemeNameReadsEmptyAsDark(t *testing.T) {
	for _, value := range []string{"", "   "} {
		got := ResolveThemeName(value, "/cfg", "/def")
		want := ThemeResolution{Form: ThemeFormBuiltin, Name: ThemeDark}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ResolveThemeName(%q) = %+v, want %+v", value, got, want)
		}
	}
}

// --- the theme file reader -------------------------------------------------

// TestReadThemeFileReadsEverySection: the transport shape T2 converts from.
// Keys come through as written; which of them are roles is not this
// package's to know.
func TestReadThemeFileReadsEverySection(t *testing.T) {
	path := filepath.Join("testdata", "themes", "every-section.toml")
	got, warnings := readThemeFile(path)
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	want := &ThemeSpec{
		Source:    path,
		Inherit:   ThemeLight,
		Colors:    map[string]string{"bg": "#1d2021", "fg": "#ebdbb2", "cyan": "#8ec07c"},
		Colors256: map[string]string{"bg": "235", "fg": "223"},
		Ramp:      []string{"mauve", "cyan"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readThemeFile = %+v, want %+v", got, want)
	}
}

// writeTheme writes a theme file into a fresh directory and returns its path.
func writeTheme(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "theme.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestReadThemeFileTakesAnIntegerAsItsDigits: `bg = 235` is how everybody
// writes an xterm index. Decoded straight into strings it would fail the
// whole file; decoded as any and coerced, it is "235".
func TestReadThemeFileTakesAnIntegerAsItsDigits(t *testing.T) {
	path := writeTheme(t, "[colors256]\nbg = 235\nfg = \"223\"\n")
	got, warnings := readThemeFile(path)
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if got == nil {
		t.Fatal("readThemeFile returned no spec for a valid file")
	}
	want := map[string]string{"bg": "235", "fg": "223"}
	if !reflect.DeepEqual(got.Colors256, want) {
		t.Errorf("Colors256 = %q, want %q", got.Colors256, want)
	}
}

// TestReadThemeFileDropsAValueThatIsNeitherStringNorInteger: one bad value
// costs its role, not the file, and the reader is told which one and why.
func TestReadThemeFileDropsAValueThatIsNeitherStringNorInteger(t *testing.T) {
	path := writeTheme(t, "[colors]\nfg = 1.5\ncyan = \"#8ec07c\"\n\n[colors256]\nbg = true\n")
	got, warnings := readThemeFile(path)
	if got == nil {
		t.Fatalf("readThemeFile returned no spec; warnings %q", warnings)
	}
	if want := map[string]string{"cyan": "#8ec07c"}; !reflect.DeepEqual(got.Colors, want) {
		t.Errorf("Colors = %q, want %q", got.Colors, want)
	}
	if len(got.Colors256) != 0 {
		t.Errorf("Colors256 = %q, want the boolean dropped", got.Colors256)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %q, want one per dropped value", warnings)
	}
	for i, field := range []string{"colors.fg", "colors256.bg"} {
		if !strings.Contains(warnings[i], path) || !strings.Contains(warnings[i], field) {
			t.Errorf("warning %d = %q, want it to name %s and the file", i, warnings[i], field)
		}
	}
}

// TestReadThemeFileInheritsDarkByDefault: the spec always names its base,
// so the converter never has to know what an absent inherit meant.
func TestReadThemeFileInheritsDarkByDefault(t *testing.T) {
	for _, body := range []string{
		"[colors]\nbg = \"#000000\"\n",
		"[theme]\ninherit = \"\"\n[colors]\nbg = \"#000000\"\n",
		"[theme]\ninherit = \" Dark \"\n[colors]\nbg = \"#000000\"\n",
	} {
		got, warnings := readThemeFile(writeTheme(t, body))
		if got == nil || got.Inherit != ThemeDark || len(warnings) != 0 {
			t.Errorf("readThemeFile(%q) = %+v, %q; want Inherit dark and no warnings", body, got, warnings)
		}
	}
}

// TestReadThemeFileWarnsOnAnUnknownInherit: only a builtin can be
// inherited — no theme-file chains — so anything else is dark, said aloud.
func TestReadThemeFileWarnsOnAnUnknownInherit(t *testing.T) {
	for _, tc := range []struct{ inherit, shown string }{
		{`"solarized"`, `"solarized"`},
		{`"gruvbox.toml"`, `"gruvbox.toml"`},
		{`5`, `5`},
	} {
		path := writeTheme(t, "[theme]\ninherit = "+tc.inherit+"\n[colors]\nbg = \"#000000\"\n")
		got, warnings := readThemeFile(path)
		if got == nil || got.Inherit != ThemeDark {
			t.Errorf("inherit = %s: spec %+v, want Inherit dark", tc.inherit, got)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], path) ||
			!strings.Contains(warnings[0], "inherit = "+tc.shown) {
			t.Errorf("inherit = %s: warnings %q, want one naming the file and the value", tc.inherit, warnings)
		}
	}
}

// TestReadThemeFileDropsANonStringRampEntry: the ramp names roles, so an
// entry that is not a string cannot be one. It goes, the rest stay.
func TestReadThemeFileDropsANonStringRampEntry(t *testing.T) {
	path := writeTheme(t, "[senders]\nramp = [\"mauve\", 5, \"cyan\"]\n")
	got, warnings := readThemeFile(path)
	if got == nil {
		t.Fatalf("readThemeFile returned no spec; warnings %q", warnings)
	}
	if want := []string{"mauve", "cyan"}; !reflect.DeepEqual(got.Ramp, want) {
		t.Errorf("Ramp = %q, want %q", got.Ramp, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], path) || !strings.Contains(warnings[0], "senders.ramp") {
		t.Errorf("warnings = %q, want one naming the file and senders.ramp", warnings)
	}
}

// TestReadThemeFileIgnoresARampThatIsNotAList: `ramp = "mauve"` is one
// easy slip from `ramp = ["mauve"]`. Decoding the ramp as a list would fail
// the whole file over it; decoded as any, it costs the ramp and says so.
func TestReadThemeFileIgnoresARampThatIsNotAList(t *testing.T) {
	path := writeTheme(t, "[colors]\nbg = \"#000000\"\n\n[senders]\nramp = \"mauve\"\n")
	got, warnings := readThemeFile(path)
	if got == nil {
		t.Fatalf("readThemeFile returned no spec; warnings %q", warnings)
	}
	if got.Ramp != nil {
		t.Errorf("Ramp = %q, want nil so the default ramp applies", got.Ramp)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], path) || !strings.Contains(warnings[0], "senders.ramp") {
		t.Errorf("warnings = %q, want one naming the file and senders.ramp", warnings)
	}
}

// TestReadThemeFileWarnsOnceAboutAFileWithNoColours: a theme with nothing
// in it is well defined — it is its base — but it is almost certainly not
// what its author meant, so it is a spec and one warning.
func TestReadThemeFileWarnsOnceAboutAFileWithNoColours(t *testing.T) {
	for _, tc := range []struct{ body, base string }{
		{"", ThemeDark},
		{"# nothing yet\n", ThemeDark},
		{"[theme]\ninherit = \"light\"\n[colors]\n", ThemeLight},
	} {
		path := writeTheme(t, tc.body)
		got, warnings := readThemeFile(path)
		if got == nil {
			t.Errorf("readThemeFile(%q) returned no spec, want its base", tc.body)
			continue
		}
		if got.Inherit != tc.base || len(got.Colors) != 0 || len(got.Colors256) != 0 || got.Ramp != nil {
			t.Errorf("readThemeFile(%q) = %+v, want %s and nothing else", tc.body, got, tc.base)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], path) {
			t.Errorf("readThemeFile(%q) warnings = %q, want exactly one naming the file", tc.body, warnings)
		}
	}
}

// TestReadThemeFileReportsWhereItFailedToParse: a file that is not TOML
// is no theme at all, and "it did not parse" without a line number sends
// the reader hunting. The unquoted hex is the likeliest slip there is: in
// TOML a bare # starts a comment, so the value is missing.
func TestReadThemeFileReportsWhereItFailedToParse(t *testing.T) {
	path := writeTheme(t, "[colors]\nfg = \"#ebdbb2\"\nbg = #1d2021\n")
	got, warnings := readThemeFile(path)
	if got != nil {
		t.Errorf("readThemeFile = %+v, want no spec for a file that does not parse", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], path) || !strings.Contains(warnings[0], "line 3") {
		t.Errorf("warnings = %q, want one naming the file and line 3", warnings)
	}
}

// TestReadThemeFileFailsADuplicateKeyWhole: duplicate keys and tables are
// hard TOML errors, so the file fails like any other that does not parse.
// go-toml reports them as a plain error rather than a *toml.DecodeError, so
// there is no position to give — but the message names the key, and the
// warning must still say something rather than nothing.
func TestReadThemeFileFailsADuplicateKeyWhole(t *testing.T) {
	for _, tc := range []struct{ body, names string }{
		{"[colors]\nbg = \"#000000\"\nbg = \"#111111\"\n", "key bg"},
		{"[colors]\nbg = \"#000000\"\n\n[colors]\nfg = \"#111111\"\n", "table colors"},
	} {
		path := writeTheme(t, tc.body)
		got, warnings := readThemeFile(path)
		if got != nil {
			t.Errorf("readThemeFile(%q) = %+v, want no spec", tc.body, got)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], path) ||
			!strings.Contains(warnings[0], tc.names) || !strings.Contains(warnings[0], "already") {
			t.Errorf("readThemeFile(%q) warnings = %q, want one naming the file and %s", tc.body, warnings, tc.names)
		}
	}
}

// TestReadThemeFileRefusesADirectory: a directory where a theme should be
// is stat'd and named as what it is, not handed to a read that fails with
// something confusing.
func TestReadThemeFileRefusesADirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gruvbox.toml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	got, warnings := readThemeFile(path)
	if got != nil {
		t.Errorf("readThemeFile = %+v, want no spec", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], path) || !strings.Contains(warnings[0], "not a regular file") {
		t.Errorf("warnings = %q, want one saying the path is not a regular file", warnings)
	}
}

// TestReadThemeFileDoesNotBlockOnAFifo: opening a fifo for reading blocks
// until something writes to it, which at startup is forever. The stat comes
// first so the open never happens.
func TestReadThemeFileDoesNotBlockOnAFifo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme.toml")
	makeFifo(t, path)

	type result struct {
		spec     *ThemeSpec
		warnings []string
	}
	done := make(chan result, 1)
	go func() {
		spec, warnings := readThemeFile(path)
		done <- result{spec, warnings}
	}()
	select {
	case got := <-done:
		if got.spec != nil {
			t.Errorf("readThemeFile = %+v, want no spec", got.spec)
		}
		if len(got.warnings) != 1 || !strings.Contains(got.warnings[0], "not a regular file") {
			t.Errorf("warnings = %q, want one saying the fifo is not a regular file", got.warnings)
		}
	case <-time.After(5 * time.Second):
		// Unblock the reader so the test binary can exit.
		if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			f.Close()
		}
		t.Fatal("readThemeFile blocked on a fifo")
	}
}

// TestReadThemeFileRefusesAFileOverTheCap: a theme is a few dozen lines.
// Anything past 64 KiB is not one — a log, an image, a mistyped path — and
// is not read into memory to find that out.
func TestReadThemeFileRefusesAFileOverTheCap(t *testing.T) {
	body := "[colors]\nbg = \"#000000\"\n" + strings.Repeat("# padding\n", 64<<10/10)
	path := writeTheme(t, body)
	got, warnings := readThemeFile(path)
	if got != nil {
		t.Errorf("readThemeFile = %+v, want no spec for a %d-byte file", got, len(body))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], path) || !strings.Contains(warnings[0], "64 KiB") {
		t.Errorf("warnings = %q, want one naming the file and the cap", warnings)
	}

	// And exactly at the cap is still a theme.
	atCap := writeTheme(t, body[:64<<10])
	if got, warnings := readThemeFile(atCap); got == nil {
		t.Errorf("a file of exactly 64 KiB was refused: %q", warnings)
	}
}

// TestReadThemeFileWarnsAboutAFileThatIsNotThere: `theme = "~/nord.toml"`
// with no such file is a typo worth one line, not a silent dark palette.
func TestReadThemeFileWarnsAboutAFileThatIsNotThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nord.toml")
	got, warnings := readThemeFile(path)
	if got != nil {
		t.Errorf("readThemeFile = %+v, want no spec", got)
	}
	// The OS's own words for it, which differ between platforms.
	_, statErr := os.Stat(path)
	why := errors.Unwrap(statErr).Error()
	if len(warnings) != 1 || !strings.Contains(warnings[0], path) || !strings.Contains(warnings[0], why) {
		t.Errorf("warnings = %q, want one naming the file and why", warnings)
	}
}
