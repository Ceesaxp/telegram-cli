package notification

import (
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// englandFlag is a flag whose tag characters Go's %q does not print, and
// spells as \U000e0067 — an escape AppleScript does not know.
const englandFlag = "\U0001F3F4\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F"

// hostileTitle and hostileBody are text a sender controls, made to break
// whichever way a notifier could misread it: a leading dash an option parser
// would take, quotes and a backslash that would end or escape a string in
// code, and the flag.
const (
	hostileTitle = `-e "Ana" \ ` + englandFlag
	hostileBody  = `-w 'six' "o'clock" \n ` + englandFlag
)

// ran is what a runner was asked to run.
type ran struct {
	name string
	args []string
}

// recording is a runner that runs nothing and notes what it was asked to.
func recording(into *ran) runner {
	return func(name string, args ...string) error {
		*into = ran{name, args}
		return nil
	}
}

// notify-send takes the title and body as positional arguments, after its
// options, so a message that starts with a dash was read as one: "-w" made
// it wait for the notification to close. "--" ends the options first.
func TestNotifySendGetsTheTextAfterItsOptions(t *testing.T) {
	var got ran
	_ = sendLinux(recording(&got), hostileTitle, hostileBody)

	want := ran{"notify-send", []string{
		"--app-name=Tele-TUI", "--icon=telegram", "--urgency=normal",
		"--", hostileTitle, hostileBody,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notify-send was run as\n%q\nwant\n%q", got, want)
	}
}

// On macOS the text used to be spliced into AppleScript source with %q. For
// the flag that wrote \U000e0067, the script did not compile, and the
// notification was lost — and, the run having failed, the next message rang.
// The script is fixed now, and the text reaches it as arguments.
func TestOsascriptGetsTheTextAsArguments(t *testing.T) {
	var got ran
	_ = sendMacOS(recording(&got), hostileTitle, hostileBody)

	want := ran{"osascript", []string{
		"-e", "on run argv",
		"-e", "display notification (item 1 of argv) with title (item 2 of argv)",
		"-e", "end run",
		"--", hostileBody, hostileTitle,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("osascript was run as\n%q\nwant\n%q", got, want)
	}
}

// And osascript really does hand those arguments to the script untouched —
// the leading -e, the quotes, the flag. Checked with the same arguments and
// a script that returns them instead of posting, so nothing reaches the
// desktop.
func TestOsascriptHandsTheTextThroughUntouched(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("osascript is macOS's")
	}
	const osascript = "/usr/bin/osascript"
	if _, err := os.Stat(osascript); err != nil {
		t.Skip("no osascript here")
	}

	var got ran
	_ = sendMacOS(recording(&got), hostileTitle, hostileBody)
	args := slices.Clone(got.args)
	const post = "display notification (item 1 of argv) with title (item 2 of argv)"
	i := slices.Index(args, post)
	if i < 0 {
		t.Fatalf("the script does not post the way this test expects, so it is not safe to run: %q", args)
	}
	args[i] = "return (item 1 of argv) & linefeed & (item 2 of argv)"

	out, err := exec.Command(osascript, args...).Output()
	if err != nil {
		t.Fatalf("osascript would not run the script: %v", err)
	}
	if got, want := strings.TrimSuffix(string(out), "\n"), hostileBody+"\n"+hostileTitle; got != want {
		t.Errorf("the script was handed\n%q\nwant\n%q", got, want)
	}
}
