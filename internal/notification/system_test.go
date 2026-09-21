package notification

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

// systemNotifier captures what would reach notify-send or osascript.
func systemNotifier(t *testing.T) (*Notifier, func() (string, string)) {
	t.Helper()

	n := NewNotifier(true, true, MethodSystem)
	done := make(chan [2]string, 1)
	n.system = func(title, body string) error {
		done <- [2]string{title, body}
		return nil
	}
	t.Cleanup(n.Close)

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

// drain lets the notifier's processes exit and returns once its worker has
// posted everything it was given. Waiting rather than closing: Close drops
// whatever is still waiting, which is what these tests are about.
func drain(n *Notifier, p *process) {
	p.exit()
	n.queue.wait()
}

// A burst — a busy group, or the backlog replayed after a reconnect — used
// to start a notifier process per message, all at once. One runs at a time
// now, and everything that arrives meanwhile waits behind it as a single
// notification that says how many messages it stands for.
func TestABurstRunsOneNotifierAtATime(t *testing.T) {
	n := NewNotifier(true, true, MethodSystem)
	p := newProcess()
	n.system = p.notify

	for i := range 50 {
		n.Notify("Ana", fmt.Sprintf("message %d", i))
	}
	p.awaitStart(t)
	drain(n, p)

	runs, peak := p.report()
	if peak != 1 {
		t.Errorf("%d notifier processes ran at once, want 1", peak)
	}
	if len(runs) != 2 {
		t.Fatalf("50 messages started %d notifier processes, want 2 — the first, and one for the rest: %q",
			len(runs), runs)
	}
	if want := [2]string{"Ana", "message 0"}; runs[0] != want {
		t.Errorf("the first message was posted as %q, want %q", runs[0], want)
	}
	if want := [2]string{"tele-tui", "49 new messages"}; runs[1] != want {
		t.Errorf("the rest were posted as %q, want %q", runs[1], want)
	}
}

// Coalescing is for bursts, and must not cost a lone message its promptness:
// it goes out at once, as itself, without waiting to see whether another
// follows.
func TestAnIsolatedMessageIsPostedAtOnce(t *testing.T) {
	n := NewNotifier(true, true, MethodSystem)
	p := newProcess()
	n.system = p.notify

	n.Notify("Ana", "see you at six")
	p.awaitStart(t)
	drain(n, p)

	runs, _ := p.report()
	if want := [2]string{"Ana", "see you at six"}; len(runs) != 1 || runs[0] != want {
		t.Errorf("one message was posted as %q, want exactly [%q]", runs, want)
	}
}

// A count is for when there is something to count. One message waiting
// behind another is still one message, and says who it is from.
func TestOneMessageWaitingKeepsItsOwnWords(t *testing.T) {
	n := NewNotifier(true, true, MethodSystem)
	p := newProcess()
	n.system = p.notify

	n.Notify("Ana", "see you at six")
	p.awaitStart(t)
	n.Notify("Ben", "running late")
	drain(n, p)

	runs, _ := p.report()
	if len(runs) != 2 {
		t.Fatalf("%d notifier processes for 2 messages, want 2: %q", len(runs), runs)
	}
	if want := [2]string{"Ben", "running late"}; runs[1] != want {
		t.Errorf("the message that waited was posted as %q, want %q", runs[1], want)
	}
}

// Close is for shutting down, and a process started after it would outlive
// whatever asked for it.
func TestAClosedNotifierStartsNothing(t *testing.T) {
	n := NewNotifier(true, true, MethodSystem)
	p := newProcess()
	p.exit()
	n.system = p.notify

	n.Close()
	n.Notify("Ana", "hi")
	n.Close()

	if runs, _ := p.report(); len(runs) != 0 {
		t.Errorf("a closed notifier started %q", runs)
	}
}

// Where there is no notifier to run, the fallback is the terminal bell — and
// the bell is a byte for the terminal, which this process does not own. It
// goes back to the caller for tea.Raw like any other sequence: printed from
// the background, it landed wherever the renderer happened to be mid-frame.
func TestAPlatformWithoutANotifierHandsBackTheBell(t *testing.T) {
	n := NewNotifier(true, true, MethodSystem)
	n.system = platformNotifier("plan9", installed("notify-send", "osascript"))

	if got := n.Notify("Ana", "hi"); got != "\a" {
		t.Errorf("Notify = %q, want the bell handed back", got)
	}
}

// A burst with no notifier to run used to ring the bell once per message:
// fifty bells for a busy group. It rings once now, and once more the
// interval after.
func TestABurstWithoutANotifierRingsOnce(t *testing.T) {
	n := NewNotifier(true, true, MethodSystem)
	n.system = nil
	clock := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	n.bells = newBellLimiter(func() time.Time { return clock })

	var rang int
	for i := range 50 {
		if n.Notify("Ana", fmt.Sprintf("message %d", i)) == "\a" {
			rang++
		}
	}
	if rang != 1 {
		t.Errorf("a burst of 50 rang the bell %d times, want 1", rang)
	}

	clock = clock.Add(minSoundInterval)
	if got := n.Notify("Ana", "later"); got != "\a" {
		t.Errorf("a message the interval later got %q, want the bell", got)
	}
}

// Whether there is a notifier is decided up front, by looking. A missing
// notify-send used to be found out by running it, in the background, where
// the only fallback left was to print.
func TestThePlatformNotifierIsTheOneInstalled(t *testing.T) {
	tests := []struct {
		goos      string
		installed []string
		want      bool
	}{
		{"linux", []string{"notify-send"}, true},
		{"linux", nil, false},
		{"darwin", []string{"osascript"}, true},
		{"darwin", nil, false},
	}

	for _, tt := range tests {
		got := platformNotifier(tt.goos, installed(tt.installed...)) != nil
		if got != tt.want {
			t.Errorf("on %s with %q installed, a notifier: %v, want %v",
				tt.goos, tt.installed, got, tt.want)
		}
	}
}

// A notify-send that is installed but fails — no notification daemon — is
// found out in the background, and the background has no business writing
// to the terminal. It used to ring the bell there, mid-frame.
func TestAFailingNotifierPrintsNothing(t *testing.T) {
	failingPrograms(t, "notify-send")

	if out := stdout(t, func() { sendLinux("Ana", "hi") }); out != "" {
		t.Errorf("a failing notify-send wrote %q to the terminal", out)
	}
}

// An installed notify-send that fails — ssh to a box with libnotify but no
// notification daemon or session bus — used to ring the bell from the
// background: wrong, but the reader's only alert. Swallowing the failure left
// them with nothing at all. So once a run has failed, the next message hands
// the bell back to the caller, as a missing notifier does, and within the
// same limit.
func TestAFailingNotifierFallsBackToTheBell(t *testing.T) {
	failingPrograms(t, "notify-send")
	n := NewNotifier(true, true, MethodSystem)
	n.system = platformNotifier("linux", exec.LookPath)
	clock := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	n.bells = newBellLimiter(func() time.Time { return clock })
	defer n.Close()

	if got := n.Notify("Ana", "one"); got != "" {
		t.Fatalf("precondition: the first message was handed %q before any run had failed", got)
	}
	n.queue.wait()

	if got := n.Notify("Ana", "two"); got != "\a" {
		t.Errorf("after a failed run the next message was handed %q, want the bell", got)
	}
	n.queue.wait()
	if got := n.Notify("Ana", "three"); got != "" {
		t.Errorf("inside the bell's limit the one after was handed %q, want nothing", got)
	}
}

// The fallback lasts as long as the failure does. The notifier is still tried
// while the bell stands in, and once a run works — a daemon started since,
// a session bus that came back — the bell stops.
func TestANotifierThatWorksAgainStopsTheBell(t *testing.T) {
	n := NewNotifier(true, true, MethodSystem)
	results := []error{errors.New("no daemon"), nil}
	var runs int
	n.system = func(string, string) error {
		err := results[min(runs, len(results)-1)]
		runs++
		return err
	}
	clock := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	n.bells = newBellLimiter(func() time.Time { return clock })
	defer n.Close()

	n.Notify("Ana", "one") // fails
	n.queue.wait()
	if got := n.Notify("Ana", "two"); got != "\a" { // works
		t.Fatalf("precondition: after a failed run the next message was handed %q", got)
	}
	n.queue.wait()

	clock = clock.Add(minSoundInterval)
	if got := n.Notify("Ana", "three"); got != "" {
		t.Errorf("after a run that worked the next message was handed %q, want nothing", got)
	}
}

// On a machine with nothing to run, the fallback comes back to the caller
// and nothing is written from here — the whole of the rule, end to end,
// through the constructors and the call the app uses.
func TestWithNothingToRunTheBellComesBackAndNothingIsPrinted(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	n := NewNotifier(true, true, MethodSystem)
	s := NewSoundPlayer(true)

	var alerted string
	out := stdout(t, func() {
		alerted = Alert(n, s, "Ana", "hi")
		n.Close()
		s.Close()
	})

	if alerted != "\a" {
		t.Errorf("Alert = %q, want the bell", alerted)
	}
	if out != "" {
		t.Errorf("%q was written to the terminal", out)
	}
}

// A notifier that goes to the terminal must not ALSO hand the text to the
// system: one message, one alert.
func TestOnlyOnePathDelivers(t *testing.T) {
	n := NewNotifier(true, true, MethodAuto)
	n.terminal = TerminalTitleAndBody

	delivered := make(chan struct{}, 1)
	n.system = func(string, string) error {
		delivered <- struct{}{}
		return nil
	}

	if seq := n.Notify("Ana", "hi"); seq == "" {
		t.Fatal("the terminal path produced no sequence")
	}
	select {
	case <-delivered:
		t.Error("the system notifier was also asked to post it")
	default:
	}
}
