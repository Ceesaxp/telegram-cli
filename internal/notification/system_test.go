package notification

import (
	"testing"
)

// systemNotifier captures what would reach notify-send or osascript.
func systemNotifier(t *testing.T) (*Notifier, func() (string, string)) {
	t.Helper()

	n := NewNotifier(true, true, MethodSystem)
	done := make(chan [2]string, 1)
	n.system = func(title, body string) { done <- [2]string{title, body} }

	return n, func() (string, string) {
		t.Helper()
		got := <-done
		return got[0], got[1]
	}
}

// The OSC escaping does not belong on the system path.
//
// notify-send and osascript take their arguments as arguments; there are no
// semicolon-separated fields to break out of. Escaping for them anyway turned
// "Meet at 6; bring food" into "Meet at 6, bring food" and flattened every
// multi-line message — mangling the text to protect against a syntax that
// path does not use.
func TestTheSystemPathKeepsThePunctuationItWasSent(t *testing.T) {
	n, wait := systemNotifier(t)

	n.Notify("Ana", "Meet at 6; bring food")

	title, body := wait()
	if want := "Meet at 6; bring food"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
	if title != "Ana" {
		t.Errorf("title = %q, want Ana", title)
	}
}

// And keeps the line breaks, which a desktop notifier renders.
func TestTheSystemPathKeepsLineBreaks(t *testing.T) {
	n, wait := systemNotifier(t)

	n.Notify("Ana", "first line\nsecond line")

	if _, body := wait(); body != "first line\nsecond line" {
		t.Errorf("body = %q, want the line break kept", body)
	}
}

// What it does NOT keep is control characters: those have no business in a
// notification whichever way it is delivered, and osascript would render
// them as literal backslash escapes.
func TestTheSystemPathStillStripsEscapes(t *testing.T) {
	n, wait := systemNotifier(t)

	n.Notify("Ana", "before\x1b]0;title\x07after")

	_, body := wait()
	for _, r := range body {
		if r < 0x20 && r != '\n' && r != '\t' {
			t.Errorf("a control character survived: %q", body)
			break
		}
	}
	if body == "" {
		t.Error("the whole body was dropped")
	}
}

// The sequence path still escapes, because it still has to.
func TestTheSequencePathStillEscapes(t *testing.T) {
	got := notifier(MethodAuto, TerminalTitleAndBody).Notify("Ana", "Meet at 6; bring food")

	if want := "\x1b]777;notify;Ana;Meet at 6, bring food\x1b\\"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A notifier that goes to the terminal must not ALSO hand the text to the
// system: one message, one alert.
func TestOnlyOnePathDelivers(t *testing.T) {
	n := NewNotifier(true, true, MethodAuto)
	n.terminal = TerminalTitleAndBody

	delivered := make(chan struct{}, 1)
	n.system = func(string, string) { delivered <- struct{}{} }

	if seq := n.Notify("Ana", "hi"); seq == "" {
		t.Fatal("the terminal path produced no sequence")
	}
	select {
	case <-delivered:
		t.Error("the system notifier was also asked to post it")
	default:
	}
}
