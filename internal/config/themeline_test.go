package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// configFile writes body to a config.toml of its own and returns the path.
// The default config directory is a temp one too: SetThemeLine checks its
// write with the real loader, and the loader looks for themes there.
func configFile(t *testing.T, body string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestSetThemeLineEditsOnlyTheThemeLine: the file is the user's, comments,
// order, spelling and spacing included, so the one value is all that
// changes — not the key's spelling, not the spaces around the =, not the
// comment after it, not the line endings, not whether the file ends in a
// newline. Re-encoding the whole file, as Save does, would lose all of that
// to change one word.
func TestSetThemeLineEditsOnlyTheThemeLine(t *testing.T) {
	tests := []struct {
		name, before, value, after string
	}{
		{name: "the theme line in [ui], among other keys, keeping its comment",
			value: "gruvbox",
			before: "# my config\n[telegram]\napi_id = 1\n\n[ui]\nrail = true\n" +
				"# theme = \"light\"\ntheme = \"dark\"   # the palette\nparse_markdown = false\n\n" +
				"[keys]\nquit = \"ctrl+q\"\n",
			after: "# my config\n[telegram]\napi_id = 1\n\n[ui]\nrail = true\n" +
				"# theme = \"light\"\ntheme = 'gruvbox'   # the palette\nparse_markdown = false\n\n" +
				"[keys]\nquit = \"ctrl+q\"\n"},
		{name: "a quoted key",
			value:  "gruvbox",
			before: "[ui]\n\"theme\" = \"dark\"\n",
			after:  "[ui]\n\"theme\" = 'gruvbox'\n"},
		{name: "a literal-quoted key",
			value:  "gruvbox",
			before: "[ui]\n'theme' = 'dark'\n",
			after:  "[ui]\n'theme' = 'gruvbox'\n"},
		{name: "no spaces around the =, and indented",
			value:  "gruvbox",
			before: "[ui]\n  theme='dark'\n",
			after:  "[ui]\n  theme='gruvbox'\n"},
		{name: "tabs around the =",
			value:  "gruvbox",
			before: "[ui]\ntheme\t=\t\"dark\"\t# tabs\n",
			after:  "[ui]\ntheme\t=\t'gruvbox'\t# tabs\n"},
		{name: "a multi-line string value",
			value:  "gruvbox",
			before: "[ui]\ntheme = \"\"\"dark\"\"\"\nrail = true\n",
			after:  "[ui]\ntheme = 'gruvbox'\nrail = true\n"},
		{name: "a [ui] header with spaces, a comment and indentation",
			value:  "gruvbox",
			before: "  [ ui ]  # interface\ntheme = \"dark\"\n",
			after:  "  [ ui ]  # interface\ntheme = 'gruvbox'\n"},
		{name: "a quoted [ui] header",
			value:  "gruvbox",
			before: "[\"ui\"]\ntheme = \"dark\"\n",
			after:  "[\"ui\"]\ntheme = 'gruvbox'\n"},
		{name: "a theme key in another table is not the theme",
			value:  "gruvbox",
			before: "[media]\ntheme = \"dark\"\n\n[ui]\ntheme = \"dark\"\n",
			after:  "[media]\ntheme = \"dark\"\n\n[ui]\ntheme = 'gruvbox'\n"},
		{name: "a table header inside a multi-line string is not a table",
			value: "gruvbox",
			before: "[telegram]\nphone = \"\"\"\n[ui]\ntheme = \"evil\"\n\"\"\"\n\n" +
				"[ui]\ntheme = \"dark\"\n",
			after: "[telegram]\nphone = \"\"\"\n[ui]\ntheme = \"evil\"\n\"\"\"\n\n" +
				"[ui]\ntheme = 'gruvbox'\n"},
		// The loader matches tables and keys whatever their case, as
		// go-toml matches struct fields, so this has to find them the same
		// way — or it adds a second theme the loader then reads instead.
		{name: "a capitalised key",
			value:  "gruvbox",
			before: "[ui]\nTheme = \"dark\"  # mine\n",
			after:  "[ui]\nTheme = 'gruvbox'  # mine\n"},
		{name: "an upper-case key",
			value:  "gruvbox",
			before: "[ui]\nTHEME = \"dark\"\n",
			after:  "[ui]\nTHEME = 'gruvbox'\n"},
		{name: "an upper-case [UI] header",
			value:  "gruvbox",
			before: "[UI]\ntheme = \"dark\"\nrail = true\n",
			after:  "[UI]\ntheme = 'gruvbox'\nrail = true\n"},
		{name: "a mixed-case [Ui] header and key",
			value:  "gruvbox",
			before: "[Ui]\ntHeMe = \"dark\"\n",
			after:  "[Ui]\ntHeMe = 'gruvbox'\n"},
		{name: "an upper-case [UI] without a theme gets one under it",
			value:  "gruvbox",
			before: "[UI]\nrail = true\n",
			after:  "[UI]\ntheme = 'gruvbox'\nrail = true\n"},
		// An array of tables under ui is not ui itself, and the loader
		// takes it: only an array of [[ui]] makes ui something else.
		{name: "an array of tables under ui",
			value:  "gruvbox",
			before: "[ui]\ntheme = \"dark\"\n\n[[ui.plugins]]\nname = \"x\"\n",
			after:  "[ui]\ntheme = 'gruvbox'\n\n[[ui.plugins]]\nname = \"x\"\n"},
		{name: "an array of tables under ui, and no [ui]",
			value:  "gruvbox",
			before: "[[ui.plugins]]\nname = \"x\"\n",
			after:  "[[ui.plugins]]\nname = \"x\"\n\n[ui]\ntheme = 'gruvbox'\n"},
		{name: "[ui] without a theme key gets one under the header",
			value:  "gruvbox",
			before: "[ui]  # interface\nrail = true\n",
			after:  "[ui]  # interface\ntheme = 'gruvbox'\nrail = true\n"},
		{name: "[ui] as the last line, with no newline after it",
			value:  "gruvbox",
			before: "[telegram]\napi_id = 1\n[ui]",
			after:  "[telegram]\napi_id = 1\n[ui]\ntheme = 'gruvbox'"},
		{name: "no [ui] table gets one at the end",
			value:  "gruvbox",
			before: "[telegram]\napi_id = 1\n",
			after:  "[telegram]\napi_id = 1\n\n[ui]\ntheme = 'gruvbox'\n"},
		{name: "an empty file",
			value:  "gruvbox",
			before: "",
			after:  "[ui]\ntheme = 'gruvbox'\n"},
		{name: "CRLF stays CRLF when the value is replaced",
			value:  "gruvbox",
			before: "[ui]\r\ntheme = \"dark\"\r\nrail = true\r\n",
			after:  "[ui]\r\ntheme = 'gruvbox'\r\nrail = true\r\n"},
		{name: "CRLF stays CRLF when a line is inserted",
			value:  "gruvbox",
			before: "[ui]\r\nrail = true\r\n",
			after:  "[ui]\r\ntheme = 'gruvbox'\r\nrail = true\r\n"},
		{name: "CRLF stays CRLF when a table is appended",
			value:  "gruvbox",
			before: "[telegram]\r\napi_id = 1\r\n",
			after:  "[telegram]\r\napi_id = 1\r\n\r\n[ui]\r\ntheme = 'gruvbox'\r\n"},
		{name: "no final newline stays that way when the value is replaced",
			value:  "gruvbox",
			before: "[ui]\ntheme = \"dark\"",
			after:  "[ui]\ntheme = 'gruvbox'"},
		{name: "no final newline stays that way when a table is appended",
			value:  "gruvbox",
			before: "[telegram]\napi_id = 1",
			after:  "[telegram]\napi_id = 1\n\n[ui]\ntheme = 'gruvbox'"},
		{name: "a Windows path is written literally, backslashes and all",
			value:  `C:\Users\me\themes\nord.toml`,
			before: "[ui]\ntheme = \"dark\"\n",
			after:  "[ui]\ntheme = 'C:\\Users\\me\\themes\\nord.toml'\n"},
		{name: "a value with a ' is a basic string",
			value:  "~/themes/o'brien.toml",
			before: "[ui]\ntheme = \"dark\"\n",
			after:  "[ui]\ntheme = \"~/themes/o'brien.toml\"\n"},
		{name: "a Windows path with a ' has its backslashes escaped",
			value:  `C:\Users\O'Brien\themes\nord.toml`,
			before: "[ui]\ntheme = \"dark\"\n",
			after:  "[ui]\ntheme = \"C:\\\\Users\\\\O'Brien\\\\themes\\\\nord.toml\"\n"},
		{name: "a value with a double quote",
			value:  `~/themes/"quoted"'.toml`,
			before: "[ui]\ntheme = \"dark\"\n",
			after:  "[ui]\ntheme = \"~/themes/\\\"quoted\\\"'.toml\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := configFile(t, tt.before)

			if _, err := SetThemeLine(path, tt.value, true); err != nil {
				t.Fatalf("SetThemeLine: %v", err)
			}
			if got := readFile(t, path); got != tt.after {
				t.Errorf("the file is\n%q\nwant\n%q", got, tt.after)
			}
			// And the real loader reads back what was written.
			cfg, err := loadFrom(path)
			if err != nil {
				t.Fatalf("the edited file does not load: %v", err)
			}
			if cfg.UI.Theme != tt.value {
				t.Errorf("the loader reads ui.theme = %q, want %q", cfg.UI.Theme, tt.value)
			}
		})
	}
}

