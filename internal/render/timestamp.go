package render

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// clock is the source of "now" for every timestamp this package formats.
type clock func() time.Time

// pinned holds a replacement clock, or nil for the real one.
//
// A seam rather than a parameter. Almost every label here is RELATIVE — "2m",
// "yd", "TODAY" — so the same message renders differently every minute, and a
// frame that cannot be reproduced cannot be asserted against a golden. The
// alternative is threading a time through every component that draws a
// timestamp, which is most of them, to serve one caller.
//
// Production never writes it. An atomic rather than a plain variable so a
// test that pins the clock cannot race the renderer under -race.
var pinned atomic.Pointer[clock]

// Now is the instant the renderer measures against: the real clock, unless
// a test has pinned one.
func Now() time.Time {
	if c := pinned.Load(); c != nil {
		return (*c)()
	}
	return time.Now()
}

// PinClock fixes the renderer's clock at t and returns the function that
// restores the previous one. Callers must restore it — a leaked pin makes
// every later test in the process render at somebody else's instant.
func PinClock(t time.Time) (restore func()) {
	return PinClockFunc(func() time.Time { return t })
}

// PinClockFunc is PinClock with a moving clock, for the tests that need one
// instant per call rather than one for the whole test.
func PinClockFunc(c clock) (restore func()) {
	previous := pinned.Swap(&c)
	return func() { pinned.Store(previous) }
}

// FormatTimestamp formats a Unix timestamp for display.
func FormatTimestamp(ts int32) string {
	t := time.Unix(int64(ts), 0).Local()
	now := Now()

	if sameDay(t, now) {
		return t.Format("15:04")
	}

	if sameDay(t, now.AddDate(0, 0, -1)) {
		return "Yesterday " + t.Format("15:04")
	}

	if now.Sub(t) < 7*24*time.Hour {
		return t.Format("Mon 15:04")
	}

	if t.Year() == now.Year() {
		return t.Format("Jan 02")
	}

	return t.Format("2006-01-02")
}

// FormatTimestampSmart formats a Unix timestamp using a compact, date-aware
// form suitable for lists (e.g. the chat list) where a full timestamp on
// every row would be redundant: messages from today show only the time,
// messages from the last 6 calendar days show a short weekday plus time,
// and anything older falls back to an ISO date.
func FormatTimestampSmart(ts int32) string {
	t := time.Unix(int64(ts), 0).Local()
	now := Now().Local()

	// Calendar days, not raw 24h durations — see calendarDaysBetween.
	daysAgo := calendarDaysBetween(t, now)

	switch {
	case daysAgo == 0:
		return t.Format("15:04")
	case daysAgo >= 1 && daysAgo <= 6:
		return t.Format("Mon 15:04")
	default:
		return t.Format("2006-01-02")
	}
}

// FormatClock is the thread grid's time column: bare wall-clock, always
// five cells, no date part. The grid puts the date in a day divider
// instead, so repeating it on every row would only cost body width.
func FormatClock(ts int32) string {
	return time.Unix(int64(ts), 0).Local().Format("15:04")
}

// FormatDayLabel is the label of a thread grid day divider. It is upper
// case because a divider is a rule with a word set into it, not a sentence.
//
// The year is dropped within the current year: the divider's job is to say
// which day the messages under it belong to, and "12 AUG" answers that for
// anything recent while "12 AUG 2024" is what you need once it does not.
func FormatDayLabel(ts int32) string {
	t := time.Unix(int64(ts), 0).Local()
	now := Now().Local()

	switch days := calendarDaysBetween(t, now); {
	case days == 0:
		return "TODAY"
	case days == 1:
		return "YESTERDAY"
	case days <= 6:
		// Within the week the weekday is the part a reader actually
		// navigates by — "that was Tuesday" is how anyone remembers a
		// conversation from three days ago, and "26 AUG" makes them count
		// backwards to find out whether it was.
		return strings.ToUpper(t.Format("Mon 2 Jan"))
	case t.Year() == now.Year():
		return strings.ToUpper(t.Format("2 Jan"))
	default:
		return strings.ToUpper(t.Format("2 Jan 2006"))
	}
}

// SameDay reports whether two Unix timestamps fall on the same local
// calendar day. The thread grid uses it to decide where day dividers go.
func SameDay(a, b int32) bool {
	return sameDay(time.Unix(int64(a), 0).Local(), time.Unix(int64(b), 0).Local())
}

// FormatRelativeTime returns a relative time string (e.g., "2m ago").
func FormatRelativeTime(ts int32) string {
	t := time.Unix(int64(ts), 0)
	d := Now().Sub(t)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return FormatTimestamp(ts)
	}
}

// FormatRelativeShort is a relative time in the smallest space that still
// says something: "now", "4m", "4h", "yd", "2d", or a date once weeks have
// passed.
//
// The rail's right-hand field is four cells and the chat list's is five.
// "4h ago" does not fit beside a name, and the "ago" is the half that
// carries no information — a relative time is already relative.
//
// Hours give way to days at the DAY boundary, not at twenty-four hours.
// "23h" for something sent at nine last night is arithmetic; what the
// reader wants to know is whether it happened today, and a message from
// yesterday evening reading "14h" while one from this morning reads "6h"
// puts them on the same scale when the useful distinction is the night
// between them.
func FormatRelativeShort(ts int32) string {
	if ts == 0 {
		return ""
	}
	then := time.Unix(int64(ts), 0).Local()
	now := Now().Local()

	if d := now.Sub(then); d < time.Hour {
		if d < time.Minute {
			return "now"
		}
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}

	switch days := calendarDaysBetween(then, now); {
	case days <= 0:
		return fmt.Sprintf("%dh", int(now.Sub(then).Hours()))
	case days == 1:
		return "yd"
	case days <= 6:
		return fmt.Sprintf("%dd", days)
	default:
		return then.Format("2 Jan")
	}
}

// calendarDaysBetween is how many local calendar days separate then and now.
//
// Both dates are re-anchored in UTC before subtracting, for the reason
// FormatTimestampSmart gives at length: a local day crossing a DST
// transition is 23 or 25 hours long, and dividing a raw duration by 24
// hours puts an actual "yesterday" on the wrong side of the boundary twice
// a year.
func calendarDaysBetween(then, now time.Time) int {
	ty, tm, td := then.Date()
	ny, nm, nd := now.Date()
	return int(time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC).
		Sub(time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)) / (24 * time.Hour))
}

// FormatLastSeen formats a user's last seen timestamp.
func FormatLastSeen(ts int32) string {
	if ts == 0 {
		return "unknown"
	}
	return "last seen " + FormatRelativeTime(ts)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
