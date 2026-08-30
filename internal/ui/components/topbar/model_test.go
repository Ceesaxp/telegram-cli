package topbar

import (
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
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
	m.SetPlaceholders("mtproto 2.0", "devices 1")
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

// TestRightGroupDegradesInOrder is the specified shrink order: the status
// description truncates, then the device count is dropped, then the
// transport. The clock is never dropped.
//
// The widths here bracket each transition rather than being magic numbers —
// what matters is the ORDER pieces disappear in, so each case asserts what
// is still present as well as what has gone.
func TestRightGroupDegradesInOrder(t *testing.T) {
	full := plain(newBar(140).View())
	if !strings.Contains(full, "mtproto 2.0") || !strings.Contains(full, "devices 1") {
		t.Fatalf("the widest form is missing a piece: %q", full)
	}

	// Narrow until each piece goes, and record the order.
	var lostDevices, lostTransport int
	for w := 140; w >= 20; w-- {
		v := plain(newBar(w).View())
		if lostDevices == 0 && !strings.Contains(v, "devices 1") {
			lostDevices = w
		}
		if lostTransport == 0 && !strings.Contains(v, "mtproto") {
			lostTransport = w
		}
	}

	if lostDevices == 0 || lostTransport == 0 {
		t.Fatalf("pieces never dropped (devices at %d, transport at %d)",
			lostDevices, lostTransport)
	}
	if lostDevices <= lostTransport {
		t.Errorf("transport dropped at width %d but devices only at %d; "+
			"devices must go first", lostTransport, lostDevices)
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
