package chatview

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// barRows counts how many of a message's rows carry the cursor bar, and
// returns the rendered lines so a caller can look at their colour.
func barRows(lines []string) int {
	n := 0
	for _, line := range lines {
		runes := []rune(ansi.Strip(line))
		if len(runes) > 1 && runes[1] == '▌' {
			n++
		}
	}
	return n
}

// A mark on the first line marks the first line. The design's grid table
// gives column 1 to "the cursor bar on the selected message", with no
// first-row exception — a five-line message with one marked line does not
// read as five selected lines.
func TestTheCursorBarRunsDownTheWholeMessage(t *testing.T) {
	const width = 60
	m := gridModel(t, width)
	m.focused = true
	msgs := m.store.Messages.Get(testChatID)

	// A message long enough to wrap several times.
	msgs[1].Content = text(strings.Repeat("wrapping words ", 12))
	m.cache.clear()

	lines := m.gridMessageLines(msgs[1], msgs[0], true)
	rows := len(lines)
	if rows < 4 {
		t.Fatalf("want a multi-row message, got %d rows", rows)
	}

	// Every row but the day divider above it.
	want := rows - dividerRows(lines)
	if got := barRows(lines); got != want {
		t.Errorf("the bar covers %d of %d message rows", got, want)
	}
}

// A divider is the boundary above a message, not the message. Marking it
// would say the cursor is sitting on a row it cannot be on.
func TestTheCursorBarStopsAtTheDivider(t *testing.T) {
	m := gridModel(t, 60)
	m.focused = true
	msgs := m.store.Messages.Get(testChatID)

	lines := m.gridMessageLines(msgs[0], nil, true)
	if len(lines) < 2 {
		t.Fatalf("want a divider and a message row, got %d lines", len(lines))
	}
	if runes := []rune(ansi.Strip(lines[0])); len(runes) > 1 && runes[1] == '▌' {
		t.Errorf("the day divider carries the cursor bar: %q", ansi.Strip(lines[0]))
	}
}

// The bar's colour is the only thing on screen that says which panel has the
// keyboard. Two panels drawing an equally bright cursor is two panels
// claiming to be active.
func TestTheCursorBarDimsWhenTheThreadIsNotFocused(t *testing.T) {
	r := theme.DarkRoles(false)
	m := gridModel(t, 60)
	msgs := m.store.Messages.Get(testChatID)

	m.focused = true
	focused := strings.Join(m.gridMessageLines(msgs[1], msgs[0], true), "\n")
	m.focused = false
	blurred := strings.Join(m.gridMessageLines(msgs[1], msgs[0], true), "\n")

	if !strings.Contains(focused, "38;5;"+string(r.Cyan)) {
		t.Error("the focused thread's cursor bar is not cyan")
	}
	if strings.Contains(blurred, "38;5;"+string(r.Cyan)) {
		t.Error("the unfocused thread still draws a cyan cursor bar")
	}
	if !strings.Contains(blurred, "38;5;"+string(r.Ghost)) {
		t.Error("the unfocused thread's cursor bar is not ghost")
	}
	// It is still drawn — the cursor has not moved, only the panel's claim
	// on the keyboard has.
	if got := barRows(strings.Split(blurred, "\n")); got == 0 {
		t.Error("the unfocused thread stopped drawing the cursor entirely")
	}
}

// dividerRows counts the leading rows that are dividers rather than message
// rows: a divider fills the whole width with a rule, so its second cell is
// never blank and never the bar.
func dividerRows(lines []string) int {
	n := 0
	for _, line := range lines {
		runes := []rune(ansi.Strip(line))
		if len(runes) > 1 && runes[1] != '▌' && runes[1] != ' ' {
			n++
			continue
		}
		break
	}
	return n
}
