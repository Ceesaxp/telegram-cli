package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// exampleKeyRe matches a TOML assignment, commented out or not, and takes
// the key. Commented-out lines count: config.example.toml uses them for
// settings whose default is "unset", and a reader looking for a key finds
// one either way.
var exampleKeyRe = regexp.MustCompile(`(?m)^\s*#?\s*([a-z_0-9]+)\s*=`)

func readExample(t *testing.T) string {
	t.Helper()
	for dir, _ := os.Getwd(); ; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, "config.example.toml")
		if raw, err := os.ReadFile(path); err == nil {
			return string(raw)
		}
		if parent := filepath.Dir(dir); parent == dir {
			t.Fatal("config.example.toml not found above the working directory")
			return ""
		}
	}
}

// tomlKeys walks a config struct and returns every leaf setting's TOML key.
func tomlKeys(t reflect.Type) []string {
	var keys []string
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Type.Kind() == reflect.Struct {
			keys = append(keys, tomlKeys(field.Type)...)
			continue
		}
		if tag := field.Tag.Get("toml"); tag != "" {
			keys = append(keys, tag)
		}
	}
	return keys
}

// TestTheExampleDocumentsEverySetting.
//
// A setting that exists and is not in config.example.toml is a setting
// nobody will find: the example IS the reference, there is no generated
// list, and the only other place the keys appear is a struct definition.
//
// TestExampleConfigKeysMatchDefaults next door checks the same thing for
// [keys] and only for [keys], which is why every other section was free to
// drift — and did.
func TestTheExampleDocumentsEverySetting(t *testing.T) {
	example := readExample(t)

	var missing []string
	for _, key := range tomlKeys(reflect.TypeOf(Config{})) {
		if !regexp.MustCompile(`(?m)^\s*#?\s*` + regexp.QuoteMeta(key) + `\s*=`).MatchString(example) {
			missing = append(missing, key)
		}
	}
	if len(missing) != 0 {
		t.Errorf("config.example.toml does not mention: %s", strings.Join(missing, ", "))
	}
}

// TestTheExampleInventsNoSettings, which is the same drift running the
// other way: a key that was renamed or removed leaves the example
// advertising something the parser will ignore.
func TestTheExampleInventsNoSettings(t *testing.T) {
	known := map[string]bool{}
	for _, key := range tomlKeys(reflect.TypeOf(Config{})) {
		known[key] = true
	}

	var invented []string
	for _, match := range exampleKeyRe.FindAllStringSubmatch(readExample(t), -1) {
		if !known[match[1]] {
			invented = append(invented, match[1])
		}
	}
	if len(invented) != 0 {
		t.Errorf("config.example.toml sets keys nothing reads: %s", strings.Join(invented, ", "))
	}
}

// TestTheExampleParses, and produces the config it appears to. A file that
// documents everything and does not load is worse than one that is short.
func TestTheExampleParses(t *testing.T) {
	// Through Load itself rather than the decoder underneath it, so
	// anything the real path does after unmarshalling is exercised too.
	t.Setenv("TELETUI_CONFIG", "../../config.example.toml")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("config.example.toml does not load: %v", err)
	}

	// Spot-check one setting from each section, so a parse that silently
	// dropped a table is caught rather than counted as a pass.
	if cfg.UI.Theme != "dark" {
		t.Errorf("ui.theme = %q", cfg.UI.Theme)
	}
	if cfg.Media.ImageProtocol != "auto" {
		t.Errorf("media.image_protocol = %q", cfg.Media.ImageProtocol)
	}
	if cfg.Notifications.Method != NotifyMethodAuto {
		t.Errorf("notifications.method = %q", cfg.Notifications.Method)
	}
	if cfg.Keys.Quit != "ctrl+q" {
		t.Errorf("keys.quit = %q", cfg.Keys.Quit)
	}
	if cfg.Storage.DownloadDir == "" {
		t.Error("storage.download_dir is empty")
	}
}

// TestTheInertSettingsAreMarkedAsSuch. Three settings round-trip and are
// never consulted; each is kept so an existing config does not lose keys on
// -migrate-config, and each has to SAY so where somebody will read it.
//
// The test is on the word rather than the behaviour because the behaviour
// is an absence: there is no call to assert. If one of these is ever wired
// up, this fails and the note comes out with the same commit.
func TestTheInertSettingsAreMarkedAsSuch(t *testing.T) {
	example := readExample(t)

	for _, key := range []string{"timestamp_format", "date_format", "auto_download_voice"} {
		at := strings.Index(example, key+" =")
		if at < 0 {
			t.Errorf("%s is not in the example at all", key)
			continue
		}
		// The note is in the comment block immediately above it.
		if !strings.Contains(example[max(0, at-700):at], "INERT") {
			t.Errorf("%s is documented as though something reads it", key)
		}
	}
}
