package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- decision I-13: a bare printable cannot be keys.quit -------------------

func TestIsBarePrintableKey(t *testing.T) {
	cases := map[string]bool{
		"x":      true,
		"?":      true,
		"+":      true,
		"q":      true,
		"ctrl+q": false,
		"alt+x":  false,
		"esc":    false,
		"f1":     false,
		"pgup":   false,
		"enter":  false,
		// space TYPES a character, whatever its name is longer than.
		"space":   true,
		"":        false,
		"ctrl+f5": false,
	}
	for key, want := range cases {
		if got := IsBarePrintableKey(key); got != want {
			t.Errorf("IsBarePrintableKey(%q) = %v, want %v", key, got, want)
		}
	}
}

// TestResolveQuitKeyRefusesABarePrintable: quit is matched before every
// focus gate, so quit = "x" made x untypable in a message. The refusal
// keeps the default — a client with no way out is worse than one that
// ignored a line of config.
func TestResolveQuitKeyRefusesABarePrintable(t *testing.T) {
	cases := []struct {
		configured  string
		wantKey     string
		wantRefused bool
	}{
		{"", DefaultQuitKey, false},
		{"ctrl+q", "ctrl+q", false},
		{"CTRL+Q", "ctrl+q", false},
		{"f9", "f9", false},
		{"ctrl+alt+x", "ctrl+alt+x", false},
		{"x", DefaultQuitKey, true},
		{"?", DefaultQuitKey, true},
		{"Q", DefaultQuitKey, true},
		// The named spelling of a printable key. quit is matched ahead of
		// every focus gate, so quit = "space" would mean the spacebar
		// exited the client instead of putting a space in a message —
		// which is exactly the class of loss this refusal exists for, and
		// the rune check alone did not catch it.
		{"space", DefaultQuitKey, true},
		{"spacebar", DefaultQuitKey, true},
		{"SPACE", DefaultQuitKey, true},
		// A literal space is trimmed to nothing on the way in, so it reads
		// as "unset" rather than as a refusal. Same safe key either way,
		// and worth pinning so a change to the trimming cannot make it a
		// live binding without anybody noticing.
		{" ", DefaultQuitKey, false},
	}
	for _, tc := range cases {
		key, refused := ResolveQuitKey(tc.configured)
		if key != tc.wantKey || refused != tc.wantRefused {
			t.Errorf("ResolveQuitKey(%q) = (%q, %v), want (%q, %v)",
				tc.configured, key, refused, tc.wantKey, tc.wantRefused)
		}
	}
}

// TestStartupWarningsReportTheRefusal is the other half of I-13: the warning
// is owed at startup, not only to somebody who happens to run
// -migrate-config on a client they are already using.
func TestStartupWarningsReportTheRefusal(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keys.Quit = "x"

	got := StartupWarnings(cfg)
	if len(got) == 0 {
		t.Fatal("a refused quit binding produced no startup warning")
	}
	if !strings.Contains(got[0], `"x"`) || !strings.Contains(got[0], "keys.quit") {
		t.Errorf("warning = %q, want it to name the value and the field", got[0])
	}
	if !strings.Contains(got[0], DefaultQuitKey) {
		t.Errorf("warning = %q, want it to say what quit falls back to", got[0])
	}
}

// TestStartupWarningsCarryTheCollisions: the collision report is the other
// thing a user is owed before the screen is taken, and it was already
// written — it was just only reachable behind a flag.
func TestStartupWarningsCarryTheCollisions(t *testing.T) {
	cfg := defaultConfig()
	cfg.Keys.Help = "/"
	cfg.Keys.Search = "/"

	if got := StartupWarnings(cfg); len(got) == 0 {
		t.Error("a collision produced no startup warning")
	}

	if got := StartupWarnings(defaultConfig()); len(got) != 0 {
		t.Errorf("the shipped defaults warn about %v, want nothing", got)
	}
	if got := StartupWarnings(nil); got != nil {
		t.Errorf("StartupWarnings(nil) = %v, want nil", got)
	}
}

// --- decision I-13: the fields the keymap cut removed ---------------------

// TestRemovedKeyFieldsStillLoad: an old config.toml keeps working. The
// decoder ignores keys the struct no longer has, so nothing breaks on
// upgrade — which is the promise the removal was made under, and worth
// pinning rather than assuming of a library.
func TestRemovedKeyFieldsStillLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	old := `[keys]
quit = "ctrl+q"
focus_chat_list = "f1"
focus_chat_view = "f2"
focus_composer = "f3"
contacts_alt = "f4"
forward = "f"
scroll_up = "k"
scroll_down = "j"
page_up = "pgup"
page_down = "pgdown"
reply = "R"
`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELETUI_CONFIG", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("a config holding the removed fields does not load: %v", err)
	}
	// And the fields that survive are still read off the same file.
	if cfg.Keys.Reply != "R" {
		t.Errorf("reply = %q, want the file's R", cfg.Keys.Reply)
	}
}

// TestRemovedKeyFieldsAreReportedAsRemoved: -migrate-config has to say what
// it dropped, the way it did for ui.chat_list_width. An unrecognised key
// reads as a typo the user should fix; a removed one is a setting that used
// to work, and the difference is the whole point of the report.
func TestRemovedKeyFieldsAreReportedAsRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	old := `[keys]
focus_chat_list = "f1"
contacts_alt = "f4"
forward = "f"
scroll_up = "k"
page_down = "pgdown"
`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, err := LoadRawFile(path)
	if err != nil {
		t.Fatalf("LoadRawFile: %v", err)
	}

	removed := raw.Removed()
	for _, field := range []string{
		"keys.focus_chat_list", "keys.contacts_alt", "keys.forward",
		"keys.scroll_up", "keys.page_down",
	} {
		if _, ok := removed[field]; !ok {
			t.Errorf("%s was not reported as removed: %v", field, removed)
		}
	}
	// And not as a typo, which is the other half of the distinction.
	for _, unknown := range raw.Unknown() {
		if strings.HasPrefix(unknown, "keys.") {
			t.Errorf("%s was reported as unrecognised rather than removed", unknown)
		}
	}
}
