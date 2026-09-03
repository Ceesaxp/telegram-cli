package topbar

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/charmbracelet/x/ansi"
)

// drawnWidth is the row's width as a terminal in the given mode draws it,
// which is not the same number as the width the row was laid out to.
func drawnWidth(t *testing.T, view string, mode cell.EmojiMode) int {
	t.Helper()
	prev := cell.CurrentEmojiMode()
	cell.SetEmojiMode(mode)
	defer cell.SetEmojiMode(prev)
	return cell.Width(ansi.Strip(view))
}

// The gap after the clock, and the setting that closes it.
//
// The clock is right-aligned by arithmetic: the gap before it is the width
// minus what everything else measured. On a terminal that draws "❤️" in one
// cell where the tables say two, every such folder makes the row come out a
// cell short of the right edge — the reported three-character gap. Nothing
// is corrupted; the row simply stops early.
//
// ui.emoji_width is the declaration that fixes it, and it has to be a
// declaration: the width cannot be measured from here, and the runtime query
// that would ask was removed in wave 5 for leaking its response into the
// composer.
func TestADeclaredModeEndsTheRowAtTheRightEdge(t *testing.T) {
	const w = 100

	// Three folders whose names are a NARROW character plus U+FE0F, which
	// is the class that produces the gap. The mixed set the other tests use
	// cancels out — its two selectors lose a cell each and its flag gains
	// two — which is a good reason not to reuse it here. "⭐️" would not
	// work either: U+2B50 is East Asian Wide on its own, so the selector
	// changes nothing and a terminal ignoring it still draws two.
	bar := func() Model {
		m := emojiBar(w)
		m.SetFolders([]Folder{
			{Name: "All", Active: true},
			{Name: "❤️"}, // U+2764, one cell without the selector
			{Name: "⁉️"}, // U+2049
			{Name: "‼️"}, // U+203C
		})
		return m
	}

	// The bug, stated: laid out by the tables, drawn by a terminal that
	// composes nothing, the row ends short.
	cell.SetEmojiMode(cell.EmojiAuto)
	short := drawnWidth(t, bar().View(), cell.EmojiSeparate)
	if short != w-3 {
		t.Fatalf("three selector folders draw %d cells in a width of %d, "+
			"want %d — the arithmetic of the gap has changed", short, w, w-3)
	}

	// The fix: laid out by the same declaration the terminal draws by, the
	// row ends exactly at the edge.
	t.Cleanup(func() { cell.SetEmojiMode(cell.EmojiAuto) })
	cell.SetEmojiMode(cell.EmojiSeparate)
	if got := drawnWidth(t, bar().View(), cell.EmojiSeparate); got != w {
		t.Errorf("declared separate, the row draws %d cells in a width of %d "+
			"(it drew %d when laid out by the tables)", got, w, short)
	}
}

// And the declaration must not cost the row its other invariants: whatever
// the mode, the tabs never run into the connection group beside them.
func TestNoModeLetsTheTabsEatTheRightGroup(t *testing.T) {
	t.Cleanup(func() { cell.SetEmojiMode(cell.EmojiAuto) })

	for _, mode := range []cell.EmojiMode{
		cell.EmojiAuto, cell.EmojiComposed, cell.EmojiSeparate,
	} {
		cell.SetEmojiMode(mode)
		for w := 40; w <= 220; w++ {
			view := ansi.Strip(emojiBar(w).View())
			if got := cell.Width(view); got != w {
				t.Fatalf("mode %v at width %d: the row is %d cells", mode, w, got)
			}
			if !strings.Contains(view, "12:40") {
				t.Fatalf("mode %v at width %d: the clock was dropped:\n%s",
					mode, w, view)
			}
		}
	}
}
