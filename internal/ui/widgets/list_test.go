package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// newThemedList builds a List wired to the real shipped theme, mirroring
// chatlist.New()'s wiring exactly. Row/bar rendering tests must use this
// (or an equivalent real-theme construction) rather than a zero-value
// List{}: the wrap-class bug this file guards against (lipgloss
// Style.Width(w) wrapping instead of truncating when the style itself
// carries padding) is invisible with zero-value styles, since those have
// no padding to trigger it — that's exactly how it escaped the original
// version of this test file.
func newThemedList() List {
	th := theme.DarkTheme()
	l := NewList()
	l.StyleNormal = th.ChatListItem
	l.StyleActive = th.ChatListItemActive
	l.StyleTitle = th.ChatListTitle
	l.StyleSub = th.ChatListPreview
	l.StyleMeta = th.ChatListTime
	l.StyleBadge = th.ChatListUnread
	l.StyleOnline = th.ChatListOnline
	return l
}

// stressItems returns list items designed to expose rune-vs-cell width
// mismatches: mixed double-width emoji + flag sequences + Cyrillic in the
// title, CJK text in the subtitle, a long badge, and an oversized avatar
// block whose lines are wider than the reserved avatar column.
func stressItems() []ListItem {
	return []ListItem{
		{
			ID:       "1",
			Title:    "🔕📢 Иван Петров 🇷🇺 группа обсуждения",
			Subtitle: "你好世界，这是一个很长的中文字幕文本用于测试宽度截断",
			Badge:    "999999999999",
			Meta:     "Пн 15:04",
			Online:   true,
			Muted:    true,
		},
		{
			ID:       "2",
			Title:    "Short",
			Subtitle: "normal subtitle",
			Badge:    "3",
			Meta:     "12:00",
			Avatar:   "🖼️🖼️🖼️🖼️🖼️\n🖼️🖼️🖼️🖼️🖼️", // deliberately wider than the 4-cell avatar column
		},
		{
			ID:       "3",
			Title:    "📢📢📢📢📢📢📢📢📢📢 channel of emoji",
			Subtitle: "",
			Badge:    "",
			Meta:     "",
		},
	}
}

// TestViewRowsNeverExceedWidth is the regression test for the panel-frame
// shear bug: truncate() used to count runes, so a title/subtitle full of
// double-cell glyphs (emoji, flags, CJK) rendered wider than the configured
// Width. Every line List.View() produces — for every stress item, at every
// width including narrow terminals, through the real shipped theme styles
// — must be at most Width display cells (ansi.StringWidth), matching what
// lipgloss.JoinHorizontal assumes when composing this panel next to others.
func TestViewRowsNeverExceedWidth(t *testing.T) {
	widths := []int{6, 10, 20, 30, 60, 120}

	for _, width := range widths {
		l := newThemedList()
		l.Width = width
		l.Height = 10 // enough rows to show every stress item
		l.SetItems(stressItems())

		out := l.View()
		for lineNo, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width=%d: line %d has display width %d > %d: %q",
					width, lineNo, w, width, line)
			}
		}
	}
}

// TestViewRowsNeverExceedWidthActiveAndMuted covers the active-row and
// muted-row style paths specifically, since they take a different branch
// through the title/badge styling before the final cell clamp.
func TestViewRowsNeverExceedWidthActiveAndMuted(t *testing.T) {
	for _, width := range []int{10, 20, 30} {
		l := newThemedList()
		l.Width = width
		l.Height = 10
		items := stressItems()
		l.SetItems(items)
		l.Cursor = 0 // the muted+online item becomes "active"

		out := l.View()
		for lineNo, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width=%d: active-row line %d has display width %d > %d: %q",
					width, lineNo, w, width, line)
			}
		}
	}
}

// TestViewEveryItemRendersExactlyItemHeightLines is FINDING 1's direct
// regression test. Through the real theme (ChatListItem/ChatListItemActive
// carry PaddingLeft(1)+PaddingRight(1)), rendering a row's content — built
// to fill the full column width — used to hand a padded style a
// Width(l.Width) call with content already at that width, which lipgloss
// interprets as an overflow and WORD-WRAPS onto 3-4 lines instead of the
// fixed 2-line stride ItemAtRow's row-to-index arithmetic assumes. Every
// item, regardless of content (emoji/CJK/long text included, via
// stressItems), must render to exactly itemHeight (2) lines at every
// width, including the app's default geometry-adjacent widths (26, 28)
// and narrower.
func TestViewEveryItemRendersExactlyItemHeightLines(t *testing.T) {
	widths := []int{20, 26, 28, 40, 80}
	items := stressItems()

	for _, width := range widths {
		l := newThemedList()
		l.Width = width
		l.Height = 40 // tall enough to show every item without scrolling
		l.SetItems(items)

		out := l.View()
		lines := strings.Split(out, "\n")
		want := len(items) * 2
		if len(lines) != want {
			t.Errorf("width=%d: View() produced %d lines, want exactly %d (%d items * itemHeight 2) — "+
				"a mismatch here desyncs ItemAtRow's fixed 2-line-per-item stride from what's actually painted",
				width, len(lines), want, len(items))
		}
		for lineNo, ln := range lines {
			if w := ansi.StringWidth(ln); w > width {
				t.Errorf("width=%d: line %d has display width %d > %d: %q", width, lineNo, w, width, ln)
			}
		}
	}
}

