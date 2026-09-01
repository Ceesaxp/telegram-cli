package notification

import (
	"os"
	"strings"
)

// Desktop notifications from a terminal application, without asking another
// program to post them.
//
// The old path ran `osascript -e 'display notification ...'`, and macOS
// attributes a notification to the app that posted it — which for osascript
// is Script Editor. So a message from a person arrived under Script Editor's
// name and Script Editor's icon, with Script Editor's notification settings
// deciding whether it appeared at all. That is not a cosmetic problem: it is
// the wrong identity on the alert, and there is no osascript flag that fixes
// it, because the process really is Script Editor.
//
// A command-line binary cannot post under its own name on macOS at all. The
// UserNotifications framework requires a bundle identifier, which requires
// living inside a .app — so the honest choices are to ship an app bundle, to
// depend on somebody else's (terminal-notifier), or to stop asking the
// operating system directly.
//
// The terminal is already an application with a name, an icon and a
// notification permission the user granted deliberately. It can be asked to
// post the alert itself, over the same channel everything else on this screen
// travels: an escape sequence. The notification then comes from Ghostty, or
// iTerm2, or whatever is actually on screen — which is the truthful answer to
// "what just made this noise" — and it works unchanged over SSH, where every
// system-notification path fires on the wrong machine.

const (
	// osc777 is the urxvt-derived form, and the only widely-implemented one
	// that carries a title as well as a body.
	//
	//	ESC ] 777 ; notify ; TITLE ; BODY ST
	osc777 = "\x1b]777;notify;%s;%s\x1b\\"

	// osc9 is iTerm2's, and carries a body only — so the title has to be
	// folded into it.
	//
	//	ESC ] 9 ; BODY ST
	osc9 = "\x1b]9;%s\x1b\\"
)

// TerminalSupport is what a terminal will accept.
type TerminalSupport int

const (
	// TerminalNone means send nothing: a terminal that does not understand
	// the sequence PRINTS it, which would drop `]777;notify;Ana;hi` into
	// the middle of somebody's chat. The failure is asymmetric, so this is
	// an allowlist and the default answer is no.
	TerminalNone TerminalSupport = iota
	// TerminalTitleAndBody takes OSC 777.
	TerminalTitleAndBody
	// TerminalBodyOnly takes OSC 9.
	TerminalBodyOnly
)

// detectTerminal reports what the terminal this process is attached to will
// accept, from the environment alone.
//
// From the environment alone because the alternative — asking the terminal
// and reading its reply — was removed in wave 5: the response bytes arrived
// as input and were typed into the composer. An allowlist that is wrong
// costs a notification; a query that is wrong costs the user's draft.
func detectTerminal() TerminalSupport {
	// A multiplexer sits between this process and the terminal, and unless
	// it is configured to pass the sequence through it will either eat it
	// or print it. Neither is worth the risk, and neither is detectable.
	if os.Getenv("TMUX") != "" {
		return TerminalNone
	}
	if strings.HasPrefix(strings.ToLower(os.Getenv("TERM")), "screen") {
		return TerminalNone
	}

	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "ghostty", "wezterm":
		return TerminalTitleAndBody
	case "iterm.app":
		return TerminalBodyOnly
	}
	switch strings.ToLower(os.Getenv("TERM")) {
	case "xterm-kitty":
		return TerminalTitleAndBody
	case "foot", "foot-extra":
		return TerminalTitleAndBody
	case "rxvt-unicode", "rxvt-unicode-256color":
		return TerminalTitleAndBody
	}
	if os.Getenv("WT_SESSION") != "" { // Windows Terminal
		return TerminalBodyOnly
	}
	return TerminalNone
}

// sanitize strips what would break the sequence or the screen.
//
// A message body is attacker-controlled text: it arrives from whoever sent
// it. It is about to be placed inside an escape sequence whose fields are
// separated by semicolons and terminated by ST, so a body containing either
// would end the sequence early and leave the remainder to be drawn as text —
// or, worse, to be read as a sequence of its own. Control characters go for
// the same reason.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == ';':
			b.WriteRune(',')
		case r == '\n', r == '\t':
			b.WriteRune(' ')
		case r < 0x20, r == 0x7f:
			// Dropped, not replaced: ESC, BEL and the rest have no
			// business in a notification and no useful stand-in.
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
