package cell

import "testing"

// Printable is the last thing between a message and the terminal, and a
// message can carry anything a theme file does: theme files are made to be
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
		if got := Printable(in); got != want {
			t.Errorf("Printable(%q) = %q, want %q", in, got, want)
		}
	}
}
