package chatlist

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
	"github.com/imtaqin/telegram-cli/internal/ui/widgets"
)

func selectableItem() widgets.ListItem {
	return widgets.ListItem{
		Title:    "infra-oncall",
		Subtitle: "nadia: rebased, CI green",
		Meta:     "2m",
		Badge:    "4",
		Kind:     int(telegram.ChatTypeSupergroup),
	}
}

// The selection is a full-width band on both rows of the chat, not a mark in
// the gutter. It has to survive the sigil, the title, the time and the badge,
// each of which closes its own styled span.
func TestTheSelectedRowIsBandedEdgeToEdge(t *testing.T) {
	const width = 38

	for _, line := range rowModel().renderRow(selectableItem(), true, true, width) {
		if w := cell.Width(line); w != width {
			t.Fatalf("row is %d cells, want %d", w, width)
		}
		if p := cell.PaintedWidth(line); p != width {
			t.Errorf("sel covers %d of %d cells, dying at column %d\n%s",
				p, width, p, strings.ReplaceAll(line, "\x1b", "ESC"))
		}
	}
}

// The badge is drawn on cyan and stays there. A band that repainted over it
// would hide the one thing on the row that is asking for attention.
func TestTheBadgeKeepsItsOwnColourInsideTheBand(t *testing.T) {
	r := theme.DarkRoles(false)
	preview := rowModel().renderRow(selectableItem(), true, true, 38)[1]

	if !strings.Contains(preview, "48;5;"+string(r.Cyan)) {
		t.Errorf("the badge lost its cyan inside the selection band:\n%s",
			strings.ReplaceAll(preview, "\x1b", "ESC"))
	}
}

// An unselected row carries no background of its own. Panel is the column's
// surface and the frame fills it, including the rows below the last chat —
// painting it here as well would be a second mechanism for one rule, and the
// one that cannot cover the padding.
func TestAnUnselectedRowLeavesTheSurfaceToTheFrame(t *testing.T) {
	for _, line := range rowModel().renderRow(selectableItem(), false, false, 38) {
		if p := cell.PaintedWidth(line); p != 0 {
			t.Errorf("an unselected row painted %d cells:\n%s",
				p, strings.ReplaceAll(line, "\x1b", "ESC"))
		}
	}
	chrome := newTestModel()
	for name, line := range map[string]string{
		"filter header": chrome.renderFilterHeader(38),
		"list footer":   chrome.renderListFooter(38),
	} {
		if p := cell.PaintedWidth(line); p != 0 {
			t.Errorf("the %s painted %d cells:\n%s",
				name, p, strings.ReplaceAll(line, "\x1b", "ESC"))
		}
	}
}

// The bar runs down both rows of a selected chat. A mark on the title line
// only marks the title, and the preview underneath reads as belonging to no
// row in particular.
func TestTheSelectionBarRunsDownBothRows(t *testing.T) {
	for i, line := range rowModel().renderRow(selectableItem(), true, true, 38) {
		if runes := []rune(ansi.Strip(line)); len(runes) == 0 || runes[0] != '▌' {
			t.Errorf("row %d does not start with the selection bar: %q",
				i, ansi.Strip(line))
		}
	}
}

// The bar's colour is the only thing on screen that says which panel has the
// keyboard, so an unfocused list still shows WHERE the cursor is without
// claiming to be where the keys go.
func TestTheSelectionBarDimsWhenTheListIsNotFocused(t *testing.T) {
	r := theme.DarkRoles(false)

	focused := strings.Join(rowModel().renderRow(selectableItem(), true, true, 38), "\n")
	blurred := strings.Join(rowModel().renderRow(selectableItem(), true, false, 38), "\n")

	if !strings.Contains(focused, "38;5;"+string(r.Cyan)) {
		t.Error("the focused list's selection bar is not cyan")
	}
	if strings.Contains(blurred, "38;5;"+string(r.Cyan)) {
		t.Error("the unfocused list still draws a cyan selection bar")
	}
	for i, line := range rowModel().renderRow(selectableItem(), true, false, 38) {
		if runes := []rune(ansi.Strip(line)); len(runes) == 0 || runes[0] != '▌' {
			t.Errorf("row %d of an unfocused selection lost its bar", i)
		}
	}
}

// The row renderer is installed in New, not by a setter the caller has to
// remember. A chat list that fell back to the widget's own row drawing would
// show the pre-2.0 layout — a coloured initials block where the type sigil
// goes — and every assertion in this file would still pass, because they all
// call renderRow directly.
func TestTheListDrawsTUI2Rows(t *testing.T) {
	m := newTestModel()
	m.SetSize(38, 10)
	m.loading = false
	*m.dirty = true

	m.store.Chats.Set(&telegram.Chat{
		ID: 1, Type: telegram.ChatTypeSupergroup, Title: "infra-oncall",
	})
	m.refreshList()

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "# infra-oncall") {
		t.Errorf("the chat list is not drawing TUI 2.0 rows (no type sigil):\n%s", view)
	}
}
