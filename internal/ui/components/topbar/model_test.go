package topbar

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
)

func newBar(width int) Model {
	m := New(theme.DarkRoles(false))
	m.SetWidth(width)
	m.SetFolders([]Folder{
		{Name: "all", Active: true},
		{Name: "unread"},
		{Name: "work"},
		{Name: "channels"},
		{Name: "archive"},
	})
	m.SetConnection(Connected, "connected")
	m.SetClock("21:04")
	m.SetDevices(3)
	return m
}

func TestViewIsExactlyWide(t *testing.T) {
	for w := 1; w <= 250; w++ {
		if got := cell.Width(newBar(w).View()); got != w {
			t.Fatalf("width %d: View() is %d cells", w, got)
		}
	}
}

// TestFolderTabsAreNumbered: the digit that selects a tab is shown rather
// than remembered, which is the whole reason the tabs are labelled this way.
func TestFolderTabsAreNumbered(t *testing.T) {
	view := plain(newBar(120).View())
	for _, want := range []string{"1:all", "2:unread", "3:work", "4:channels", "5:archive"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not contain the numbered tab %q: %q", want, view)
		}
	}
}

// TestRightGroupDegradesInOrder is the shrink order that is left now that
// the transport cell is gone: the device count drops, then the status
// description, and the clock never does.
//
// The widths bracket the transition rather than being magic numbers — what
// matters is the ORDER, so the test asserts what survives each step as well
// as what has gone.
func TestRightGroupDegradesInOrder(t *testing.T) {
	full := plain(newBar(140).View())
	if !strings.Contains(full, "3 devices") || !strings.Contains(full, "connected") {
		t.Fatalf("the widest form is missing a piece: %q", full)
	}

	var lostDevices, lostStatus int
	for w := 140; w >= 12; w-- {
		v := plain(newBar(w).View())
		if lostDevices == 0 && !strings.Contains(v, "devices") {
			lostDevices = w
		}
		if lostStatus == 0 && !strings.Contains(v, "connected") {
			lostStatus = w
		}
	}

	if lostDevices == 0 || lostStatus == 0 {
		t.Fatalf("pieces never dropped (devices at %d, status at %d)",
			lostDevices, lostStatus)
	}
	if lostDevices <= lostStatus {
		t.Errorf("the status went at width %d but devices only at %d; "+
			"devices must go first", lostStatus, lostDevices)
	}
}

// An unanswered device count draws nothing. Every account has at least the
// session doing the asking, so a zero is always "not asked yet" and never a
// fact about the account — and "devices 0" would state it as one.
func TestAnUnknownDeviceCountDrawsNothing(t *testing.T) {
	m := newBar(140)
	m.SetDevices(0)

	v := plain(m.View())
	if strings.Contains(v, "device") {
		t.Errorf("an unknown device count was drawn: %q", v)
	}
	if !strings.Contains(v, "connected") {
		t.Errorf("the status went with it: %q", v)
	}
}

// One device is "1 device". "devices 1" reads as placeholder text even when
// it is true, which is what it was for four phases.
func TestOneDeviceIsSingular(t *testing.T) {
	m := newBar(140)
	m.SetDevices(1)
	if v := plain(m.View()); !strings.Contains(v, "1 device ") {
		t.Errorf("want the singular form: %q", v)
	}

	m.SetDevices(2)
	if v := plain(m.View()); !strings.Contains(v, "2 devices") {
		t.Errorf("want the plural form: %q", v)
	}
}

// TestClockIsNeverDropped is the one hard guarantee on this row: whatever
// else is cut, the time survives.
func TestClockIsNeverDropped(t *testing.T) {
	for w := 12; w <= 200; w++ {
		if v := plain(newBar(w).View()); !strings.Contains(v, "21:04") {
			t.Errorf("width %d dropped the clock: %q", w, v)
		}
	}
}

