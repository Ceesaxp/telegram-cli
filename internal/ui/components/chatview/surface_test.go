package chatview

import (
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/ui/cell"
)

func esc(s string) string { return strings.ReplaceAll(s, "\x1b", "ESC") }

// The selected message is a band across the full pane. Every row of it opens
// styled spans for the time, the sender and the body, each closing with a
// reset — under the wrapping spelling the band was a single cyan cell in the
// gutter, which is the "just a mark" the band exists not to be.
func TestTheSelectedMessageIsBandedEdgeToEdge(t *testing.T) {
	const width = 100
	m := gridModel(t, width)
	msgs := m.store.Messages.Get(testChatID)

	lines := m.gridMessageLines(msgs[3], msgs[2], true)
	if len(lines) == 0 {
		t.Fatal("no lines rendered")
	}
	for i, line := range lines {
		if w := cell.Width(line); w != width {
			t.Fatalf("line %d is %d cells, want %d", i, w, width)
		}
		if p := cell.PaintedWidth(line); p != width {
			t.Errorf("line %d: curline covers %d of %d cells, dying at column %d\n%s",
				i, p, width, p, esc(line))
		}
	}
}

// A divider belongs to the boundary above a message, not to the message. The
// band stops at it: a day divider lit as part of the selection says the
// cursor is sitting on the divider, which is not somewhere it can be.
func TestADividerAboveTheSelectionIsNotBanded(t *testing.T) {
	const width = 100
	m := gridModel(t, width)
	msgs := m.store.Messages.Get(testChatID)

	// The oldest message always carries a day divider above it.
	lines := m.gridMessageLines(msgs[0], nil, true)
	if len(lines) < 2 {
		t.Fatalf("want a divider and at least one body row, got %d lines", len(lines))
	}
	if p := cell.PaintedWidth(lines[0]); p != 0 {
		t.Errorf("the day divider is banded for %d cells:\n%s", p, esc(lines[0]))
	}
	if p := cell.PaintedWidth(lines[1]); p != width {
		t.Errorf("the message below the divider is banded for %d of %d cells:\n%s",
			p, width, esc(lines[1]))
	}
}

// An unselected message carries no background. bg is the thread column's
// surface and the frame fills it, so a message that painted itself would be
// painting the same colour twice — and would still leave the gaps between
// messages to the frame anyway.
func TestAnUnselectedMessageLeavesTheSurfaceToTheFrame(t *testing.T) {
	m := gridModel(t, 100)
	msgs := m.store.Messages.Get(testChatID)

	for i, line := range m.gridMessageLines(msgs[3], msgs[2], false) {
		if p := cell.PaintedWidth(line); p != 0 {
			t.Errorf("line %d of an unselected message painted %d cells:\n%s",
				i, p, esc(line))
		}
	}
}

// The header is panel and the column around it is bg, so this one is a real
// exception rather than a repeat of the surface.
func TestTheThreadHeaderIsPaintedEdgeToEdge(t *testing.T) {
	const width = 100
	m := gridModel(t, width)

	line := m.renderHeader()
	if w := cell.Width(line); w != width {
		t.Fatalf("header is %d cells, want %d", w, width)
	}
	if p := cell.PaintedWidth(line); p != width {
		t.Errorf("panel covers %d of %d cells, dying at column %d\n%s",
			p, width, p, esc(line))
	}
}
