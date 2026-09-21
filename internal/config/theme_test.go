package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
