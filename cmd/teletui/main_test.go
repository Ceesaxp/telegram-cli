package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// End to end: a theme file with escape sequences in its keys, loaded the
// way main loads it, prints warnings that carry no control byte at all —
// through config's reading and theme's role check alike.
func TestAHostileThemeFileWarnsWithoutAnEscape(t *testing.T) {
	dir := t.TempDir()
	themes := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themes, 0o700); err != nil {
		t.Fatal(err)
	}
	hostile := "\"\\u001b]0;pwned\\u0007\" = 1\n\n" +
		"[colors]\n\"\\u001b]0;pwned\\u0007\" = \"#000000\"\n\"CYAN\\u001b[31m\" = \"#111111\"\n\"\\u001b[2J\" = 1.5\n"
	if err := os.WriteFile(filepath.Join(themes, "evil.toml"), []byte(hostile), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[ui]\ntheme = \"evil\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELETUI_CONFIG", cfgPath)
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	warnings := startupWarnings(cfg)
	if len(warnings) < 4 {
		t.Fatalf("got %d warnings, want one for each hostile key: %q", len(warnings), warnings)
	}
	for _, w := range warnings {
		if strings.ContainsFunc(w, func(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) }) {
			t.Errorf("a warning carries a control character to the terminal: %q", w)
		}
	}
}
