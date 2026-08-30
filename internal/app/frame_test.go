package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/golden"
	"github.com/imtaqin/telegram-cli/internal/ui/layout"
)

// goldenSizes are the reference terminal sizes decision 11 names. They are
// the sizes docs/fixtures covers, so a frame that is exact at all five is
// exact at the sizes the design was signed off against.
var goldenSizes = []struct{ w, h int }{
	{80, 24}, {100, 30}, {120, 40}, {137, 29}, {200, 60},
}

func framedModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := mainModel(t, PanelChatList)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return updated.(Model)
}

// TestFrameRowsAreExactlyWide is the assertion the whole harness was built
// for, and the one that must pass from day one: every rendered row is
// exactly the terminal width.
//
// This is width only, deliberately. Byte equality against the fixtures is
// the design contract, but it cannot pass until the thread grid, the chat
// list rows and the rail all land — the goldens are renders of a finished
// TUI 2.0. Separating the two assertions by lifetime is exactly why
// golden.Compare reports them as different DiffKinds.
func TestFrameRowsAreExactlyWide(t *testing.T) {
	for _, size := range goldenSizes {
		t.Run(sizeName(size.w, size.h), func(t *testing.T) {
			m := framedModel(t, size.w, size.h)
			assertFrameExact(t, m.View().Content, size.w, size.h)
		})
	}
}

// TestFrameIsExactAtEverySize sweeps well beyond the reference sizes,
// including the responsive boundaries and the degenerate small ones. A frame
// that is only correct at the five sizes someone thought to check is a frame
// that shears on somebody's terminal.
func TestFrameIsExactAtEverySize(t *testing.T) {
	widths := []int{
		20, 40, 60, 71, 72, 79, 80, 89, 90, 100, 117, 118, 119, 137, 200, 300,
	}
	heights := []int{3, 8, 11, 12, 19, 20, 24, 40, 60}

	for _, w := range widths {
		for _, h := range heights {
			m := framedModel(t, w, h)
			view := m.View().Content

			for i, line := range golden.SplitLines(view) {
				if got := cell.Width(line); got != w {
					t.Fatalf("%dx%d row %d: display width %d, want %d: %q",
						w, h, i+1, got, w, line)
				}
			}
		}
	}
}

// TestFrameSurvivesWideRunes: a chat title full of double-width glyphs is
// the classic way a frame tears, because a rune-counting budget lets the
// panel draw twice the cells it was given.
func TestFrameSurvivesWideRunes(t *testing.T) {
	m := framedModel(t, 100, 30)
	m.chatView.OpenChat(testChatID, strings.Repeat("四", 40))
	m.composer.SetChatId(testChatID)
	m.notify("四字熟語入門 " + strings.Repeat("📢", 20))

	assertFrameExact(t, m.View().Content, 100, 30)
}

// TestFrameHasTheRightChromeRows checks the vertical budget end to end: the
// top bar and hint bar are present exactly when the layout says, and the
// body gets what is left.
func TestFrameHasTheRightChromeRows(t *testing.T) {
	tests := []struct {
		h                    int
		wantTopBar, wantHint bool
	}{
		{24, true, true},
		{20, true, true},
		{19, true, false},
		{12, true, false},
		{11, false, false},
	}

	for _, tc := range tests {
		t.Run(sizeName(100, tc.h), func(t *testing.T) {
			m := framedModel(t, 100, tc.h)
			l := m.layout

			if l.TopBar != tc.wantTopBar || l.HintBar != tc.wantHint {
				t.Fatalf("chrome = (top %v, hint %v), want (%v, %v)",
					l.TopBar, l.HintBar, tc.wantTopBar, tc.wantHint)
			}

			lines := golden.SplitLines(m.View().Content)
			if len(lines) != tc.h {
				t.Fatalf("rendered %d rows, want %d", len(lines), tc.h)
			}

			// The top bar is the only row carrying the app mark.
			hasMark := strings.Contains(golden.StripANSI(lines[0]), "tg")
			if hasMark != tc.wantTopBar {
				t.Errorf("row 1 %s the app mark; top bar shown = %v",
					map[bool]string{true: "has", false: "lacks"}[hasMark], tc.wantTopBar)
			}
		})
	}
}

// TestFrameDrawsPanelRules pins the borderless contract: panels are divided
// by single-cell rules at the columns the layout computed, and there are no
// rounded box borders anywhere.
func TestFrameDrawsPanelRules(t *testing.T) {
	m := framedModel(t, 100, 30)
	view := golden.StripANSI(m.View().Content)

	for _, boxChar := range []string{"╭", "╮", "╰", "╯", "─"} {
		if strings.Contains(view, boxChar) {
			t.Errorf("the frame still draws a box border (%q); TUI 2.0 is borderless", boxChar)
		}
	}

	l := m.layout
	lines := golden.SplitLines(m.View().Content)
	// Body rows sit between the two chrome rows.
	body := lines[1 : len(lines)-1]

	ruleCol := l.ChatListWidth
	for i, line := range body {
		runes := []rune(golden.StripANSI(line))
		if ruleCol >= len(runes) {
			t.Fatalf("body row %d is only %d cells; expected a rule at %d",
				i+1, len(runes), ruleCol)
		}
		if runes[ruleCol] != '│' {
			t.Errorf("body row %d has %q at the rule column %d, want │",
				i+1, string(runes[ruleCol]), ruleCol)
		}
	}
}