// TestClickMappingSurvivesRealThemeRendering exercises the actual bug
// scenario from FINDING 1's report ("clicking Carol opens Dave"): with
// real, padded theme styles and a title/subtitle long enough to have
// wrapped under the old code, ItemAtRow's arithmetic must still agree
// with which item is actually painted at each rendered line — checked by
// grepping the rendered output, not just trusting the arithmetic in
// isolation.
func TestClickMappingSurvivesRealThemeRendering(t *testing.T) {
	l := newThemedList()
	l.Width = 28 // ChatListWidth at the app's default 80-col geometry
	l.Height = 40
	l.SetItems([]ListItem{
		{ID: "1", Title: "Alice", Subtitle: "hi"},
		{ID: "2", Title: "Bob has a very long name that would have wrapped under the old code", Subtitle: "a long subtitle too, long enough to matter for the wrap bug"},
		{ID: "3", Title: "Carol", Subtitle: "short"},
		{ID: "4", Title: "Dave", Subtitle: "short2"},
	})

	out := l.View()
	lines := strings.Split(out, "\n")
	if want := 4 * 2; len(lines) != want {
		t.Fatalf("View() produced %d lines, want %d (4 items * itemHeight 2)", len(lines), want)
	}

	// Row 4 (0-based, local to the list) is Carol's title line under the
	// fixed 2-line stride: rows 0-1 Alice, 2-3 Bob, 4-5 Carol, 6-7 Dave.
	if idx := l.ItemAtRow(4); idx != 2 {
		t.Fatalf("ItemAtRow(4) = %d, want 2 (Carol) — this is the exact desync that opened the wrong chat on click", idx)
	}
	if !strings.Contains(lines[4], "Carol") {
		t.Fatalf("rendered line 4 = %q, want it to contain %q (ItemAtRow(4) says this row is Carol's)", lines[4], "Carol")
	}
	if idx := l.ItemAtRow(6); idx != 3 {
		t.Fatalf("ItemAtRow(6) = %d, want 3 (Dave)", idx)
	}
	if !strings.Contains(lines[6], "Dave") {
		t.Fatalf("rendered line 6 = %q, want it to contain %q (ItemAtRow(6) says this row is Dave's)", lines[6], "Dave")
	}
	// Bob's rows (2-3) must not have bled into Carol's or Dave's slot.
	if strings.Contains(lines[2], "Carol") || strings.Contains(lines[3], "Carol") {
		t.Fatalf("Bob's rows (2-3) unexpectedly contain Carol's content: %q / %q", lines[2], lines[3])
	}
}

// TestMetaSurvivesLongTitle is FINDING 5's regression test: a long title
// must truncate to make room for the timestamp, not consume the whole
// column and starve the timestamp out.
func TestMetaSurvivesLongTitle(t *testing.T) {
	l := newThemedList()
	l.Width = 28
	l.Height = 10
	l.SetItems([]ListItem{
		{ID: "1", Title: "This is a very long chat title that would consume the entire row width on its own", Meta: "12:00"},
	})

	out := l.View()
	if !strings.Contains(out, "12:00") {
		t.Fatalf("timestamp did not survive a long title; View() =\n%s", out)
	}
}

// TestBadgeSurvivesLongSubtitle is FINDING 5's badge counterpart: a long
// subtitle must truncate to make room for the unread badge.
func TestBadgeSurvivesLongSubtitle(t *testing.T) {
	l := newThemedList()
	l.Width = 28
	l.Height = 10
	l.SetItems([]ListItem{
		{ID: "1", Title: "Alice", Subtitle: "This is a very long subtitle message that would consume the entire row width on its own", Badge: "5"},
	})

	out := l.View()
	if !strings.Contains(out, "5") {
		t.Fatalf("badge did not survive a long subtitle; View() =\n%s", out)
	}
}

// The former TestTruncateIsCellAware and TestFitCellPadsAndClamps moved to
// internal/ui/cell along with the helpers themselves: this package's
// truncate() and fitCell() were duplicates of chatlist.truncateLabel and
// help.truncatePlain/padPlain, and all of them are now cell.Truncate,
// cell.Pad, and cell.Fit. The double-width-rune cases they guarded moved
// with them; see TestTruncateNeverExceedsBudgetOnWideRunes and
// TestFitIsExact there.
//
// The row-composition tests above still exercise those helpers through
// List.View(), which is what actually matters here.
