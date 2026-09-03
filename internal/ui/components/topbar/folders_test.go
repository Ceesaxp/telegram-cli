package topbar

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// emojiFolders is a real folder list: Telegram folder names are very often a
// single colour emoji, and several of them are composed sequences whose
// rendered width a terminal decides for itself.
var emojiFolders = []Folder{
	{Name: "💬 All", Active: true},
	{Name: "❤️"},  // base + U+FE0F
	{Name: "🔤"},   // a plain wide glyph
	{Name: "⁉️👥"}, // presentation selector plus a second emoji
	{Name: "🇷🇸"},  // a regional-indicator pair
	{Name: "Serbia/BG"},
}

func emojiBar(w int) Model {
	m := New(theme.DarkRoles(false))
	m.SetWidth(w)
	m.SetClock("12:40")
	m.SetConnection(Connected, "connected")
	m.SetDevices(10)
	m.SetFolders(emojiFolders)
	return m
}

// The active folder was marked by a brighter foreground and nothing else.
// A colour emoji ignores the foreground it is given, so on a folder list made
// of pictures — which is what Telegram folder names usually are — nothing on
// the row said which folder was open.
func TestTheActiveFolderIsMarkedByABackground(t *testing.T) {
	r := theme.DarkRoles(false)
	view := emojiBar(200).View()

	if !strings.Contains(view, "48;5;"+string(r.Sel)) {
		t.Errorf("the active tab has no background:\n%s",
			strings.ReplaceAll(view, "\x1b", "ESC"))
	}

	// Exactly one tab wears it. Two would say two folders are open.
	if got := strings.Count(view, "48;5;"+string(r.Sel)); got != 1 {
		t.Errorf("%d tabs carry the active background, want 1", got)
	}
}

// The reservation covers BOTH ways the label might be drawn.
//
// A label whose only risky sequence is a presentation selector can only come
// out narrower than the tables say, and narrower costs a gap — so it needs
// no extra room. A joined or paired sequence can come out wider, and wider
// overwrites the connection group beside it, so the reservation has to be
// the decomposed width rather than the tables'.
//
// Stated as "reserved > measured wherever the sequence is risky", this test
// passed while the reservation was a cell per composition rune — which is
// four for a three-person family that a terminal drawing the parts puts in
// six.
func TestTheReservationCoversBothRenderings(t *testing.T) {
	labels := []string{
		"1:work",
		"3:🔤",
		"2:❤️",
		"5:🇷🇸",
		"6:👨‍👩‍👧",
	}

	for _, label := range labels {
		t.Run(label, func(t *testing.T) {
			reserved := reservedWidth(label)

			tables := cell.Width(label)
			cell.SetEmojiMode(cell.EmojiSeparate)
			separate := cell.Width(label)
			cell.SetEmojiMode(cell.EmojiAuto)

			if reserved < tables {
				t.Errorf("reserved %d, but the tables measure %d", reserved, tables)
			}
			if reserved < separate {
				t.Errorf("reserved %d, but a terminal that composes nothing "+
					"draws %d", reserved, separate)
			}
			// And no more than it has to be: an over-reservation is a tab
			// dropped early, which is a cost even when it is the safe one.
			if want := max(tables, separate); reserved != want {
				t.Errorf("reserved %d, want %d", reserved, want)
			}
		})
	}
}

// A declared mode replaces the hedge outright: the user has said which way
// their terminal draws, so the tabs are laid out at exactly that width and
// the row ends flush against the clock.
func TestADeclaredModeDropsTheHedge(t *testing.T) {
	t.Cleanup(func() { cell.SetEmojiMode(cell.EmojiAuto) })

	for _, mode := range []cell.EmojiMode{cell.EmojiComposed, cell.EmojiSeparate} {
		cell.SetEmojiMode(mode)
		for _, label := range []string{"2:❤️", "5:🇷🇸", "6:👨‍👩‍👧"} {
			if got, want := reservedWidth(label), cell.Width(label); got != want {
				t.Errorf("mode %v: reserved %d for %q, want its measured %d",
					mode, got, label, want)
			}
		}
	}
}

// The failure this prevents, stated as the invariant: the tabs never eat into
// the connection group beside them. Under-reserving is what put "nnected" on
// the top bar of a terminal that drew a flag wider than the tables said.
func TestTheRightGroupSurvivesEmojiFolders(t *testing.T) {
	for w := 40; w <= 220; w++ {
		view := ansi.Strip(emojiBar(w).View())

		if got := cell.Width(view); got != w {
			t.Fatalf("width %d: the row is %d cells", w, got)
		}
		// The clock is the one thing that can never go.
		if !strings.Contains(view, "12:40") {
			t.Fatalf("width %d dropped the clock: %q", w, view)
		}
		// And whatever of the status group survived must be whole: a
		// truncated "connected" means something overwrote it.
		if strings.Contains(view, "nnected") && !strings.Contains(view, "connected") {
			t.Fatalf("width %d: the tabs overwrote the status: %q", w, view)
		}
		if strings.Contains(view, "device") && !strings.Contains(view, "10 devices") {
			t.Fatalf("width %d: the device cell is cut: %q", w, view)
		}
	}
}

// A click has to land on the folder the user aimed at, so the hit-test must
// agree with where the tabs were DRAWN — which is their measured width, not
// the room reserved for them.
//
// Checked against the rendered row rather than against tabSpans itself: a
// test that asks TabAt about columns tabSpans produced is asking one function
// whether it agrees with itself, and passes just as well when both are wrong
// together.
func TestTabSpansMatchTheDrawnTabsWithEmoji(t *testing.T) {
	m := emojiBar(200)
	view := ansi.Strip(m.View())

	// Walk the row and find each label where it actually landed.
	at := 0
	for i, f := range emojiFolders {
		label := itoa(i+1) + ":" + f.Name
		idx := strings.Index(view[at:], label)
		if idx < 0 {
			break // the rest did not fit
		}
		column := cell.Width(view[:at+idx])
		if got := m.TabAt(column); got != i {
			t.Errorf("folder %d is drawn at column %d, but a click there hits %d",
				i, column, got)
		}
		at += idx + len(label)
	}

	// Between the mark and the first tab is nobody's.
	if got := m.TabAt(0); got != -1 {
		t.Errorf("column 0 hit folder %d, want none", got)
	}
}

// The right group is right-aligned, so the row must END with the clock. The
// gap between the tabs and it is what makes that true, and it has to be
// measured from what the tabs drew rather than from what they reserved — an
// over-reservation would otherwise leave the clock short of the edge with
// padding behind it.
func TestTheClockSitsAtTheRightEdge(t *testing.T) {
	for _, w := range []int{60, 90, 120, 200} {
		view := ansi.Strip(emojiBar(w).View())
		if got := strings.TrimRight(view, " "); !strings.HasSuffix(got, "12:40") {
			t.Errorf("width %d: the row does not end with the clock: %q", w, view)
		}
		// One trailing cell, which is the row's own margin.
		if trailing := len(view) - len(strings.TrimRight(view, " ")); trailing != 1 {
			t.Errorf("width %d: %d trailing spaces after the clock, want 1", w, trailing)
		}
	}
}
