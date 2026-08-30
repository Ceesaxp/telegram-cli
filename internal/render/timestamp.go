package render

import (
	"fmt"
	"strings"
	"time"
)

// FormatTimestamp formats a Unix timestamp for display.
func FormatTimestamp(ts int32) string {
	t := time.Unix(int64(ts), 0).Local()
	now := time.Now()

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
	now := time.Now().Local()

	// Compare calendar days, not raw 24h durations: a Local-location day
	// crossing a DST transition can be 23 or 25 hours long, which would
	// make Sub(...).Hours()/24 truncate to the wrong day count (e.g. an
	// actual "yesterday" reading as 0 days ago across a spring-forward
	// transition). Re-anchor both dates' Y/M/D components in UTC — a
	// location with no DST — so the subtraction is always an exact
	// multiple of 24h and daysAgo comes out exact.
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	day := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	daysAgo := int(today.Sub(day) / (24 * time.Hour))

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
	now := time.Now().Local()

	switch {
	case sameDay(t, now):
		return "TODAY"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "YESTERDAY"
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
	d := time.Since(t)

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
