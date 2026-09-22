package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestReloadReadsTheFileLoadRead: the same file, not whatever the
// environment would name now.
func TestReloadReadsTheFileLoadRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "work.toml")
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELETUI_CONFIG", path)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("TELETUI_CONFIG", filepath.Join(root, "elsewhere.toml"))
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"light\"\nrail = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fresh, err := cfg.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if fresh.UI.Theme != "light" || !fresh.UI.Rail {
		t.Errorf("Reload read theme %q, rail %v; want the edited file's light, true", fresh.UI.Theme, fresh.UI.Rail)
	}
	if fresh.Path() != path {
		t.Errorf("the reloaded config belongs to %q, want %q", fresh.Path(), path)
	}
	if cfg.UI.Theme != "dark" {
		t.Errorf("Reload changed the config it was called on: theme %q", cfg.UI.Theme)
	}
}

// TestReloadFindsAFileCreatedSince: a first run read no file, and one
// written since — by :theme, say — is where Load would find it next time,
// so that is where a reload reads it.
func TestReloadFindsAFileCreatedSince(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("TELETUI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(xdg, "tele-tui", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"light\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fresh, err := cfg.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if fresh.UI.Theme != "light" {
		t.Errorf("Reload read theme %q, want the new file's light", fresh.UI.Theme)
	}
}

// TestReloadSaysWhyAFileDoesNotLoad, and returns nothing to use in its
// place.
func TestReloadSaysWhyAFileDoesNotLoad(t *testing.T) {
	path := configFile(t, "[ui]\ntheme = \"dark\"\n")
	t.Setenv("TELETUI_CONFIG", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[ui\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fresh, err := cfg.Reload()
	if err == nil || fresh != nil {
		t.Errorf("Reload of a broken file = %v, %v; want an error and no config", fresh, err)
	}
}

// TestChangedSettingsNamesTheTOMLKeys: what a reader would look for in
// config.toml, in the order the file's tables and fields are declared.
func TestChangedSettingsNamesTheTOMLKeys(t *testing.T) {
	a, b := defaultConfig(), defaultConfig()
	b.Keys.Compose = "a"
	b.UI.ParseMarkdown = !a.UI.ParseMarkdown
	b.Storage.SendDirs = append(b.Storage.SendDirs, "/srv/outbox")

	got := ChangedSettings(a, b)
	want := []string{"storage.send_dirs", "ui.parse_markdown", "keys.compose"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ChangedSettings = %q, want %q", got, want)
	}
	if got := ChangedSettings(a, defaultConfig()); got != nil {
		t.Errorf("two default configs differ in %q", got)
	}
}

// TestChangedSettingsIgnoresWhatLoadKeepsBeside: the file Load read and the
// theme it resolved ride on the Config, and are not settings.
func TestChangedSettingsIgnoresWhatLoadKeepsBeside(t *testing.T) {
	a, b := defaultConfig(), defaultConfig()
	b.path = "/elsewhere.toml"
	b.themeConfigDir, b.themeDefaultDir = "/a", "/b"
	b.themeSpec, b.themeBuiltin, b.themeWarnings = &ThemeSpec{Name: "x"}, ThemeLight, []string{"w"}

	if got := ChangedSettings(a, b); got != nil {
		t.Errorf("ChangedSettings = %q, want nothing", got)
	}
}

// TestChangedSettingsCoversEverySetting: every setting config.toml can hold
// is compared, including one added after this test was written — the walk
// is over the struct, not a list. Each is changed on its own and has to be
// the one thing reported. A setting of a kind this test cannot change fails
// here, so it gets looked at rather than skipped.
func TestChangedSettingsCoversEverySetting(t *testing.T) {
	count := 0
	cfgType := reflect.TypeOf(Config{})
	for i := range cfgType.NumField() {
		section := cfgType.Field(i)
		if section.Tag.Get("toml") == "" {
			continue
		}
		for j := range section.Type.NumField() {
			field := section.Type.Field(j)
			tag := field.Tag.Get("toml")
			if tag == "" {
				continue
			}
			key := section.Tag.Get("toml") + "." + tag
			b := defaultConfig()
			changeValue(t, key, reflect.ValueOf(b).Elem().Field(i).Field(j))

			if got := ChangedSettings(defaultConfig(), b); !reflect.DeepEqual(got, []string{key}) {
				t.Errorf("changing %s alone reports %q", key, got)
			}
			count++
		}
	}
	if count < 40 {
		t.Fatalf("walked only %d settings; the walk is probably broken", count)
	}
}

// changeValue sets v to something other than what it holds.
func changeValue(t *testing.T, key string, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.String:
		v.SetString(v.String() + "-changed")
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.String {
			t.Fatalf("%s is a %s, which this test does not know how to change", key, v.Type())
		}
		v.Set(reflect.Append(v, reflect.ValueOf("changed")))
	default:
		t.Fatalf("%s is a %s, which this test does not know how to change", key, v.Type())
	}
}
