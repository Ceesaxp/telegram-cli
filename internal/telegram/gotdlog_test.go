package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	stdlog "log"
	"strings"
	"testing"

	gotdlog "github.com/gotd/log"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	prev := stdlog.Writer()
	var buf bytes.Buffer
	stdlog.SetOutput(&buf)
	t.Cleanup(func() { stdlog.SetOutput(prev) })
	return &buf
}

// The reason gotd fetched a difference is the one fact that tells a
// missed message apart from a message that never existed; it has to reach
// the debug log with its attributes, not just its headline.
func TestGotdLoggerWritesRecordsToTheStandardLog(t *testing.T) {
	buf := captureLog(t)
	l := gotdLogger("updates")

	if !l.Enabled(context.Background(), gotdlog.LevelDebug) {
		t.Fatal("logger must be enabled while the standard log has a writer")
	}
	l.Log(context.Background(), gotdlog.LevelDebug, "Getting difference",
		gotdlog.String("reason", "short-message-peer-access-hash-unknown"),
		gotdlog.Int64("pts", 12345),
		gotdlog.Error(errors.New("boom")),
	)

	out := buf.String()
	for _, want := range []string{
		"gotd/updates DEBUG: Getting difference",
		"reason=short-message-peer-access-hash-unknown",
		"pts=12345",
		"boom",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log output %q lacks %q", out, want)
		}
	}
}

// Logging is off by default, and gotd checks Enabled before it builds
// attributes: the bridge must say no rather than format into the void.
func TestGotdLoggerIsSilentWhenTheLogIsDiscarded(t *testing.T) {
	prev := stdlog.Writer()
	stdlog.SetOutput(io.Discard)
	t.Cleanup(func() { stdlog.SetOutput(prev) })

	if gotdLogger("updates").Enabled(context.Background(), gotdlog.LevelError) {
		t.Fatal("logger must report disabled while the standard log discards")
	}
}
