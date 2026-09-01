package notification

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
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
}

// NewNotifier creates a new notification dispatcher.
func NewNotifier(enabled, showPreview bool, method string) *Notifier {
	return &Notifier{
		enabled:     enabled,
		showPreview: showPreview,
		method:      ResolveMethod(method),
		terminal:    detectTerminal(),
	}
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
// event loop for as long as the desktop takes to answer.
func (n *Notifier) Notify(title, body string) string {
	if !n.enabled {
		return ""
	}
	if !n.showPreview {
		body = "New message"
	}

	title, body = sanitize(title), sanitize(body)
	if title == "" && body == "" {
		return ""
	}

	if seq, ok := n.terminalSequence(title, body); ok {
		return seq
	}

	go n.sendSystem(title, body)
	return ""
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

func (n *Notifier) sendSystem(title, body string) {
	switch runtime.GOOS {
	case "linux":
		n.sendLinux(title, body)
	case "darwin":
		n.sendMacOS(title, body)
	default:
		// Unsupported platform.
	}
}

func (n *Notifier) sendLinux(title, body string) {
	// Try notify-send first.
	cmd := exec.Command("notify-send",
		"--app-name=Tele-TUI",
		"--icon=telegram",
		"--urgency=normal",
		title,
		body,
	)
	if err := cmd.Run(); err != nil {
		// Fallback: terminal bell.
		fmt.Print("\a")
	}
}

// sendMacOS is the path that posts as Script Editor.
//
// It is kept as the fallback for Terminal.app, which implements no
// notification sequence at all, and for a user who prefers the system's own
// alert. See terminal.go for why a CLI cannot do better here without
// shipping an app bundle.
func (n *Notifier) sendMacOS(title, body string) {
	script := fmt.Sprintf(
		`display notification %q with title %q`,
		body, title,
	)
	cmd := exec.Command("osascript", "-e", script)
	cmd.Run()
}
