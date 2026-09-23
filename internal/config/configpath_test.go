package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadRemembersWhereItLooked: a theme listed or switched to later is
// searched for where the startup one was — the loaded file's directory, then
// the default one — and the file the config came from is the one written
// back to. A TELETUI_CONFIG profile elsewhere is exactly the case where the
// two directories differ.
func TestLoadRemembersWhereItLooked(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profiles", "work.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELETUI_CONFIG", path)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	// Recorded, not looked up again: what Load used stays what it used.
	t.Setenv("TELETUI_CONFIG", filepath.Join(root, "elsewhere.toml"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "elsewhere"))

	configDir, defaultDir := cfg.ThemeDirs()
	if want := filepath.Join(root, "profiles"); configDir != want {
		t.Errorf("config dir = %q, want the loaded file's %q", configDir, want)
	}
	if want := filepath.Join(root, "xdg", "tele-tui"); defaultDir != want {
		t.Errorf("default dir = %q, want %q", defaultDir, want)
	}
	if got := cfg.Path(); got != path {
		t.Errorf("Path() = %q, want the file Load read, %q", got, path)
	}
}

// TestWithNoConfigFileTheDefaultLocationStandsIn: a first run read no file,
// so its themes are the default directory's, and the file a write creates is
// the one Load would find next time.
func TestWithNoConfigFileTheDefaultLocationStandsIn(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("TELETUI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(xdg, "tele-tui")
	if configDir, defaultDir := cfg.ThemeDirs(); configDir != want || defaultDir != want {
		t.Errorf("ThemeDirs() = %q, %q, want %q twice", configDir, defaultDir, want)
	}
	if got, want := cfg.Path(), filepath.Join(want, "config.toml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

// TestAConfigLoadDidNotBuildLooksWhereLoadWould: a test's &Config{} has no
// record of a file, so it answers with where Load would look now.
func TestAConfigLoadDidNotBuildLooksWhereLoadWould(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profiles", "work.toml")
	t.Setenv("TELETUI_CONFIG", path)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	cfg := &Config{}
	configDir, defaultDir := cfg.ThemeDirs()
	if want := filepath.Join(root, "profiles"); configDir != want {
		t.Errorf("config dir = %q, want %q", configDir, want)
	}
	if want := filepath.Join(root, "xdg", "tele-tui"); defaultDir != want {
		t.Errorf("default dir = %q, want %q", defaultDir, want)
	}
	if got := cfg.Path(); got != path {
		t.Errorf("Path() = %q, want ConfigPath()'s %q", got, path)
	}
}
