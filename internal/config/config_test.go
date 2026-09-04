package config

import (
	"strings"
	"testing"
)

// --- decision I-13: a bare printable cannot be keys.quit -------------------

func TestIsBarePrintableKey(t *testing.T) {
	cases := map[string]bool{
		"x":       true,
		"?":       true,
		"+":       true,
		"q":       true,
		"ctrl+q":  false,
		"alt+x":   false,
		"esc":     false,
		"f1":      false,
		"pgup":    false,
		"enter":   false,
		"space":   false,
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