// TestSetThemeLineRefusesWhatItCannotEditSafely: a ui.theme written some
// other way than a line in a [ui] table cannot be changed by rewriting one
// line, and adding a [ui] table beside it would make the file invalid. A
// file that is not TOML at all has no line to trust. Each is left alone,
// unbacked-up, with a word to edit it by hand.
func TestSetThemeLineRefusesWhatItCannotEditSafely(t *testing.T) {
	tests := []struct {
		name, body string
	}{
		{"a dotted top-level key", "ui.theme = \"dark\"\n"},
		{"a dotted top-level key with spaces", "ui . theme = \"dark\"\n"},
		{"another dotted ui key, which a [ui] table would collide with", "ui.rail = true\n"},
		{"an inline table", "ui = { theme = \"dark\" }\n"},
		{"an inline table without theme", "ui = { rail = true }\n"},
		{"ui that is not a table", "ui = \"dark\"\n"},
		{"a dotted theme key inside [ui]", "[ui]\ntheme.name = \"dark\"\n"},
		{"a [ui.theme] table", "[ui.theme]\nname = \"dark\"\n"},
		{"an array of [[ui]] tables", "[[ui]]\ntheme = \"dark\"\n"},
		{"a theme that is not a string", "[ui]\ntheme = 5\n"},
		{"an upper-case dotted top-level key", "UI.Theme = \"dark\"\n"},
		{"two [ui] tables, told apart only by case", "[ui]\ntheme = \"dark\"\n[UI]\nrail = true\n"},
		{"two theme keys, told apart only by case", "[ui]\ntheme = \"dark\"\nTheme = \"light\"\n"},
		{"not TOML at all", "[ui\ntheme = \"dark\"\n"},
		{"a key defined twice", "[ui]\ntheme = \"dark\"\ntheme = \"light\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := configFile(t, tt.body)

			kept, err := SetThemeLine(path, "gruvbox", true)
			if err == nil {
				t.Fatalf("SetThemeLine accepted %q:\n%s", tt.body, readFile(t, path))
			}
			if kept {
				t.Error("a refusal says the original is kept, and it made no backup")
			}
			if !strings.Contains(err.Error(), "by hand") {
				t.Errorf("the error %q does not say to edit the file by hand", err)
			}
			if got := readFile(t, path); got != tt.body {
				t.Errorf("the file was changed to\n%q", got)
			}
			if _, err := os.Stat(path + ".bak"); err == nil {
				t.Error("a refusal left a backup behind")
			}
		})
	}
}