// TestConnectionDotFollowsState: the dot and its wording are set together,
// so they cannot disagree about whether the client is connected.
func TestConnectionDotFollowsState(t *testing.T) {
	for _, tc := range []struct {
		state ConnState
		text  string
	}{
		{Connected, "connected"},
		{Connecting, "connecting"},
		{Disconnected, "disconnected"},
	} {
		m := newBar(140)
		m.SetConnection(tc.state, tc.text)
		if v := plain(m.View()); !strings.Contains(v, tc.text) {
			t.Errorf("state %v: view lacks %q", tc.state, tc.text)
		}
	}
}

// TestWideRuneFolderNames: folder titles carry emoji in real accounts, and
// a rune-counted tab budget would draw twice its cells.
func TestWideRuneFolderNames(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetWidth(100)
	m.SetFolders([]Folder{
		{Name: "四字熟語", Active: true},
		{Name: "📢📢📢"},
		{Name: strings.Repeat("四", 30)},
	})
	m.SetClock("21:04")

	if got := cell.Width(m.View()); got != 100 {
		t.Errorf("wide-rune folder names rendered %d cells, want 100", got)
	}
}

func TestZeroWidthIsEmpty(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetWidth(0)
	if got := m.View(); got != "" {
		t.Errorf("View() = %q at zero width, want empty", got)
	}
}

// labelColumn finds a label's starting COLUMN in a rendered row.
//
// strings.Index gives a byte offset, which is not the same thing: the row
// contains multi-byte box-drawing characters, so a byte index runs ahead of
// the column by two for every "│" already passed. Every glyph on this row is
// one cell wide, so the rune index is the column.
func labelColumn(view, label string) int {
	runes := []rune(view)
	want := []rune(label)
	for i := 0; i+len(want) <= len(runes); i++ {
		if string(runes[i:i+len(want)]) == label {
			return i
		}
	}
	return -1
}

func plain(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && (r == 'm' || r == 'K'):
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestTabAtHitsTheDrawnSpans is the folder-tab hit-test that moved here from
// chatlist when the tabs moved to this bar. Every drawn label must be
// clickable at every column it occupies, and nothing else may be.
func TestTabAtHitsTheDrawnSpans(t *testing.T) {
	m := newBar(120)
	view := plain(m.View())

	names := []string{"1:all", "2:unread", "3:work", "4:channels", "5:archive"}
	for want, label := range names {
		start := labelColumn(view, label)
		if start < 0 {
			t.Fatalf("tab %q was not drawn: %q", label, view)
		}
		for x := start; x < start+len([]rune(label)); x++ {
			if got := m.TabAt(x); got != want {
				t.Errorf("TabAt(%d) = %d, want %d (inside %q)", x, got, want, label)
			}
		}
	}
}

// TestTabAtMissesTheGaps: the space between two tabs belongs to neither, so
// a click that lands between labels does nothing rather than picking one.
func TestTabAtMissesTheGaps(t *testing.T) {
	m := newBar(120)
	view := plain(m.View())

	for _, label := range []string{"2:unread", "3:work"} {
		start := labelColumn(view, label)
		if got := m.TabAt(start - 1); got != -1 {
			t.Errorf("TabAt(%d) = %d on the gap before %q, want -1", start-1, got, label)
		}
	}

	if got := m.TabAt(0); got != -1 {
		t.Errorf("TabAt(0) = %d on the app mark, want -1", got)
	}
	if got := m.TabAt(119); got != -1 {
		t.Errorf("TabAt(119) = %d on the clock, want -1", got)
	}
}

// TestTabAtIgnoresTabsThatWereNotDrawn: at a narrow width some tabs are
// dropped, and a click must never be attributed to one the user cannot see.
func TestTabAtIgnoresTabsThatWereNotDrawn(t *testing.T) {
	m := newBar(40)
	view := plain(m.View())

	for i, label := range []string{"1:all", "2:unread", "3:work", "4:channels", "5:archive"} {
		drawn := strings.Contains(view, label)
		hit := false
		for x := range 40 {
			if m.TabAt(x) == i {
				hit = true
				break
			}
		}
		if hit != drawn {
			t.Errorf("tab %q drawn=%v but clickable=%v", label, drawn, hit)
		}
	}
}
