package telegram

import (
	"context"
	"fmt"
	"io"
	stdlog "log"
	"strings"

	gotdlog "github.com/gotd/log"
)

// gotdLogger bridges gotd's structured logger onto the standard log, which
// is where TELETUI_DEBUG points. Without it the update manager's own
// account of itself — every getDifference and why, gaps, "difference too
// long", seq validation failures — went to a no-op logger, and a message
// that never arrived left no trace anywhere.
//
// Enabled is answered from the log writer, so with logging off (the
// default: io.Discard) gotd skips building attributes on its hot paths.
type gotdLogger string

var _ gotdlog.Logger = gotdLogger("")

func (l gotdLogger) Enabled(context.Context, gotdlog.Level) bool {
	return stdlog.Writer() != io.Discard
}

func (l gotdLogger) Log(ctx context.Context, level gotdlog.Level, msg string, attrs ...gotdlog.Attr) {
	if !l.Enabled(ctx, level) {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "gotd/%s %s: %s", string(l), level, msg)
	for _, a := range attrs {
		fmt.Fprintf(&b, " %s=%s", a.Key, a.Value.String())
	}
	stdlog.Print(b.String())
}
