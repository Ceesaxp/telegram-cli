package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes a config file and points the loader at it.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELETUI_CONFIG", path)
	return path
}

// TestRemovedUIFieldsAreReportedAsRemoved (decision 10).
//
// Reported as REMOVED rather than as unrecognised, because they are different
// news: an unrecognised key reads as a typo the user should fix, while a
// removed one is a setting that used to work and now does not. A user who
// tuned chat_list_width deserves to be told it stopped doing anything, not
// left to wonder why the column ignores them.
func TestRemovedUIFieldsAreReportedAsRemoved(t *testing.T) {
	path := writeConfig(t, `
[telegram]
api_id = 1
api_hash = "x"

[ui]
theme = "dark"
chat_list_width = 45
show_avatars = false
`)

	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatalf("LoadRawFile: %v", err)
	}

	// Not in the unknown list: two reports of one fact is noise, and the
	// generic one is the less useful of the two.
	for _, u := range raw.Unknown() {
		if strings.HasPrefix(u, "ui.chat_list_width") || strings.HasPrefix(u, "ui.show_avatars") {
			t.Errorf("%s was reported as an unrecognised key", u)
		}
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	changes := changeMap(t, Migrate(cfg, raw))

	for field, oldValue := range map[string]string{
		"ui.chat_list_width": "45",
		"ui.show_avatars":    "false",
	} {
		c, ok := changes[field]
		if !ok {
			t.Errorf("%s was not reported", field)
			continue
		}
		if !c.Removed {
			t.Errorf("%s was reported as a change, not a removal", field)
		}
		if c.Old != oldValue {
			t.Errorf("%s reported its old value as %q, want %q", field, c.Old, oldValue)
		}
		if !strings.Contains(c.String(), "(removed)") {
			t.Errorf("%s summary does not say it was removed: %q", field, c.String())
		}
	}
}

// TestRemovedFieldsAreGoneFromTheSchema. The point of removing them is that
// nothing reads them; a struct field left behind is how a setting keeps being
// parsed and silently ignored.
func TestRemovedFieldsAreGoneFromTheSchema(t *testing.T) {
	known := knownFields()
	for _, field := range []string{"chat_list_width", "show_avatars"} {
		if known["ui"][field] {
			t.Errorf("ui.%s is still in the schema", field)
		}
	}

	// And ui.mode_indicator was never in it. Decision 10: a modal client
	// whose mode indicator can be switched off is a modal client that will
	// be used with it switched off.
	if known["ui"]["mode_indicator"] {
		t.Error("ui.mode_indicator exists; the mode badge is not configurable away")
	}
	if removedFields["ui"]["mode_indicator"] {
		t.Error("ui.mode_indicator is listed as removed; it was never added")
	}
}

// TestMigrateAddsInlineImages: a new key with a non-zero default is worth
// reporting, because the client's behaviour depends on it and the user has
// never seen it.
func TestMigrateAddsInlineImages(t *testing.T) {
	cfg := &Config{}
	changes := changeMap(t, Migrate(cfg, nil))

	c, ok := changes["ui.inline_images"]
	if !ok {
		t.Fatal("ui.inline_images was not filled in")
	}
	if !c.Absent {
		t.Error("ui.inline_images was reported as replacing a value it never had")
	}
	if c.New != InlineImagesOnOpen {
		t.Errorf("ui.inline_images -> %q, want %q", c.New, InlineImagesOnOpen)
	}
	if cfg.UI.InlineImages != InlineImagesOnOpen {
		t.Errorf("the config was not updated: %q", cfg.UI.InlineImages)
	}

	// ui.rail is deliberately NOT reported: its default is the zero value,
	// so there is nothing to tell anyone, and with no raw file to consult
	// "absent" and "set to false" are the same observation — reporting it
	// would make the migration non-idempotent.
	if _, ok := changes["ui.rail"]; ok {
		t.Error("ui.rail was reported despite defaulting to its zero value")
	}
}

// TestResolveInlineImages: a typo costs the user the setting, not the client.
func TestResolveInlineImages(t *testing.T) {
	for in, want := range map[string]string{
		"never":     InlineImagesNever,
		"NEVER":     InlineImagesNever,
		" always ":  InlineImagesAlways,
		"on_open":   InlineImagesOnOpen,
		"":          InlineImagesOnOpen,
		"sometimes": InlineImagesOnOpen,
	} {
		if got := ResolveInlineImages(in); got != want {
			t.Errorf("ResolveInlineImages(%q) = %q, want %q", in, got, want)
		}
	}
}
