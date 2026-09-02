package render

import (
	"testing"
	"time"
)

// TestPinClockFixesTheRenderersNow is what every golden frame rests on: the
// labels in this package are relative, so a fixture can only be compared
// against a render taken at a known instant.
func TestPinClockFixesTheRenderersNow(t *testing.T) {
	want := time.Date(2025, time.August, 29, 21, 4, 0, 0, time.UTC)

	restore := PinClock(want)
	if got := Now(); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
	restore()

	if got := Now(); got.Equal(want) {
		t.Fatal("the pin outlived its restore")
	}
	if time.Since(Now()) > time.Minute {
		t.Fatalf("Now() = %v, which is not the wall clock", Now())
	}
}

// TestPinsNest, because one test pinning inside another must put back what
// it found rather than the real clock.
func TestPinsNest(t *testing.T) {
	outer := time.Date(2025, time.August, 29, 21, 4, 0, 0, time.UTC)
	inner := outer.Add(time.Hour)

	restoreOuter := PinClock(outer)
	defer restoreOuter()

	restoreInner := PinClock(inner)
	if got := Now(); !got.Equal(inner) {
		t.Fatalf("Now() = %v, want the inner pin %v", got, inner)
	}
	restoreInner()

	if got := Now(); !got.Equal(outer) {
		t.Fatalf("Now() = %v, want the outer pin %v back", got, outer)
	}
}

// at is a timestamp relative to the pinned clock.
func at(base time.Time, d time.Duration) int32 { return int32(base.Add(-d).Unix()) }

// TestRelativeShortCountsCalendarDays. "23h" for something sent at nine
// last night is arithmetic; what a reader wants to know is whether it
// happened today.
func TestRelativeShortCountsCalendarDays(t *testing.T) {
	// A Friday at 21:04, so "yesterday" and "two days ago" are unambiguous.
	now := time.Date(2025, time.August, 29, 21, 4, 0, 0, time.UTC)
	defer PinClock(now)()

	local := time.Local
	time.Local = time.UTC
	defer func() { time.Local = local }()

	cases := map[string]struct {
		ago  time.Duration
		want string
	}{
		"seconds":             {30 * time.Second, "now"},
		"minutes":             {2 * time.Minute, "2m"},
		"an hour":             {time.Hour, "1h"},
		"this morning":        {13 * time.Hour, "13h"},
		"last night":          {23 * time.Hour, "yd"},
		"yesterday afternoon": {26 * time.Hour, "yd"},
		"the day before":      {50 * time.Hour, "2d"},
		"within the week":     {5 * 24 * time.Hour, "5d"},
		"older than that":     {20 * 24 * time.Hour, "9 Aug"},
	}
	for name, tc := range cases {
		if got := FormatRelativeShort(at(now, tc.ago)); got != tc.want {
			t.Errorf("%s (%v ago) = %q, want %q", name, tc.ago, got, tc.want)
		}
	}

	if got := FormatRelativeShort(0); got != "" {
		t.Errorf("a message with no date = %q, want empty", got)
	}
}

// TestDayDividersNameTheWeekdayWithinTheWeek. "That was Tuesday" is how
// anyone remembers a conversation from three days ago; "26 AUG" makes them
// count backwards to find out whether it was.
func TestDayDividersNameTheWeekdayWithinTheWeek(t *testing.T) {
	now := time.Date(2025, time.August, 29, 21, 4, 0, 0, time.UTC)
	defer PinClock(now)()

	local := time.Local
	time.Local = time.UTC
	defer func() { time.Local = local }()

	cases := map[time.Duration]string{
		2 * time.Hour:        "TODAY",
		26 * time.Hour:       "YESTERDAY",
		3 * 24 * time.Hour:   "TUE 26 AUG",
		6 * 24 * time.Hour:   "SAT 23 AUG",
		20 * 24 * time.Hour:  "9 AUG",
		400 * 24 * time.Hour: "25 JUL 2024",
	}
	for ago, want := range cases {
		if got := FormatDayLabel(at(now, ago)); got != want {
			t.Errorf("%v ago = %q, want %q", ago, got, want)
		}
	}
}