// TestSetThemeLineCreatesAMissingFile: a first run has no config.toml, and a
// theme chosen then is saved to where Load will find it — private, as every
// config write is, since the file will hold an api_hash one day.
func TestSetThemeLineCreatesAMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "tele-tui", "config.toml")

	// There was nothing to keep, so nothing is missing: the file is
	// tele-tui's from here on, and a later save has no reason to copy it.
	if kept, err := SetThemeLine(path, "gruvbox", true); err != nil || !kept {
		t.Fatalf("SetThemeLine = %v, %v", kept, err)
	}
	if got, want := readFile(t, path), "[ui]\ntheme = 'gruvbox'\n"; got != want {
		t.Errorf("the new file is %q, want %q", got, want)
	}
	if found, _ := filepath.Glob(path + ".bak*"); len(found) != 0 {
		t.Errorf("a file created from nothing was backed up: %v", found)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil {
			t.Fatal(err)
		} else if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("the new file's mode is %v, want 0600", got)
		}
	}
}

// TestSetThemeLineKeepsOneBackup: the backup is the file as it was before
// tele-tui first edited it — so it is made from the bytes the edit started
// from, and made once. With config.toml.bak there already, from an earlier
// session or from -migrate-config, a save makes none, timestamped or not:
// each would be one more copy of an api_hash and a phone number, and none
// of them the user's own file.
func TestSetThemeLineKeepsOneBackup(t *testing.T) {
	original := "[ui]\ntheme = \"dark\"  # mine\n"
	path := configFile(t, original)

	kept, err := SetThemeLine(path, "gruvbox", true)
	if err != nil || !kept {
		t.Fatalf("SetThemeLine = %v, %v; want the original kept", kept, err)
	}
	if got := readFile(t, path+".bak"); got != original {
		t.Errorf("the backup holds %q, want the original %q", got, original)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path + ".bak"); err != nil {
			t.Fatal(err)
		} else if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("the backup's mode is %v, want 0600: it holds what the config does", got)
		}
	}

	// A later session, asked to back up again.
	for _, theme := range []string{"nord", "amber"} {
		if kept, err := SetThemeLine(path, theme, true); err != nil || !kept {
			t.Fatalf("SetThemeLine(%s) = %v, %v; want the original still kept", theme, kept, err)
		}
	}
	if got := readFile(t, path+".bak"); got != original {
		t.Errorf("a later save overwrote the backup with %q", got)
	}
	if found, _ := filepath.Glob(path + ".bak*"); len(found) != 1 {
		t.Errorf("backups %v, want config.toml.bak alone", found)
	}
}