// TestSinglePanelFrameIsExact covers the sub-72 path, which has no rule and
// no second column — the easiest place for an off-by-one to hide, since the
// arithmetic that normally accounts for the rule is skipped.
func TestSinglePanelFrameIsExact(t *testing.T) {
	for _, w := range []int{20, 40, 71} {
		m := framedModel(t, w, 24)
		if !m.layout.SinglePanel {
			t.Fatalf("%d columns should be single-panel", w)
		}
		assertFrameExact(t, m.View().Content, w, 24)
	}
}

// TestOverlaysKeepTheFrameExact: an overlay is placed over the frame, so a
// mis-sized overlay row tears the screen just as surely as a mis-sized panel.
func TestOverlaysKeepTheFrameExact(t *testing.T) {
	t.Run("palette", func(t *testing.T) {
		m := framedModel(t, 100, 30)
		updated, _ := m.Update(decodeKey(t, ":"))
		m = updated.(Model)
		if !m.palette.IsVisible() {
			t.Fatal("precondition: palette did not open")
		}
		assertFrameExact(t, m.View().Content, 100, 30)
	})

	t.Run("help", func(t *testing.T) {
		m := framedModel(t, 100, 30)
		m.help.SetVisible(true)
		assertFrameExact(t, m.View().Content, 100, 30)
	})
}

// TestLayoutMatchesTheGoldenGeometry ties the app's computed layout back to
// the fixture files, so the frame and the goldens cannot drift apart on
// region widths even before byte equality is achievable.
func TestLayoutMatchesTheGoldenGeometry(t *testing.T) {
	dir, err := golden.Dir()
	if err != nil {
		t.Fatalf("locating fixtures: %v", err)
	}

	for _, size := range goldenSizes {
		name := "frame-" + sizeName(size.w, size.h)
		t.Run(name, func(t *testing.T) {
			f, err := golden.Load(dir + "/" + name + ".txt")
			if err != nil {
				t.Fatalf("loading fixture: %v", err)
			}

			l := layout.Compute(f.Width, f.Height, 1, true)
			if l.TotalWidth() != f.Width {
				t.Errorf("layout regions sum to %d, fixture is %d wide",
					l.TotalWidth(), f.Width)
			}

			// The fixture's own rule columns, read back out of the file.
			wantRule := l.ChatListWidth
			row := golden.StripANSI(f.Lines[len(f.Lines)/2])
			runes := []rune(row)
			if wantRule < len(runes) && runes[wantRule] != '│' {
				t.Errorf("fixture has %q at column %d, but the layout puts the "+
					"chat list rule there", string(runes[wantRule]), wantRule)
			}
		})
	}
}

// TestClickingATopBarTabSwitchesFolder is the other half of the folder-tab
// click that moved out of chatlist: the top bar owns those pixels now, so
// the app routes row 0 to it and hands the result back to the chat list,
// which still owns folder state.
func TestClickingATopBarTabSwitchesFolder(t *testing.T) {
	m := framedModel(t, 120, 30)
	m.chatList.SetFoldersForTest([]string{"All", "Work", "News"})
	m.refreshChrome()

	if got := m.chatList.ActiveFolderIndex(); got != 0 {
		t.Fatalf("precondition: active folder = %d, want 0", got)
	}

	// Find a column inside the second tab the way a user would: by looking
	// at what is drawn. Rune index, not byte index — the row has multi-byte
	// box-drawing glyphs in it.
	col := columnOf(golden.StripANSI(m.topBar.View()), "2:Work")
	if col < 0 {
		t.Fatalf("the Work tab was not drawn: %q", golden.StripANSI(m.topBar.View()))
	}

	updated, _ := m.Update(tea.MouseClickMsg{X: col, Y: 0, Button: tea.MouseLeft})
	m = updated.(Model)

	if got := m.chatList.ActiveFolderIndex(); got != 1 {
		t.Errorf("clicking column %d (inside the Work tab) selected folder %d, want 1",
			col, got)
	}
}

// TestClickingTheFilterHeaderDoesNotSwitchFolders guards the bug the tab
// move could have introduced: row 0 of the CHAT LIST is the filter header
// now, where the tab bar used to be. A click there must not silently change
// folders.
func TestClickingTheFilterHeaderDoesNotSwitchFolders(t *testing.T) {
	m := framedModel(t, 120, 30)
	m.chatList.SetFoldersForTest([]string{"All", "Work", "News"})

	// Body row 0 — the chat list's filter header, one row below the top bar.
	updated, _ := m.Update(tea.MouseClickMsg{X: 10, Y: 1, Button: tea.MouseLeft})
	m = updated.(Model)

	if got := m.chatList.ActiveFolderIndex(); got != 0 {
		t.Errorf("clicking the filter header selected folder %d, want it unchanged", got)
	}
}

// --- helpers --------------------------------------------------------------

// columnOf returns the starting column of needle in a rendered row, or -1.
func columnOf(row, needle string) int {
	runes, want := []rune(row), []rune(needle)
	for i := 0; i+len(want) <= len(runes); i++ {
		if string(runes[i:i+len(want)]) == needle {
			return i
		}
	}
	return -1
}

func assertFrameExact(t *testing.T, view string, w, h int) {
	t.Helper()

	lines := golden.SplitLines(view)
	if len(lines) != h {
		t.Errorf("rendered %d rows, want %d", len(lines), h)
	}
	for i, line := range lines {
		if got := cell.Width(line); got != w {
			t.Errorf("row %d: display width %d, want %d: %q", i+1, got, w, line)
		}
	}
}

func sizeName(w, h int) string {
	return itoa(w) + "x" + itoa(h)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
