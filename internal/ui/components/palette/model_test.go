package palette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	uv "github.com/charmbracelet/ultraviolet"
)

func testItems() []Item {
	return []Item{
		{Name: "keymap", Description: "show the cheat sheet", Key: "?"},
		{Name: "mark-read", Description: "mark this chat read"},
		{Name: "quit", Description: "quit tele-tui", Key: "q"},
		{Name: "search", Args: "<query>", Description: "search all chats", Key: "ctrl+g"},
	}
}

func openPalette(t *testing.T) Model {
	t.Helper()
	m := New(theme.DarkRoles(false))
	m.SetItems(testItems())
	m.Open()
	return m
}

// decodeKey builds a keypress from the bytes a terminal actually sends,
// matching internal/app's testing discipline: a hand-built Key can assert a
// spelling the decoder never produces.
func decodeKey(t *testing.T, seq string) tea.KeyPressMsg {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(seq))
	if n != len(seq) {
		t.Fatalf("decode(%q) consumed %d of %d bytes", seq, n, len(seq))
	}
	kp, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("decode(%q) produced %T, want uv.KeyPressEvent", seq, ev)
	}
	return tea.KeyPressMsg(kp)
}

func typeString(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = m.Update(decodeKey(t, string(r)))
	}
	return m
}

func names(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

// --- Filtering ------------------------------------------------------------

func TestEmptyQueryListsEverything(t *testing.T) {
	m := openPalette(t)
	if got := len(m.Matches()); got != len(testItems()) {
		t.Errorf("empty query matched %d items, want all %d", got, len(testItems()))
	}
}

func TestPrefixFiltering(t *testing.T) {
	m := typeString(t, openPalette(t), "qu")
	if got := names(m.Matches()); len(got) != 1 || got[0] != "quit" {
		t.Errorf("query %q matched %v, want [quit]", "qu", got)
	}
}

// TestFuzzyMatchingFindsSubsequences is the "fuzzy" half of the spec's
// "fuzzy prefix matching": the letters have to appear in order, but not
// adjacently.
func TestFuzzyMatchingFindsSubsequences(t *testing.T) {
	m := typeString(t, openPalette(t), "mkrd")
	got := names(m.Matches())
	if len(got) != 1 || got[0] != "mark-read" {
		t.Errorf("query %q matched %v, want [mark-read]", "mkrd", got)
	}
}

// TestPrefixMatchesSortAboveFuzzyOnes matters because Enter runs whatever is
// highlighted: an exactly-typed command must be the one at the top, not
// buried under a looser match that happens to sort earlier.
func TestPrefixMatchesSortAboveFuzzyOnes(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetItems([]Item{
		{Name: "some-quit-alias"}, // contains q,u,i,t as a subsequence
		{Name: "quit"},            // real prefix match
	})
	m.Open()
	m = typeString(t, m, "quit")

	got := names(m.Matches())
	if len(got) == 0 || got[0] != "quit" {
		t.Errorf("matches = %v, want the prefix match %q first", got, "quit")
	}
}

func TestNoMatchesLeavesNothingSelected(t *testing.T) {
	m := typeString(t, openPalette(t), "zzzz")
	if len(m.Matches()) != 0 {
		t.Fatalf("expected no matches, got %v", names(m.Matches()))
	}
	if _, ok := m.Selected(); ok {
		t.Error("Selected() returned an item with nothing matching")
	}
}

// TestArgumentsDoNotFilter: only the command word filters the list, so
// typing ":search foo" keeps showing search rather than filtering itself to
// nothing once the argument is typed.
func TestArgumentsDoNotFilter(t *testing.T) {
	m := typeString(t, openPalette(t), "search foo bar")
	got := names(m.Matches())
	if len(got) != 1 || got[0] != "search" {
		t.Errorf("matches = %v, want [search]", got)
	}
	if m.Query() != "search foo bar" {
		t.Errorf("Query() = %q, want the whole line", m.Query())
	}
}

func TestSplitQuery(t *testing.T) {
	tests := []struct{ in, name, args string }{
		{"", "", ""},
		{"quit", "quit", ""},
		{"search foo", "search", "foo"},
		{"search  foo  bar", "search", "foo  bar"},
		{"  search foo", "search", "foo"},
	}
	for _, tc := range tests {
		name, args := SplitQuery(tc.in)
		if name != tc.name || args != tc.args {
			t.Errorf("SplitQuery(%q) = (%q, %q), want (%q, %q)",
				tc.in, name, args, tc.name, tc.args)
		}
	}
}

// --- Keys -----------------------------------------------------------------

// TestPrintablesTypeRatherThanNavigate is the deliberate divergence from the
// handoff, which specified j/k for movement. The palette is a text surface:
// if j and k navigated, ":jump" and ":keymap" could not be typed at all.
func TestPrintablesTypeRatherThanNavigate(t *testing.T) {
	m := typeString(t, openPalette(t), "k")
	if m.Query() != "k" {
		t.Errorf("Query() = %q after typing k, want %q — j/k must type, not move",
			m.Query(), "k")
	}
	// "keymap" prefix-matches and sorts first; "mark-read" also contains a
	// k and legitimately matches fuzzily. What matters is that the letter
	// filtered rather than moved the cursor.
	got := names(m.Matches())
	if len(got) == 0 || got[0] != "keymap" {
		t.Errorf("typing k matched %v, want keymap first", got)
	}
}

func TestArrowsMove(t *testing.T) {
	m := openPalette(t)
	first, _ := m.Selected()

	m, _ = m.Update(decodeKey(t, "\x1b[B")) // down
	second, ok := m.Selected()
	if !ok || second.Name == first.Name {
		t.Fatalf("down did not move selection off %q", first.Name)
	}

	m, _ = m.Update(decodeKey(t, "\x1b[A")) // up
	back, _ := m.Selected()
	if back.Name != first.Name {
		t.Errorf("up returned to %q, want %q", back.Name, first.Name)
	}
}

func TestMovementClampsAtBothEnds(t *testing.T) {
	m := openPalette(t)
	for range 20 {
		m, _ = m.Update(decodeKey(t, "\x1b[A")) // up past the top
	}
	if got, _ := m.Selected(); got.Name != "keymap" {
		t.Errorf("clamped top selection = %q, want keymap", got.Name)
	}
	for range 20 {
		m, _ = m.Update(decodeKey(t, "\x1b[B")) // down past the bottom
	}
	if got, _ := m.Selected(); got.Name != "search" {
		t.Errorf("clamped bottom selection = %q, want search", got.Name)
	}
}

// TestEditingResetsSelection: after another character the previously
// highlighted row is usually gone, and keeping a stale index would run a
// command the user is no longer looking at.
func TestEditingResetsSelection(t *testing.T) {
	m := openPalette(t)
	m, _ = m.Update(decodeKey(t, "\x1b[B")) // move off the first row
	m = typeString(t, m, "s")

	got, ok := m.Selected()
	if !ok || got.Name != "search" {
		t.Errorf("selection after typing = %q, want the first match", got.Name)
	}
}

func TestBackspaceAndClear(t *testing.T) {
	m := typeString(t, openPalette(t), "quit")
	m, _ = m.Update(decodeKey(t, "\x7f")) // backspace
	if m.Query() != "qui" {
		t.Errorf("Query() = %q after backspace, want %q", m.Query(), "qui")
	}

	m, _ = m.Update(decodeKey(t, "\x15")) // ctrl+u
	if m.Query() != "" {
		t.Errorf("Query() = %q after ctrl+u, want empty", m.Query())
	}
	if len(m.Matches()) != len(testItems()) {
		t.Error("clearing the query did not restore the full list")
	}
}

func TestEnterRunsAndEscapeCancels(t *testing.T) {
	m := typeString(t, openPalette(t), "quit")

	if _, action := m.Update(decodeKey(t, "\r")); action != ActionRun {
		t.Errorf("enter produced %v, want ActionRun", action)
	}
	if _, action := m.Update(decodeKey(t, "\x1b")); action != ActionCancel {
		t.Errorf("esc produced %v, want ActionCancel", action)
	}
}

func TestUpdateIsInertWhenClosed(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetItems(testItems())
	m, action := m.Update(decodeKey(t, "x"))
	if action != ActionNone || m.Query() != "" {
		t.Errorf("a closed palette consumed a key: action=%v query=%q", action, m.Query())
	}
}

func TestCloseDropsTheQuery(t *testing.T) {
	m := typeString(t, openPalette(t), "quit")
	m.Close()
	m.Open()
	if m.Query() != "" {
		t.Errorf("reopening resurrected the query %q", m.Query())
	}
}

// --- Tab completion -------------------------------------------------------

func TestTabCompletesTheSelectedName(t *testing.T) {
	m := typeString(t, openPalette(t), "qu")
	m, _ = m.Update(decodeKey(t, "\t"))
	if m.Query() != "quit" {
		t.Errorf("Query() = %q after tab, want %q", m.Query(), "quit")
	}
}

// TestTabLeavesRoomForAnArgument: completing a command that takes one should
// land the cursor where the argument goes, not butted against the name.
func TestTabCompletesWithTrailingSpaceForArgs(t *testing.T) {
	m := typeString(t, openPalette(t), "sea")
	m, _ = m.Update(decodeKey(t, "\t"))
	if m.Query() != "search " {
		t.Errorf("Query() = %q after tab, want %q", m.Query(), "search ")
	}
}

// TestTabKeepsTypedArguments: completing the name must not discard an
// argument the user already typed.
func TestTabKeepsTypedArguments(t *testing.T) {
	m := typeString(t, openPalette(t), "sea foo")
	m, _ = m.Update(decodeKey(t, "\t"))
	if m.Query() != "search foo" {
		t.Errorf("Query() = %q after tab, want %q", m.Query(), "search foo")
	}
}

func TestTabWithNoMatchIsInert(t *testing.T) {
	m := typeString(t, openPalette(t), "zzzz")
	m, _ = m.Update(decodeKey(t, "\t"))
	if m.Query() != "zzzz" {
		t.Errorf("Query() = %q, want it unchanged with nothing to complete", m.Query())
	}
}

// --- Rendering ------------------------------------------------------------

// TestViewLinesAreExactlyWide is the frame-integrity property: the overlay
// is placed over a live frame, so a row even one cell wide would shear it.
func TestViewLinesAreExactlyWide(t *testing.T) {
	cases := map[string]Model{
		"empty query": openPalette(t),
		"filtered":    typeString(t, openPalette(t), "qu"),
		"no matches":  typeString(t, openPalette(t), "zzzz"),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			view := m.View()
			if view == "" {
				t.Fatal("View() was empty while visible")
			}
			assertUniformWidth(t, view)
		})
	}
}

