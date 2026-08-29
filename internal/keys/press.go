// Package keys holds the parts of the keymap that more than one package has
// to agree on: the key-event matcher every dispatcher uses, and the set of
// keys internal/app claims before any panel sees them.
//
// It exists because the keymap spans packages but used to be validated in
// only one. internal/app dispatches some keys, chatview and chatlist
// dispatch others, and internal/config decides which of them a user may
// rebind — with no shared vocabulary, each could only reason about its own
// half. The concrete failure that produced this package: chatview accepted
// reply = "q", advertised it on the help card as Reply, and pressing it
// quit the application, because chatview had no way to know app.go had
// claimed "q" first.
//
// It is a leaf. It imports nothing from this repository, so anyone may
// import it: app -> keys, app -> chatview -> keys, and config -> keys are
// all fine, and none of them can create a cycle.
package keys

import (
	tea "charm.land/bubbletea/v2"
)

// Press is the normalized view of a terminal key event that binding
// dispatch matches against.
//
// Matching on tea.KeyPressMsg.String() alone is not sufficient. String()
// returns Key.Text whenever the terminal attached any, and only falls back
// to Keystroke() when it did not. Terminals speaking the Kitty keyboard
// protocol report alt-modified keys *with* their composed text on macOS —
// Option+1 arrives as CSI 49;3;161u, i.e. Code='1', Mod=ModAlt,
// Text="¡" — so String() yields "¡" while Keystroke() yields "alt+1".
// Keystroke() is derived from Key.Code/Key.BaseCode and the modifier bits
// only, so it is stable across every encoding the decoder handles (legacy
// ESC-prefix, Kitty CSI-u, XTerm modifyOtherKeys).
//
// String() is still needed for the unmodified case, where it reports what
// the keyboard layout actually produced: shift+/ is "?" via String() but
// "shift+/" via Keystroke(), and a binding of "/" should match it.
//
// The fields are unexported: a Press is built by NewPress and asked
// questions, never assembled by hand. Both spellings are captured once, so
// dispatch does not recompute them for every binding it tests.
type Press struct {
	// stroke is Keystroke(): "alt+1", "ctrl+v", "f1", "a", "shift+/".
	stroke string
	// text is String(): the characters the terminal reported ("?", "A",
	// "¡"), falling back to stroke for keys that produce no text.
	text string
	// modified records whether a modifier beyond shift/caps-lock was held.
	// Such a key press must never be treated as text input.
	modified bool
}

// NewPress captures the two spellings of a key event once.
func NewPress(msg tea.KeyPressMsg) Press {
	return Press{
		stroke:   msg.Keystroke(),
		text:     msg.String(),
		modified: msg.Mod&^(tea.ModShift|tea.ModCapsLock|tea.ModNumLock|tea.ModScrollLock) != 0,
	}
}

// Matches reports whether the key press is any of the given bindings.
// Bindings are expected in config.NormalizeKey / Keystroke() form. Empty
// bindings never match, so an unset config field is inert.
//
// The Keystroke() spelling is authoritative. The String() spelling is only
// consulted for unmodified keys: allowing it for modified ones would let a
// Kitty-reported alt+/ (Text "/") fire a plain "/" binding.
func (p Press) Matches(bindings ...string) bool {
	for _, b := range bindings {
		if b == "" {
			continue
		}
		if b == p.stroke {
			return true
		}
		if !p.modified && b == p.text {
			return true
		}
	}
	return false
}

// Modified reports whether a modifier beyond shift/caps-lock was held. A
// modified key press is never text input, which is what lets a panel tell
// "the user typed a character" from "the user pressed a chord".
func (p Press) Modified() bool { return p.modified }

// Text returns the String() spelling: what the terminal reported the key
// produced, which for an unmodified key is what the layout actually typed.
// Dispatch should use Matches; this is for diagnostics and for tests that
// pin the difference between the two spellings.
func (p Press) Text() string { return p.text }

// Stroke returns the Keystroke() spelling, for diagnostics and test
// failure messages. Dispatch should use Matches, which knows when the
// other spelling is also admissible.
func (p Press) Stroke() string { return p.stroke }