// TestSetThemeLineLeavesAnExistingBackupAlone: a config.toml.bak that is
// not :theme's — -migrate-config's, say — is the older file, and stays.
func TestSetThemeLineLeavesAnExistingBackupAlone(t *testing.T) {
	path := configFile(t, "[ui]\ntheme = \"dark\"\n")
	const older = "# before -migrate-config\n"
	if err := os.WriteFile(path+".bak", []byte(older), 0o600); err != nil {
		t.Fatal(err)
	}

	if kept, err := SetThemeLine(path, "gruvbox", true); err != nil || !kept {
		t.Fatalf("SetThemeLine = %v, %v", kept, err)
	}
	if got := readFile(t, path+".bak"); got != older {
		t.Errorf("the existing backup now holds %q", got)
	}
	if found, _ := filepath.Glob(path + ".bak*"); len(found) != 1 {
		t.Errorf("backups %v, want the existing one alone", found)
	}
}

// TestSetThemeLineCanSkipTheBackup: a caller that has already kept the
// user's file — earlier in the same session — edits without a new copy,
// even with no backup there: one deleted since is not a reason to back up
// a file this session has already edited.
func TestSetThemeLineCanSkipTheBackup(t *testing.T) {
	path := configFile(t, "[ui]\ntheme = \"dark\"\n")

	if _, err := SetThemeLine(path, "gruvbox", false); err != nil {
		t.Fatalf("SetThemeLine: %v", err)
	}
	if got := readFile(t, path); got != "[ui]\ntheme = 'gruvbox'\n" {
		t.Errorf("the file is %q, want the theme saved", got)
	}
	if found, _ := filepath.Glob(path + ".bak*"); len(found) != 0 {
		t.Errorf("a write told not to back up left %v", found)
	}
}

// TestSetThemeLineKeepsTheFileMode: this edits a line; how private the file
// is was decided by its owner, and a one-line edit does not decide it again.
func TestSetThemeLineKeepsTheFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	path := configFile(t, "[ui]\ntheme = \"dark\"\n")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := SetThemeLine(path, "gruvbox", true); err != nil {
		t.Fatalf("SetThemeLine: %v", err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mode = %v after the edit, want the file's own 0640", got)
	}
}

// TestSetThemeLineRefusesAReadOnlyFile: a config made read-only was made
// so on purpose. Replacing it would still work — a rename needs only the
// directory to be writable — which is exactly why this has to ask the file.
func TestSetThemeLineRefusesAReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	original := "[ui]\ntheme = \"dark\"\n"
	path := configFile(t, original)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}

	kept, err := SetThemeLine(path, "gruvbox", true)

	if err == nil || !strings.Contains(err.Error(), "config.toml is read-only; edit it by hand or make it writable") {
		t.Fatalf("err = %v, want the read-only refusal", err)
	}
	if kept {
		t.Error("a refusal says the original is kept")
	}
	if got := readFile(t, path); got != original {
		t.Errorf("the read-only file was rewritten: %q", got)
	}
	if found, _ := filepath.Glob(path + ".bak*"); len(found) != 0 {
		t.Errorf("a refusal left %v", found)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o400 {
		t.Errorf("mode = %v, want 0400 still", got)
	}
}

