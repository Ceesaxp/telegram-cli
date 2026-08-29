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

// testHints stands in for what internal/app supplies from its resolved
// keys (Model.statusHints). This component has no hint strip of its own —
// a built-in default would be a second copy of the key table, free to
// drift from the dispatcher — so every test that needs hints to exist
// sets them, exactly as the app does in New.
const testHints = "?:Help  h/l:Panels  /:Find  [/]:Folders  q:Quit"

// TestViewStaysSingleLineAndWithinWidth guards against the regression
// where the last-resort overflow guard used lipgloss's Style.MaxWidth,
// which *wraps* long content onto additional lines instead of
// truncating it — at narrow widths the hints block alone (60+ cells)
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
	m.SetHints(testHints)
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
	m.SetHints(testHints)
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
	m.SetHints(testHints)

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
	if !strings.Contains(got, testHints) {
		t.Fatalf("full hints text did not survive at width 120\n got: %q\nwant substring: %q", got, testHints)
	}
}

// TestNoHintsUntilTheCallerSuppliesThem: this component ships no hint
// strip of its own, so an un-configured bar shows none — and, crucially,
// an empty strip must cost ZERO cells. StatusBar carries
// PaddingLeft(1)+PaddingRight(1), so styling "" would still paint two
// cells on the right and skew the layout.
func TestNoHintsUntilTheCallerSuppliesThem(t *testing.T) {
	s := store.NewStore()
	th := theme.DarkTheme()
	m := New(s, th)
	m.SetUserName("Alice")
	m.SetConnected(true)
	m.SetSize(120)

	got := ansi.Strip(m.View())
	if strings.Contains(got, ":") && strings.Contains(got, "Help") {
		t.Fatalf("an un-configured status bar rendered a hint strip: %q", got)
	}
	if content := strings.Join(strings.Fields(got), " "); content != "● Connected Alice" {
		t.Fatalf("un-configured bar reads %q, want only the connection state and user name", content)
	}
	if w := ansi.StringWidth(m.View()); w != 120 {
		t.Fatalf("un-configured bar is %d cells, want exactly 120", w)
	}
	if h := lipgloss.Height(m.View()); h != 1 {
		t.Fatalf("un-configured bar is %d lines, want 1", h)
	}
}

// TestSetHintsSetsAndClears covers the SetHints contract: the caller
// supplies a strip built from its resolved keys (so a rebind is
// reflected here), and an empty string clears it again.
func TestSetHintsSetsAndClears(t *testing.T) {
	s := store.NewStore()
	th := theme.DarkTheme()
	m := New(s, th)
	m.SetUserName("Alice")
	m.SetConnected(true)
	m.SetSize(200)

	custom := "?:Help  h/l:Panels  q:Quit  ctrl+f:Find"
	m.SetHints(custom)
	if got := m.View(); !strings.Contains(got, custom) {
		t.Fatalf("after SetHints(%q), View() = %q, want the custom hints", custom, got)
	}

	// A rebind replaces the strip wholesale — no trace of the old one.
	rebound := "?:Help  h/l:Panels  q:Quit  /:Find"
	m.SetHints(rebound)
	got := m.View()
	if !strings.Contains(got, rebound) {
		t.Fatalf("after a rebind, View() = %q, want %q", got, rebound)
	}
	if strings.Contains(got, custom) {
		t.Fatalf("after a rebind, View() still advertises the old strip: %q", got)
	}

	m.SetHints("")
	got = ansi.Strip(m.View())
	if strings.Contains(got, "Help") {
		t.Fatalf("SetHints(\"\") did not clear the strip: %q", got)
	}
	if w := ansi.StringWidth(m.View()); w != 200 {
		t.Fatalf("cleared bar is %d cells, want exactly 200", w)
	}
}

// TestSetHintsPreservesWidthGuarantees re-runs the single-line/exact-width
// guarantees with a caller-supplied strip far longer than the default:
// the hint text is now unbounded, so the "full -> drop unread -> drop
// hints -> truncate" ladder has to hold for it too.
func TestSetHintsPreservesWidthGuarantees(t *testing.T) {
	s := store.NewStore()
	th := theme.DarkTheme()
	m := New(s, th)
	m.SetUserName("Alice Wonderland Longname")
	m.SetConnected(true)
	m, _ = m.Update(telegram.UnreadCountMsg{UnreadCount: 42, UnreadUnmutedCount: 7})
	m.SetHints(strings.Repeat("?:Help  Alt+1/2/3:Focus  ", 10))

	// Widths at or above the StatusBar style's own frame size: the bar
	// must be exactly one line of exactly that width.
	for _, width := range []int{200, 120, 60, 30, 10} {
		m.SetSize(width)
		got := m.View()

		if h := lipgloss.Height(got); h != 1 {
			t.Errorf("width=%d: lipgloss.Height(View()) = %d, want 1 (single line)", width, h)
		}
		if w := ansi.StringWidth(got); w != width {
			t.Errorf("width=%d: ansi.StringWidth(View()) = %d, want exactly %d: %q", width, w, width, got)
		}
	}

	// Degenerate widths (below the style's 2-cell padding frame): still
	// one line, never a wrapped block.
	for _, width := range []int{1, 0} {
		m.SetSize(width)
		if h := lipgloss.Height(m.View()); h != 1 {
			t.Errorf("width=%d: lipgloss.Height(View()) = %d, want 1 (single line)", width, h)
		}
	}
}
