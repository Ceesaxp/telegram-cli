package app

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/golden"
)

func topBarRow(t *testing.T, m Model) string {
	t.Helper()
	return golden.SplitLines(m.View().Content)[0]
}

// The top bar says nothing about devices until the answer arrives. Every
// account has at least the session doing the asking, so a zero is always
// "not asked yet" and never a fact — and drawing it as one is the whole
// reason decision 7 called the placeholder a release blocker.
func TestTheDeviceCellIsAbsentUntilItIsKnown(t *testing.T) {
	m := framedModel(t, 200, 40)

	if row := topBarRow(t, m); strings.Contains(row, "device") {
		t.Errorf("the top bar claims a device count before asking:\n%s", row)
	}

	updated, _ := m.Update(deviceCountMsg(3))
	if row := topBarRow(t, updated.(Model)); !strings.Contains(row, "3 devices") {
		t.Errorf("the answer did not reach the top bar:\n%s", row)
	}
}

// A failure reports zero, and zero draws nothing. Nobody asked for this
// number, so failing to get it is not an event in the user's day — but it
// must not become a wrong number either, and "0 devices" would be one:
// every account has at least the session doing the asking.
func TestAFailedDeviceLookupSaysNothing(t *testing.T) {
	m := framedModel(t, 200, 40)
	updated, cmd := m.Update(deviceCountMsg(0))
	got := updated.(Model)

	if row := topBarRow(t, got); strings.Contains(row, "device") {
		t.Errorf("a failed lookup drew a device count:\n%s", row)
	}
	if cmd != nil {
		t.Error("a failed lookup produced a command; it should be silent")
	}
}

// A later answer replaces an earlier one: revoking a session elsewhere and
// reconnecting has to be able to move the number down.
func TestALaterAnswerReplacesTheEarlierOne(t *testing.T) {
	m := framedModel(t, 200, 40)
	updated, _ := m.Update(deviceCountMsg(4))
	updated, _ = updated.(Model).Update(deviceCountMsg(2))

	row := topBarRow(t, updated.(Model))
	if !strings.Contains(row, "2 devices") {
		t.Errorf("the top bar kept the stale count:\n%s", row)
	}
	if strings.Contains(row, "4 devices") {
		t.Errorf("the top bar shows both counts:\n%s", row)
	}
}

// The transport cell is gone, not hidden. It could only ever have read
// "mtproto 2.0" — gotd speaks that and nothing else — and a constant in a
// status area is decoration wearing the clothes of information.
func TestTheTransportCellIsGone(t *testing.T) {
	m := framedModel(t, 200, 40)
	updated, _ := m.Update(deviceCountMsg(3))

	if row := topBarRow(t, updated.(Model)); strings.Contains(row, "mtproto") {
		t.Errorf("the transport placeholder is still drawn:\n%s", row)
	}
}