// TestSetThemeLineWritesThroughASymlink: a config.toml symlinked out of a
// dotfiles repository is edited where it lives. Replacing the link with a
// file would quietly end the dotfiles copy's life as the source of truth.
func TestSetThemeLineWritesThroughASymlink(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dotfiles, confDir := t.TempDir(), t.TempDir()
	real := filepath.Join(dotfiles, "config.toml")
	link := filepath.Join(confDir, "config.toml")
	if err := os.WriteFile(real, []byte("[ui]\ntheme = \"dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := SetThemeLine(link, "gruvbox", true); err != nil {
		t.Fatalf("SetThemeLine: %v", err)
	}

	if info, err := os.Lstat(link); err != nil {
		t.Fatalf("the symlink is gone: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced by a regular file")
	}
	if got, want := readFile(t, real), "[ui]\ntheme = 'gruvbox'\n"; got != want {
		t.Errorf("the target holds %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(evalDir(t, dotfiles), "config.toml.bak")); err != nil {
		t.Errorf("no backup beside the target: %v", err)
	}
}

// TestSetThemeLineRestoresWhatDoesNotReadBack: the edit is checked with the
// real loader, and a file that does not load, or does not say what was
// written, is put back the way it was. A config that stops loading is a
// client that stops starting.
func TestSetThemeLineRestoresWhatDoesNotReadBack(t *testing.T) {
	original := "[ui]\ntheme = \"dark\"  # mine\n"
	checks := map[string]func(string) (*Config, error){
		"the loader fails": func(string) (*Config, error) {
			return nil, errors.New("simulated")
		},
		"the loader reads another theme": func(string) (*Config, error) {
			return &Config{UI: UIConfig{Theme: "dark"}}, nil
		},
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			path := configFile(t, original)
			if runtime.GOOS != "windows" {
				if err := os.Chmod(path, 0o640); err != nil {
					t.Fatal(err)
				}
			}
			loadWritten = check
			t.Cleanup(func() { loadWritten = loadFrom })

			kept, err := SetThemeLine(path, "gruvbox", true)
			if err == nil {
				t.Fatal("SetThemeLine reported success for a write that did not read back")
			}
			if got := readFile(t, path); got != original {
				t.Errorf("the file was left as %q, want the original restored", got)
			}
			// The backup was made before the write, and a failed save does
			// not unmake it: it says so, so the caller does not ask again.
			if !kept || readFile(t, path+".bak") != original {
				t.Errorf("kept = %v, and the backup holds %q; want the original kept", kept, readFile(t, path+".bak"))
			}
			// A retry — the same failure again — backs up nothing more.
			if _, err := SetThemeLine(path, "gruvbox", true); err == nil {
				t.Fatal("the retry succeeded")
			}
			if found, _ := filepath.Glob(path + ".bak*"); len(found) != 1 {
				t.Errorf("after a retry the backups are %v, want one", found)
			}
			if runtime.GOOS != "windows" {
				if info, err := os.Stat(path); err != nil {
					t.Fatal(err)
				} else if got := info.Mode().Perm(); got != 0o640 {
					t.Errorf("the restored file's mode is %v, want 0640", got)
				}
			}
		})
	}

	// A write that was not backed up is restored the same way: from the
	// bytes read before the edit, which no backup is needed for.
	t.Run("without a backup", func(t *testing.T) {
		path := configFile(t, original)
		loadWritten = checks["the loader fails"]
		t.Cleanup(func() { loadWritten = loadFrom })

		if _, err := SetThemeLine(path, "gruvbox", false); err == nil {
			t.Fatal("SetThemeLine reported success for a write that did not read back")
		}
		if got := readFile(t, path); got != original {
			t.Errorf("the file was left as %q, want the original restored", got)
		}
		if _, err := os.Stat(path + ".bak"); err == nil {
			t.Error("a write told not to back up left a backup")
		}
	})

	// And a file that did not exist is not left existing.
	t.Run("a created file", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		path := filepath.Join(t.TempDir(), "config.toml")
		loadWritten = checks["the loader fails"]
		t.Cleanup(func() { loadWritten = loadFrom })

		if _, err := SetThemeLine(path, "gruvbox", true); err == nil {
			t.Fatal("SetThemeLine reported success for a write that did not read back")
		}
		if _, err := os.Stat(path); err == nil {
			t.Error("the file it created and could not read back is still there")
		}
	})
}
