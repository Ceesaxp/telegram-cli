package notification

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
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
	system func(title, body string)

	// queue is what stands between a burst of messages and a burst of
	// processes.
	queue *coalescer

	// bells limits the bell rung where there is no notifier to run, so a
	// burst rings it once rather than once a message.
	bells *bellLimiter
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
	n.system = platformNotifier(runtime.GOOS, exec.LookPath)
	// Through n rather than bound now, so the queue delivers to whatever
	// the seam holds when it runs.
	n.queue = newCoalescer(func(title, body string) { n.system(title, body) })
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

	// In the background: notify-send and osascript are processes, and
	// waiting on one would stall the event loop for as long as the desktop
	// takes to answer.
	n.queue.post(title, body)
	return ""
}

// Close stops the system path from starting anything new, drops the
// notification waiting to be posted, if any, and waits for a notifier
// process that is still running.
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
func platformNotifier(goos string, lookPath func(string) (string, error)) func(title, body string) {
	var (
		program string
		send    func(title, body string)
	)
	switch goos {
	case "linux":
		program, send = "notify-send", sendLinux
	case "darwin":
		program, send = "osascript", sendMacOS
	default:
		return nil
	}

	if _, err := lookPath(program); err != nil {
		return nil
	}
	return send
}

// sendLinux posts through notify-send. A failure is not reported: it is
// installed, so the likeliest cause is a desktop with no notification
// daemon, and there is nothing this side could do about that from the
// background.
func sendLinux(title, body string) {
	cmd := exec.Command("notify-send",
		"--app-name=Tele-TUI",
		"--icon=telegram",
		"--urgency=normal",
		title,
		body,
	)
	cmd.Run()
}

// sendMacOS is the path that posts as Script Editor.
//
// It is kept as the fallback for Terminal.app, which implements no
// notification sequence at all, and for a user who prefers the system's own
// alert. See terminal.go for why a CLI cannot do better here without
// shipping an app bundle.
func sendMacOS(title, body string) {
	script := fmt.Sprintf(
		`display notification %q with title %q`,
		body, title,
	)
	cmd := exec.Command("osascript", "-e", script)
	cmd.Run()
}
