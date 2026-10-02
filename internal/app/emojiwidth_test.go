package app

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/store"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
)

// The declaration has to reach the package that measures.
//
// ui.emoji_width is the setting a user reaches for when the top bar has a
// gap in it, and it does nothing at all unless New hands it to cell — which
// is a package-level mode rather than a field on a panel, precisely so that
// the chat titles and the rail agree with the folder tabs about how wide an
// emoji is.
func TestTheConfiguredEmojiWidthReachesCell(t *testing.T) {
	t.Cleanup(func() { cell.SetEmojiMode(cell.EmojiAuto) })
	// "auto" now asks which terminal this is, so the test has to say —
	// otherwise it passes on CI and fails on the maintainer's kitty.
	unknownTerminal(t)

	tests := map[string]cell.EmojiMode{
		config.EmojiWidthComposed: cell.EmojiComposed,
		config.EmojiWidthSeparate: cell.EmojiSeparate,
		config.EmojiWidthAuto:     cell.EmojiAuto,
		// A typo costs the setting, not the client.
		"narrow": cell.EmojiAuto,
		"":       cell.EmojiAuto,
	}

	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			// Start from the other mode, so a New that sets nothing at all
			// leaves an observably wrong value rather than the right one by
			// accident.
			cell.SetEmojiMode(cell.EmojiComposed)
			if want == cell.EmojiComposed {
				cell.SetEmojiMode(cell.EmojiSeparate)
			}

			cfg := &config.Config{}
			cfg.UI.EmojiWidth = value
			New(cfg, nil, store.NewStore(), nil)

			if got := cell.CurrentEmojiMode(); got != want {
				t.Errorf("emoji_width = %q left cell in %v, want %v",
					value, got, want)
			}
		})
	}
}

// unknownTerminal blanks every variable the probe reads, so that a test says
// which terminal it is describing instead of inheriting the one the suite is
// running in.
func unknownTerminal(t *testing.T) {
	t.Helper()
	for _, k := range []string{"TMUX", "TERM", "TERM_PROGRAM", "KITTY_WINDOW_ID"} {
		t.Setenv(k, "")
	}
}

// modeAfterNew is the mode New left the cell package in for this config.
//
// It starts from a mode the case does not expect, so a New that sets nothing
// at all fails rather than passing on whatever was left behind.
func modeAfterNew(t *testing.T, width string, want cell.EmojiMode) cell.EmojiMode {
	t.Helper()
	t.Cleanup(func() { cell.SetEmojiMode(cell.EmojiAuto) })
	start := cell.EmojiSeparate
	if want == cell.EmojiSeparate {
		start = cell.EmojiComposed
	}
	cell.SetEmojiMode(start)

	cfg := &config.Config{}
	cfg.UI.EmojiWidth = width
	New(cfg, nil, store.NewStore(), nil)
	return cell.CurrentEmojiMode()
}

// The reported bug: a flag or a family in a chat title left the chat-list
// border a cell or two short on kitty, because "auto" reserved the
// un-composed width and kitty drew the composed glyph. On a terminal whose
// behaviour is known there is nothing to hedge against, so "auto" stops
// hedging.
func TestAutoFollowsATerminalKnownToCompose(t *testing.T) {
	terminals := map[string]map[string]string{
		"kitty":   {"TERM": "xterm-kitty"},
		"ghostty": {"TERM_PROGRAM": "ghostty"},
		"wezterm": {"TERM_PROGRAM": "WezTerm"},
	}
	for name, env := range terminals {
		t.Run(name, func(t *testing.T) {
			unknownTerminal(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if got := modeAfterNew(t, config.EmojiWidthAuto, cell.EmojiComposed); got != cell.EmojiComposed {
				t.Errorf("auto on %s left cell in %v, want %v",
					name, got, cell.EmojiComposed)
			}
		})
	}
}

// Nobody has measured this terminal, so "auto" keeps the reservation that
// can only ever be too generous. A gap is survivable; a row that overruns
// what is beside it is the failure this code exists to prevent.
func TestAutoStaysPessimisticOnAnUnknownTerminal(t *testing.T) {
	unknownTerminal(t)
	t.Setenv("TERM", "xterm-256color")

	if got := modeAfterNew(t, config.EmojiWidthAuto, cell.EmojiAuto); got != cell.EmojiAuto {
		t.Errorf("auto on an unrecognised terminal left cell in %v, want %v",
			got, cell.EmojiAuto)
	}
}

// A multiplexer does its own cell accounting, so the terminal outside the
// pane does not describe what is being drawn into it — and tmux leaves
// kitty's variables in the environment it hands down.
func TestAutoStaysPessimisticUnderAMultiplexer(t *testing.T) {
	multiplexers := map[string]map[string]string{
		"tmux running in kitty": {
			"TMUX": "/tmp/tmux-501/default,1,0",
			"TERM": "xterm-kitty",
		},
		"screen running in ghostty": {
			"TERM":         "screen-256color",
			"TERM_PROGRAM": "ghostty",
		},
	}
	for name, env := range multiplexers {
		t.Run(name, func(t *testing.T) {
			unknownTerminal(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if got := modeAfterNew(t, config.EmojiWidthAuto, cell.EmojiAuto); got != cell.EmojiAuto {
				t.Errorf("auto under %s left cell in %v, want %v",
					name, got, cell.EmojiAuto)
			}
		})
	}
}

// "composed" and "separate" are the user telling us what they can see. The
// probe is a guess about terminals it recognises, and a guess does not get
// to overrule somebody looking at the screen — including the user on kitty
// who has some reason to want the pessimistic reservation back.
func TestAnExplicitEmojiWidthIgnoresTheTerminal(t *testing.T) {
	tests := map[string]struct {
		env  map[string]string
		want cell.EmojiMode
	}{
		"separate, on a terminal the probe calls composed": {
			env:  map[string]string{"TERM": "xterm-kitty"},
			want: cell.EmojiSeparate,
		},
		"composed, on a terminal the probe knows nothing about": {
			env:  map[string]string{"TERM": "xterm-256color"},
			want: cell.EmojiComposed,
		},
		"composed, under tmux": {
			env: map[string]string{
				"TMUX": "/tmp/tmux-501/default,1,0",
				"TERM": "xterm-kitty",
			},
			want: cell.EmojiComposed,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			unknownTerminal(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			width := config.EmojiWidthComposed
			if tc.want == cell.EmojiSeparate {
				width = config.EmojiWidthSeparate
			}
			if got := modeAfterNew(t, width, tc.want); got != tc.want {
				t.Errorf("%s left cell in %v, want %v", name, got, tc.want)
			}
		})
	}
}
