package clipboard

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ErrNoWriter is returned when the platform has no clipboard writer this
// build knows how to drive.
var ErrNoWriter = errors.New("no clipboard tool found for copying text")

// copyTimeout bounds a helper that never returns. wl-copy in particular
// forks a server process that owns the selection for as long as the data is
// offered, and a reader waiting on its stdout would wait forever.
const copyTimeout = 5 * time.Second

// Copy puts text on the system clipboard.
//
// Text only. There is no OSC 52 fallback: writing a base64 payload to the
// terminal to have it set the clipboard would work over ssh, where none of
// these helpers exist, but it is also the one clipboard path a user cannot
// see, cannot bound, and cannot decline — several terminals accept it
// silently and some multiplexers forward it onward. The design record
// (docs/tui-2.0.md, phase 8) gates it on explicit approval, which it has not
// been given, so an ssh session gets an honest "no clipboard tool found"
// rather than a surprise.
//
// An empty string is a no-op rather than an error: "copy nothing" is what
// yanking an empty message means, and clearing the user's clipboard is not
// a service they asked for.
func Copy(text string) error {
	if text == "" {
		return nil
	}
	return writeText(text)
}

// runCopy feeds text to a helper on stdin, bounded by copyTimeout.
//
// It is shared by every platform's writeText, so the timeout and the
// stderr-in-the-error behaviour cannot drift between them.
func runCopy(text string, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(text)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return timeoutErr(ctx, name, err)
		}
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New(name + ": " + msg)
		}
		return err
	}
	return nil
}
