package app

import (
	"strings"
	"testing"
	"time"

	"github.com/Ceesaxp/telegram-cli/internal/ui/golden"
)

func hintBarRow(t *testing.T, m Model) string {
	t.Helper()
	lines := golden.SplitLines(m.View().Content)
	return lines[len(lines)-1]
}

// The clock showed the time of the last window resize.
//
// refreshChrome sets it, and refreshChrome only ran on a resize, on
// authentication, and on a folder-tab click — so on a terminal nobody
// resized, the top bar's time was the time the client started, for as long
// as the session lasted. It is the same class of defect as the transport
// placeholder this branch exists to remove: a cell that looks like live
// status and is not.
func TestTheClockAdvances(t *testing.T) {
	m := framedModel(t, 120, 30)
	m.topBar.SetClock("00:00")

	if row := topBarRow(t, m); !strings.Contains(row, "00:00") {
		t.Fatalf("precondition: the stale clock is not on screen:\n%s", row)
	}

	updated, cmd := m.Update(chromeTickMsg(time.Now()))
	if row := topBarRow(t, updated.(Model)); strings.Contains(row, "00:00") {
		t.Errorf("a tick did not refresh the clock:\n%s", row)
	}
	if cmd == nil {
		t.Error("the tick did not schedule the next one; the clock stops after one")
	}
}

// And the pulse has to start on its own. Init returned nil, which is why
// there was no tick to begin with.
func TestInitStartsTheTick(t *testing.T) {
	m := framedModel(t, 120, 30)
	if m.Init() == nil {
		t.Error("Init starts no chrome tick")
	}
}

// A notice owns the hint bar for four seconds and then gives it back. It
// never did: hintbar.ClearNotice had no caller anywhere in the program, so a
// notice stayed until something else replaced it.
func TestANoticeGivesTheRowBack(t *testing.T) {
	m := framedModel(t, 120, 30)

	// Deadlines from the wall clock, NOT from m.noticeAt. Deriving them
	// from the field under test lets a notify that never records a time
	// move the goalposts with it — which is exactly how this test passed
	// against a notify that had stopped stamping anything.
	raised := time.Now()
	m.notify("copied 42 characters")
	m.refreshChrome()

	if row := hintBarRow(t, m); !strings.Contains(row, "copied 42 characters") {
		t.Fatalf("the notice is not on the hint bar:\n%s", row)
	}

	// Not yet.
	updated, _ := m.Update(chromeTickMsg(raised.Add(noticeLife - time.Second)))
	if row := hintBarRow(t, updated.(Model)); !strings.Contains(row, "copied 42") {
		t.Errorf("the notice went early:\n%s", row)
	}

	// Now.
	updated, _ = m.Update(chromeTickMsg(raised.Add(noticeLife + time.Second)))
	got := updated.(Model)
	if row := hintBarRow(t, got); strings.Contains(row, "copied 42") {
		t.Errorf("the notice outlived its four seconds:\n%s", row)
	}
	// And the row goes back to being the hint bar, not blank.
	if row := hintBarRow(t, got); !strings.Contains(row, "quit") {
		t.Errorf("the hints did not come back:\n%s", row)
	}
}

// A tick with no notice raised must not clear one that arrives later — the
// expiry is measured from when the notice was raised, not from startup.
func TestATickWithNoNoticeIsHarmless(t *testing.T) {
	m := framedModel(t, 120, 30)

	updated, _ := m.Update(chromeTickMsg(time.Now()))
	m = updated.(Model)

	raised := time.Now()
	m.notify("copied 42 characters")
	m.refreshChrome()
	updated, _ = m.Update(chromeTickMsg(raised.Add(time.Second)))
	if row := hintBarRow(t, updated.(Model)); !strings.Contains(row, "copied 42") {
		t.Errorf("a notice raised after an idle tick was cleared at once:\n%s", row)
	}
}