// TestViewHandlesWideRunes: a description full of double-width glyphs must
// not push the row past its budget. Rune-counting would keep 60 CJK runes
// for a 60-cell column and draw 120 cells, tearing the frame open.
func TestViewHandlesWideRunes(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetItems([]Item{
		{Name: "wide", Description: strings.Repeat("四", 60), Key: "w"},
	})
	m.Open()
	assertUniformWidth(t, m.View())
}

// assertUniformWidth checks every rendered row is the same width. The frame
// style adds its own border and padding, so the absolute number is the
// style's business; what must hold is that no row differs from its
// neighbours, since that is what shears a frame.
func assertUniformWidth(t *testing.T, view string) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		t.Fatal("empty view")
	}
	want := cell.Width(lines[0])
	if want < Width {
		t.Fatalf("frame is %d cells wide, narrower than the palette's %d", want, Width)
	}
	for i, line := range lines {
		if w := cell.Width(line); w != want {
			t.Errorf("row %d has display width %d, want %d (uniform): %q",
				i+1, w, want, line)
		}
	}
}

func TestViewIsEmptyWhenClosed(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetItems(testItems())
	if got := m.View(); got != "" {
		t.Errorf("View() = %q while closed, want empty", got)
	}
}

// TestViewShowsKeyEquivalents is why the registry carries them: the palette
// teaches the keymap instead of being a second place to learn it.
func TestViewShowsKeyEquivalents(t *testing.T) {
	view := openPalette(t).View()
	for _, want := range []string{"?", "ctrl+g"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not show the key equivalent %q", want)
		}
	}
}
