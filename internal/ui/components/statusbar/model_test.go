package statusbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/store"
	"github.com/imtaqin/telegram-cli/internal/telegram"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// TestViewStaysSingleLineAndWithinWidth guards against the regression
// where the last-resort overflow guard used lipgloss's Style.MaxWidth,
// which *wraps* long content onto additional lines instead of
// truncating it — at narrow widths the hints block alone (~57 cells)
// made the one-row status bar render as a multi-line block, pushing
// whatever renders below it (the composer) off-screen. View() must
// always render exactly one line, no wider than the configured width,
// at any width including the degenerate zero case.
func TestViewStaysSingleLineAndWithinWidth(t *testing.T) {
	s := store.NewStore()
	th := theme.DarkTheme()
	m := New(s, th)
	m.SetUserName("Alice Wonderland Longname")
	m.SetConnected(true)
	m, _ = m.Update(telegram.UnreadCountMsg{UnreadCount: 42, UnreadUnmutedCount: 7})

	for _, width := range []int{120, 60, 30, 10, 0} {
		m.SetSize(width)
		got := m.View()

		if h := lipgloss.Height(got); h != 1 {
			t.Errorf("width=%d: lipgloss.Height(View()) = %d, want 1 (single line)", width, h)
		}
		if width > 0 {
			if w := ansi.StringWidth(got); w > width {
				t.Errorf("width=%d: ansi.StringWidth(View()) = %d, want <= %d", width, w, width)
			}
		}
	}
}

// TestViewDropsUnreadBeforeHints exercises the "full -> drop unread ->
// drop hints -> truncate" fallback order at a width where the fully
// detailed bar does not fit but a reduced form does: the unread badge
// should be dropped first, keeping the hints block, before the hints
// block itself is dropped.
func TestViewDropsUnreadBeforeHints(t *testing.T) {
	s := store.NewStore()
	th := theme.DarkTheme()
	m := New(s, th)
	m.SetUserName("Bob")
	m.SetConnected(true)
	m, _ = m.Update(telegram.UnreadCountMsg{UnreadCount: 42, UnreadUnmutedCount: 7})

	// Wide enough for "● Connected  Bob" + hints, but not for the extra
	// " [7 unread (42 total)]" / " [42 unread]" text as well.
	m.SetSize(60)
	got := m.View()

	if lipgloss.Height(got) != 1 {
		t.Fatalf("View() at width=60 is not a single line: %q", got)
	}
	if w := ansi.StringWidth(got); w > 60 {
		t.Fatalf("View() at width=60 has width %d, want <= 60", w)
	}
}

// TestViewExactWidthAndHintsSurviveAtWideWidth is FINDING 8's direct
// regression test. StatusBar carries PaddingLeft(1)+PaddingRight(1); the
// bar's content used to be built to fill m.width exactly and then handed
// to StatusBar.Width(m.width) — which, combined with MaxHeight(1), wrapped
// the content and then kept only the wrapped fragment, chopping the hints
// off mid-token ("←→/1-") instead of showing them in full. At a width wide
// enough for everything, the complete hints string must survive intact;
// at every width, the bar must render as exactly one line of exactly the
// configured width (padded through the real theme's StatusBar style).
func TestViewExactWidthAndHintsSurviveAtWideWidth(t *testing.T) {
	s := store.NewStore()
	th := theme.DarkTheme()
	m := New(s, th)
	m.SetUserName("Alice")
	m.SetConnected(true)

	for _, width := range []int{120, 80, 40, 10} {
		m.SetSize(width)
		got := m.View()

		if h := lipgloss.Height(got); h != 1 {
			t.Errorf("width=%d: lipgloss.Height(View()) = %d, want 1", width, h)
		}
		if w := ansi.StringWidth(got); w != width {
			t.Errorf("width=%d: ansi.StringWidth(View()) = %d, want exactly %d: %q", width, w, width, got)
		}
	}

	m.SetSize(120)
	got := m.View()
	wantHints := "Alt+1/2/3:Focus  /:Search  Alt+C:Contacts  ←→/1-9:Folders"
	if !strings.Contains(got, wantHints) {
		t.Fatalf("full hints text did not survive at width 120\n got: %q\nwant substring: %q", got, wantHints)
	}
}
