package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
)

// printable is the last thing between a warning and the terminal, and a
// warning can carry anything a theme file does: theme files are made to be
// shared, so what is in one is somebody else's text. Every C0 and C1
// control, DEL and any byte that is not UTF-8 becomes U+FFFD — replaced
// rather than dropped, so the reader can see something was there. No
// warning has a line break of its own, so newlines and tabs go too: a
// warning that spans lines is a warning somebody arranged.
func TestPrintableReplacesEveryControlCharacter(t *testing.T) {
	for in, want := range map[string]string{
		"colors.bg is not a role; ignored":   "colors.bg is not a role; ignored",
		"theme file /tmp/тема.toml — ok":     "theme file /tmp/тема.toml — ok",
		"\x1b]0;pwned\a":                     "\uFFFD]0;pwned\uFFFD",
		"CYAN\x1b[31m":                       "CYAN\uFFFD[31m",
		"a\nb\tc\rd":                         "a\uFFFDb\uFFFDc\uFFFDd",
		"del\x7f":                            "del\uFFFD",
		"c1 \u009b31m and \u0085":            "c1 \uFFFD31m and \uFFFD",
		"raw 8-bit CSI \x9b31m":              "raw 8-bit CSI \uFFFD31m",
		"already \uFFFD stays one character": "already \uFFFD stays one character",
	} {
		if got := printable(in); got != want {
			t.Errorf("printable(%q) = %q, want %q", in, got, want)
		}
	}
}

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
