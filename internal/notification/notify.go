package notification

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// Delivery methods for [NewNotifier].
const (
	// MethodAuto uses the terminal when it is known to understand the
	// sequence, and the system otherwise. The default.
	MethodAuto = "auto"
	// MethodTerminal always asks the terminal, for one the allowlist does
	// not know. A terminal that does not understand it prints it.
	MethodTerminal = "terminal"
	// MethodSystem always uses the platform's own notifier — notify-send
	// on Linux, osascript on macOS, which posts as Script Editor.
	MethodSystem = "system"
)

// ResolveMethod normalises a configured value, falling back to the default
// for an empty or unrecognised one: a typo in a cosmetic setting should cost
// the user the setting, not the client.
func ResolveMethod(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case MethodTerminal:
		return MethodTerminal
	case MethodSystem:
		return MethodSystem
	default:
		return MethodAuto
	}
}

// Notifier sends desktop notifications.
type Notifier struct {
	enabled     bool
	showPreview bool
	method      string
	terminal    TerminalSupport

	// system delivers through the platform notifier, and is a field so a
	// test can see what reaches it. That matters more than it looks:
	// whether this path gets the text or an OSC-escaped version of it is
	// the whole point of the split in terminal.go, and the real
	// implementation is a process that has already exited by the time
	// anything could ask. Nil where there is no notifier installed.
	system func(title, body string) error

	// queue is what stands between a burst of messages and a burst of
	// processes.
	queue *coalescer

	// bells limits the bell rung where there is no notifier to run, so a
	// burst rings it once rather than once a message.
	bells *bellLimiter

	// failed says the last notifier run failed, and a run that works clears
	// it: the helper is still tried while the bell stands in, so a daemon
	// that starts later is noticed. The worker writes it; Notify, on the
	// event loop, reads it.
	failed atomic.Bool
}

// NewNotifier creates a new notification dispatcher.
func NewNotifier(enabled, showPreview bool, method string) *Notifier {
	n := &Notifier{
		enabled:     enabled,
		showPreview: showPreview,
		method:      ResolveMethod(method),
		terminal:    detectTerminal(),
		bells:       newBellLimiter(time.Now),
	}
	n.system = platformNotifier(runtime.GOOS, exec.LookPath, helperTimeout)
	// Through n rather than bound now, so the queue delivers to whatever
	// the seam holds when it runs.
	n.queue = newCoalescer(func(title, body string) {
		n.failed.Store(n.system(title, body) != nil)
	})
	return n
}

// Notify posts a desktop notification and returns the escape sequence the
// caller must write to the terminal, or "" when there is nothing to write.
//
// It returns a sequence rather than writing one because this process does not
// own the terminal: Bubble Tea does, and a goroutine writing to the same file
// descriptor can land in the middle of a frame. The caller hands the result
// to tea.Raw, which puts it in the renderer's own output buffer, under the
// renderer's own lock, between frames.
//
// The system path has no such problem and is taken here, in the background —
// notify-send and osascript are processes, and waiting on one would stall the
// event loop for as long as the desktop takes to answer. One of them runs at
// a time: what arrives meanwhile is folded into a single notification behind
// it, so a burst of messages costs two processes rather than one each.
func (n *Notifier) Notify(title, body string) string {
	if !n.enabled {
		return ""
	}
	if !n.showPreview {
		body = "New message"
	}

	title, body = sanitizeText(title), sanitizeText(body)
	if title == "" && body == "" {
		return ""
	}

	if seq, ok := n.terminalSequence(title, body); ok {
		return seq
	}

	if n.system == nil {
		// Nothing to run, so the terminal is asked to ring instead — by
		// the caller, like any other write to it, and once for a burst.
		return n.bells.ring()
	}

	// Read before posting, not after: the post can start a run that
	// finishes, and rewrites the flag, before a later read. Read first, it
	// says what the runs that finished before this message found.
	failing := n.failed.Load()

	// In the background: notify-send and osascript are processes, and
	// waiting on one would stall the event loop for as long as the desktop
	// takes to answer.
	n.queue.post(title, body)

	if failing {
		// The last run failed, and whatever made it fail is likely to
		// fail this one too. The bell stands in, as it does for a
		// notifier that is not there — from the caller, never from the
		// worker, which cannot write to the terminal.
		return n.bells.ring()
	}
	return ""
}

// Close stops the system path from starting anything new, drops the
// notification waiting to be posted, if any, and waits for a notifier
// process that is still running — at most about helperTimeout, after which
// that process is killed.
//
// Nothing in the app calls it, and nothing needs to: the worker only lives
// while a process does, and leaving that process behind at exit is what
// exiting has always done. The tests call it, so that no worker outlives
// the test that started it.
func (n *Notifier) Close() {
	n.queue.close()
}

