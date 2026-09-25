package theme

import "testing"

// unknownTerminal blanks every variable the probe reads, so that a test says
// what the terminal is rather than inheriting whatever terminal the suite
// happens to be running in. A developer on Ghostty and CI on a bare pty must
// see the same answers.
func unknownTerminal(t *testing.T) {
	t.Helper()
	for _, k := range []string{"TMUX", "TERM", "TERM_PROGRAM", "KITTY_WINDOW_ID"} {
		t.Setenv(k, "")
	}
}

// The three terminals the maintainer checked by hand all compose, and the
// whole point of the probe is that a user on one of them should not have to
// find ui.emoji_width to get their chat-list border back.
func TestAKnownTerminalIsReportedAsComposing(t *testing.T) {
	tests := map[string]map[string]string{
		"kitty, by its terminfo name":   {"TERM": "xterm-kitty"},
		"kitty, by its window id":       {"KITTY_WINDOW_ID": "1"},
		"ghostty":                       {"TERM_PROGRAM": "ghostty"},
		"ghostty, by its terminfo name": {"TERM": "xterm-ghostty"},
		"wezterm, as it spells itself":  {"TERM_PROGRAM": "WezTerm"},
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			unknownTerminal(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if !ComposesEmoji() {
				t.Errorf("%s was not recognised as composing, with %v", name, env)
			}
		})
	}
}

// An allowlist says nothing about what is not on it. A terminal nobody has
// checked keeps the pessimistic reservation, which costs a gap rather than
// an overflowing row.
func TestAnUnknownTerminalIsNotReportedAsComposing(t *testing.T) {
	for _, term := range []string{"xterm-256color", "vt100", "dumb", ""} {
		t.Run(term, func(t *testing.T) {
			unknownTerminal(t)
			t.Setenv("TERM", term)
			if ComposesEmoji() {
				t.Errorf("TERM=%q was taken for a terminal known to compose", term)
			}
		})
	}
}

// Inside a multiplexer the outer terminal's identity does not describe what
// the app is drawing into: tmux and screen keep their own cell accounting
// and redraw through it, so a kitty outside the pane says nothing about the
// pane. The claim is refused however loudly the environment makes it.
func TestAMultiplexerIsNeverReportedAsComposing(t *testing.T) {
	tests := map[string]map[string]string{
		"tmux, with kitty outside it": {
			"TMUX": "/tmp/tmux-501/default,1,0",
			"TERM": "xterm-kitty",
		},
		"tmux, with ghostty outside it": {
			"TMUX":         "/tmp/tmux-501/default,1,0",
			"TERM_PROGRAM": "ghostty",
		},
		"screen, with ghostty outside it": {
			"TERM":         "screen-256color",
			"TERM_PROGRAM": "ghostty",
		},
		"screen, with a kitty window id still in the environment": {
			"TERM":            "screen",
			"KITTY_WINDOW_ID": "1",
		},
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			unknownTerminal(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if ComposesEmoji() {
				t.Errorf("%s was taken at its word: %v", name, env)
			}
		})
	}
}
