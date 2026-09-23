package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// listedNames is what the palette would show: the names, in order.
func listedNames(entries []ThemeEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

// builtinsOnly is the list with no theme files anywhere.
var builtinsOnly = []ThemeEntry{
	{Name: ThemeDark, Kind: ThemeKindBuiltin},
	{Name: ThemeLight, Kind: ThemeKindBuiltin},
}

// TestListThemesPutsTheBuiltinsFirst: dark and light are always there, and
// always first — they are the zero-config themes and the ones nothing can
// take away.
func TestListThemesPutsTheBuiltinsFirst(t *testing.T) {
	configDir, defaultDir := themeDirs(t)
	if got := ListThemes(configDir, defaultDir); !reflect.DeepEqual(got, builtinsOnly) {
		t.Errorf("with no theme files ListThemes = %+v, want %+v", got, builtinsOnly)
	}

	putTheme(t, configDir, "amber", "")
	got := ListThemes(configDir, defaultDir)
	if len(got) < 2 || !reflect.DeepEqual(got[:2], builtinsOnly) {
		t.Errorf("ListThemes = %+v, want the builtins first", got)
	}
}

// TestListThemesListsBothDirectoriesOnce: the config dir's themes and the
// default dir's, and a name in both is listed once, from the config dir —
// the copy `theme = "<name>"` would read.
func TestListThemesListsBothDirectoriesOnce(t *testing.T) {
	configDir, defaultDir := themeDirs(t)
	putTheme(t, configDir, "gruvbox", "")
	putTheme(t, configDir, "nord", "")
	putTheme(t, defaultDir, "gruvbox", "")
	putTheme(t, defaultDir, "dracula", "")

	want := append(builtinsOnly[:2:2],
		ThemeEntry{Name: "dracula", Kind: ThemeKindFile, Dir: filepath.Join(defaultDir, "themes")},
		ThemeEntry{Name: "gruvbox", Kind: ThemeKindFile, Dir: filepath.Join(configDir, "themes")},
		ThemeEntry{Name: "nord", Kind: ThemeKindFile, Dir: filepath.Join(configDir, "themes")},
	)
	if got := ListThemes(configDir, defaultDir); !reflect.DeepEqual(got, want) {
		t.Errorf("ListThemes = %+v\nwant %+v", got, want)
	}
}

// TestListThemesSortsTheFilesByName: one list, sorted, whichever directory
// each came from — the reader is choosing a theme, not a directory.
func TestListThemesSortsTheFilesByName(t *testing.T) {
	configDir, defaultDir := themeDirs(t)
	putTheme(t, configDir, "zenburn", "")
	putTheme(t, configDir, "ayu", "")
	putTheme(t, defaultDir, "monokai", "")
	putTheme(t, defaultDir, "everforest", "")

	want := []string{"dark", "light", "ayu", "everforest", "monokai", "zenburn"}
	if got := listedNames(ListThemes(configDir, defaultDir)); !reflect.DeepEqual(got, want) {
		t.Errorf("ListThemes names = %v, want %v", got, want)
	}
}

// TestListThemesSearchesOneDirectoryOnce: without TELETUI_CONFIG the config
// dir IS the default dir, and its themes are not listed twice.
func TestListThemesSearchesOneDirectoryOnce(t *testing.T) {
	configDir, _ := themeDirs(t)
	putTheme(t, configDir, "nord", "")

	want := []string{"dark", "light", "nord"}
	if got := listedNames(ListThemes(configDir, configDir)); !reflect.DeepEqual(got, want) {
		t.Errorf("ListThemes names = %v, want %v", got, want)
	}
}

// TestListThemesDoesNotListAFileABuiltinShadows: themes/dark.toml and
// themes/light.toml are never what `theme = "dark"` reads — the builtin
// wins — so offering them as themes of their own would offer something
// that cannot be chosen by that name.
func TestListThemesDoesNotListAFileABuiltinShadows(t *testing.T) {
	configDir, defaultDir := themeDirs(t)
	putTheme(t, configDir, "dark", "")
	putTheme(t, defaultDir, "light", "")

	if got := ListThemes(configDir, defaultDir); !reflect.DeepEqual(got, builtinsOnly) {
		t.Errorf("ListThemes = %+v, want the builtins alone: %+v", got, builtinsOnly)
	}
}

// TestListThemesSkipsWhatAThemeNameCannotReach: a file whose stem is not a
// plain name is never searched for, so it is not a theme `theme = "…"` can
// name, and listing it would offer a choice that only warns.
func TestListThemesSkipsWhatAThemeNameCannotReach(t *testing.T) {
	configDir, defaultDir := themeDirs(t)
	for _, name := range []string{
		".hidden",     // hidden, and starts with a dot
		"has space",   // not a plain name
		"semi;colon",  // not a plain name
		"double.toml", // themes/double.toml.toml: the stem is path-shaped
		"",            // themes/.toml
	} {
		putTheme(t, configDir, name, "")
	}
	putTheme(t, configDir, "solarized.light", "") // dots inside are fine

	want := []string{"dark", "light", "solarized.light"}
	if got := listedNames(ListThemes(configDir, defaultDir)); !reflect.DeepEqual(got, want) {
		t.Errorf("ListThemes names = %v, want %v", got, want)
	}
}

// TestListThemesSkipsDirectoriesAndOtherFiles: only a *.toml that is a file
// is a theme. A directory named like one, a README beside them, and a
// symlink to nothing are not — but a symlink to a theme is one, because
// that is how a dotfiles repository puts its themes in place.
func TestListThemesSkipsDirectoriesAndOtherFiles(t *testing.T) {
	configDir, defaultDir := themeDirs(t)
	themes := filepath.Join(configDir, "themes")
	mustWrite(t, filepath.Join(themes, "README.md"), "")
	mustWrite(t, filepath.Join(themes, "notes.txt"), "")
	mustWrite(t, filepath.Join(themes, "tomlless"), "")
	if err := os.Mkdir(filepath.Join(themes, "folder.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(themes, "gone.toml.orig"), filepath.Join(themes, "broken.toml")); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "kanagawa.toml")
	mustWrite(t, elsewhere, "")
	if err := os.Symlink(elsewhere, filepath.Join(themes, "kanagawa.toml")); err != nil {
		t.Fatal(err)
	}

	want := []string{"dark", "light", "kanagawa"}
	if got := listedNames(ListThemes(configDir, defaultDir)); !reflect.DeepEqual(got, want) {
		t.Errorf("ListThemes names = %v, want %v", got, want)
	}
}

// TestListThemesToleratesMissingDirectories: a config dir with no themes/,
// a default dir that does not exist, and a directory not given at all are
// all just places with no themes in them.
func TestListThemesToleratesMissingDirectories(t *testing.T) {
	configDir, defaultDir := themeDirs(t)
	putTheme(t, defaultDir, "nord", "")
	if err := os.Remove(filepath.Join(configDir, "themes")); err != nil {
		t.Fatal(err)
	}

	want := []string{"dark", "light", "nord"}
	if got := listedNames(ListThemes(configDir, defaultDir)); !reflect.DeepEqual(got, want) {
		t.Errorf("with no themes/ in the config dir ListThemes = %v, want %v", got, want)
	}
	if got := listedNames(ListThemes("", defaultDir)); !reflect.DeepEqual(got, want) {
		t.Errorf("with no config dir ListThemes = %v, want %v", got, want)
	}
	missing := filepath.Join(t.TempDir(), "nowhere")
	if got := ListThemes(missing, ""); !reflect.DeepEqual(got, builtinsOnly) {
		t.Errorf("with nothing to search ListThemes = %+v, want %+v", got, builtinsOnly)
	}
}

// TestListThemesListsOnlyWhatTheNameReads: a listed name has to be one that
// `theme = "<name>"` would load, from the directory the entry names. Names
// are lowercased like ui.theme values, so a Nord.toml is "nord" — listed
// where the filesystem folds case, since there "nord" reads it, and not
// where it does not, since there "nord" reads nothing.
func TestListThemesListsOnlyWhatTheNameReads(t *testing.T) {
	configDir, defaultDir := themeDirs(t)
	putTheme(t, configDir, "Nord", "")
	putTheme(t, defaultDir, "gruvbox", "")

	entries := ListThemes(configDir, defaultDir)
	for _, e := range entries {
		if e.Kind != ThemeKindFile {
			continue
		}
		r := ResolveThemeName(e.Name, configDir, defaultDir)
		want := filepath.Join(e.Dir, e.Name+".toml")
		if got := findTheme(r.Candidates); r.Form != ThemeFormStem || got != want {
			t.Errorf("listed %q from %s, but theme = %q reads %q", e.Name, e.Dir, e.Name, got)
		}
	}

	_, err := os.Stat(filepath.Join(configDir, "themes", "nord.toml"))
	foldsCase := err == nil
	listed := false
	for _, name := range listedNames(entries) {
		listed = listed || name == "nord"
		if name == "Nord" {
			t.Error(`listed "Nord": a theme name is lowercased, so the entry is "nord"`)
		}
	}
	if listed != foldsCase {
		t.Errorf(`"nord" listed = %v, but "nord" reads Nord.toml = %v on this filesystem`, listed, foldsCase)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