// terminalSequence is the escape sequence for this notification, and whether
// the terminal is going to be asked at all.
func (n *Notifier) terminalSequence(title, body string) (string, bool) {
	support := n.terminal
	if n.method == MethodSystem {
		return "", false
	}
	if n.method == MethodTerminal && support == TerminalNone {
		// The user has said their terminal handles these even though the
		// allowlist does not know it. Believe them, and pick the form that
		// carries both fields.
		support = TerminalTitleAndBody
	}

	// Escaped here rather than by the caller: this is the only path where
	// the text ends up inside a sequence.
	title, body = sanitizeSequence(title), sanitizeSequence(body)

	switch support {
	case TerminalTitleAndBody:
		return fmt.Sprintf(osc777, title, body), true
	case TerminalBodyOnly:
		// One field, so the sender's name goes in front of the message.
		// Dropping it instead would leave "see you at six" with nothing
		// saying who from, which is the one thing a notification is for.
		text := body
		if title != "" {
			text = title + ": " + body
		}
		return fmt.Sprintf(osc9, text), true
	default:
		return "", false
	}
}

// platformNotifier is the platform's own notifier on goos: a function that
// posts one notification and returns once the process it runs has exited.
// It is nil where there is none to run.
//
// Whether there is one is looked up here, once, rather than found out by
// running it: by then the process is in the background, where the only
// fallback left is to print — to a terminal this process does not own.
func platformNotifier(goos string, lookPath func(string) (string, error), timeout time.Duration) func(title, body string) error {
	var (
		program string
		send    func(run runner, title, body string) error
	)
	switch goos {
	case "linux", "freebsd", "openbsd", "netbsd":
		// The BSDs run the same desktops, and libnotify's notify-send
		// with them.
		program, send = "notify-send", sendLinux
	case "darwin":
		program, send = "osascript", sendMacOS
	default:
		return nil
	}

	if _, err := lookPath(program); err != nil {
		return nil
	}
	run := timed(timeout)
	return func(title, body string) error { return send(run, title, body) }
}

// sendLinux posts through notify-send. It is installed, so a failure most
// likely means a desktop with no notification daemon behind it.
//
// The title and body are the sender's text, and positional: "--" ends the
// options first, so a message that starts with a dash is not read as one.
func sendLinux(run runner, title, body string) error {
	return run("notify-send",
		"--app-name=Tele-TUI",
		"--icon=telegram",
		"--urgency=normal",
		"--",
		title,
		body,
	)
}

// sendMacOS is the path that posts as Script Editor.
//
// It is kept as the fallback for Terminal.app, which implements no
// notification sequence at all, and for a user who prefers the system's own
// alert. See terminal.go for why a CLI cannot do better here without
// shipping an app bundle.
//
// The script is fixed and the text reaches it as arguments, never as part
// of its source. Quoted into the source with %q, a character Go does not
// print — the tag characters in a flag emoji — came out as \U000e0067,
// which AppleScript does not know, and the notification was lost.
func sendMacOS(run runner, title, body string) error {
	return run("osascript",
		"-e", "on run argv",
		"-e", "display notification (item 1 of argv) with title (item 2 of argv)",
		"-e", "end run",
		"--", body, title,
	)
}

// helperTimeout is how long a notifier or a sound player may run before it
// is killed.
//
// With one helper at a time, a helper that never exits — paplay on a wedged
// sound server, afplay on a Bluetooth output that went away, notify-send on
// a frozen daemon — would otherwise hold the bound for the rest of the
// session: every later notification folded into the one waiting, every
// later sound dropped. The helpers answer in well under a second and the
// sounds last a second or two, so ten seconds is far past any of them
// working, and short enough that a wedged one costs an alert rather than
// the session.
const helperTimeout = 10 * time.Second

// runner runs a helper program with its arguments and says whether it
// worked. The notifiers take one, so a test can see the exact arguments a
// message becomes without running anything.
type runner func(name string, args ...string) error

// timed is the runner that runs helpers for real, each for at most timeout.
func timed(timeout time.Duration) runner {
	return func(name string, args ...string) error {
		return runHelper(timeout, name, args...)
	}
}

// runHelper runs one of the programs this package leans on — a notifier or
// a sound player — to completion, or kills it after timeout, with anything
// it started, and says whether it worked. A helper that was killed did not.
//
// There is no WaitDelay. Its two jobs are closing the pipes of a helper
// that has gone quiet, and there are none — stdin and stdout are nil — and
// killing a helper that outlives its cancellation, which is already a
// SIGKILL. What survives that cannot be hurried by a second one.
func runHelper(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	killGroupOnCancel(cmd)
	return cmd.Run()
}
