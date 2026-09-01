package notification

import (
	"strings"
	"testing"
)

// notifier builds one with the terminal support forced, so the tests are
// about the decision and not about whatever terminal happens to be running
// them.
func notifier(method string, support TerminalSupport) *Notifier {
	n := NewNotifier(true, true, method)
	n.terminal = support
	return n
}

// The sequence carries both fields where the terminal takes both.
func TestOSC777CarriesTitleAndBody(t *testing.T) {
	got := notifier(MethodAuto, TerminalTitleAndBody).Notify("Ana", "see you at six")

	if want := "\x1b]777;notify;Ana;see you at six\x1b\\"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// And folds the sender in where it takes only one. Dropping the name would
// leave "see you at six" with nothing saying who from, which is the one
// thing a notification is for.
func TestOSC9FoldsTheSenderIntoTheBody(t *testing.T) {
	got := notifier(MethodAuto, TerminalBodyOnly).Notify("Ana", "see you at six")

	if want := "\x1b]9;Ana: see you at six\x1b\\"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A terminal that does not understand the sequence PRINTS it, dropping
// `]777;notify;Ana;hi` into the middle of somebody's chat. So the allowlist
// defaults to no, and an unknown terminal goes to the system notifier —
// which returns nothing for the caller to write.
func TestAnUnknownTerminalIsNotSentASequence(t *testing.T) {
	if got := notifier(MethodAuto, TerminalNone).Notify("Ana", "hi"); got != "" {
		t.Errorf("an unknown terminal was sent %q", got)
	}
}

// The method is the user's to override, in both directions.
func TestMethodOverridesTheAllowlist(t *testing.T) {
	// "terminal" on a terminal the allowlist does not know: believe them.
	got := notifier(MethodTerminal, TerminalNone).Notify("Ana", "hi")
	if !strings.HasPrefix(got, "\x1b]777;notify;") {
		t.Errorf(`method "terminal" produced %q`, got)
	}

	// "system" on a terminal that would have taken it: don't.
	if got := notifier(MethodSystem, TerminalTitleAndBody).Notify("Ana", "hi"); got != "" {
		t.Errorf(`method "system" produced a sequence: %q`, got)
	}
}

// A message body is written by whoever sent it, and it is about to be placed
// inside an escape sequence whose fields are separated by semicolons and
// terminated by ST. A body carrying either would close the sequence early
// and leave the rest to be drawn — or read as a sequence of its own.
func TestASenderCannotBreakOutOfTheSequence(t *testing.T) {
	tests := map[string]string{
		"a semicolon":  "a;b",
		"a string end": "a\x1b\\b",
		"an escape":    "a\x1bb",
		"a bell":       "a\ab",
		"a newline":    "a\nb",
		"a whole sequence": "\x1b]777;notify;Bank;" +
			"your account is closed\x1b\\",
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			got := notifier(MethodAuto, TerminalTitleAndBody).Notify("Ana", body)

			// Exactly one sequence: one opener, one terminator, and no
			// field separator beyond the three the form itself has
			// (777, notify, title, body).
			if n := strings.Count(got, "\x1b]"); n != 1 {
				t.Errorf("%q produced %d openers: %q", body, n, got)
			}
			if n := strings.Count(got, "\x1b\\"); n != 1 {
				t.Errorf("%q produced %d terminators: %q", body, n, got)
			}
			if n := strings.Count(got, ";"); n != 3 {
				t.Errorf("%q produced %d separators: %q", body, n, got)
			}
		})
	}
}

// The title comes off the wire too — it is a chat name.
func TestASenderCannotBreakOutViaTheTitle(t *testing.T) {
	got := notifier(MethodAuto, TerminalTitleAndBody).Notify("Ana;evil", "hi")

	if n := strings.Count(got, ";"); n != 3 {
		t.Errorf("a chat name added a separator: %q", got)
	}
}

// Disabled means disabled, and a notification with nothing in it is not
// worth a sequence.
func TestNothingToSayProducesNothing(t *testing.T) {
	off := NewNotifier(false, true, MethodTerminal)
	off.terminal = TerminalTitleAndBody
	if got := off.Notify("Ana", "hi"); got != "" {
		t.Errorf("a disabled notifier produced %q", got)
	}

	if got := notifier(MethodAuto, TerminalTitleAndBody).Notify("", "\x1b\x1b"); got != "" {
		t.Errorf("an empty notification produced %q", got)
	}
}

// The preview switch has to survive the rest of this: it is the setting
// somebody uses because their screen is visible to other people.
func TestPreviewOffReplacesTheBody(t *testing.T) {
	n := NewNotifier(true, false, MethodTerminal)
	n.terminal = TerminalTitleAndBody

	got := n.Notify("Ana", "the actual private message")
	if strings.Contains(got, "actual") {
		t.Errorf("preview off still leaked the message: %q", got)
	}
	if !strings.Contains(got, "Ana") {
		t.Errorf("preview off dropped the sender too: %q", got)
	}
}

func TestResolveMethod(t *testing.T) {
	tests := map[string]string{
		"terminal":  MethodTerminal,
		"system":    MethodSystem,
		" SYSTEM ":  MethodSystem,
		"auto":      MethodAuto,
		"":          MethodAuto,
		"osascript": MethodAuto,
	}
	for in, want := range tests {
		if got := ResolveMethod(in); got != want {
			t.Errorf("ResolveMethod(%q) = %q, want %q", in, got, want)
		}
	}
}

// A multiplexer sits between this process and the terminal and will either
// eat the sequence or print it, depending on configuration that cannot be
// read from here.
func TestAMultiplexerIsNotSentASequence(t *testing.T) {
	tests := []struct{ env, value string }{
		{"TMUX", "/tmp/tmux-501/default,1,0"},
		{"TERM", "screen-256color"},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", "ghostty")
			t.Setenv(tt.env, tt.value)

			if got := detectTerminal(); got != TerminalNone {
				t.Errorf("under %s the terminal is %v, want none", tt.env, got)
			}
		})
	}
}

func TestDetectTerminal(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want TerminalSupport
	}{
		{"ghostty", map[string]string{"TERM_PROGRAM": "ghostty"}, TerminalTitleAndBody},
		{"kitty", map[string]string{"TERM": "xterm-kitty"}, TerminalTitleAndBody},
		{"foot", map[string]string{"TERM": "foot"}, TerminalTitleAndBody},
		{"iTerm2", map[string]string{"TERM_PROGRAM": "iTerm.app"}, TerminalBodyOnly},
		{"Windows Terminal", map[string]string{"WT_SESSION": "abc"}, TerminalBodyOnly},
		{"Terminal.app", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, TerminalNone},
		{"plain xterm", map[string]string{"TERM": "xterm-256color"}, TerminalNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TMUX", "")
			t.Setenv("TERM", "")
			t.Setenv("TERM_PROGRAM", "")
			t.Setenv("WT_SESSION", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			if got := detectTerminal(); got != tt.want {
				t.Errorf("%s detected as %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
