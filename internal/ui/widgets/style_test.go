package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// TestFitLineNeverWraps is the direct regression test for the root-cause
// class named in adversarial review: content built to fill totalWidth,
// then rendered through a real, padded, SHIPPED theme style via a bare
// style.Width(totalWidth) call, silently word-wraps onto extra lines
// instead of rendering as one line — because lipgloss.Style.Width treats
// its argument as the TOTAL width including the style's own padding.
// FitLine must never do this, for every padded style actually shipped in
// theme.DarkTheme() that this bug class hit: the chat list row styles
// (list.go), the folder tab styles (chatlist's tab bar), the status bar
// style, and the unread badge style.
func TestFitLineNeverWraps(t *testing.T) {
	th := theme.DarkTheme()
	styles := map[string]lipgloss.Style{
		"ChatListItem":       th.ChatListItem,
		"ChatListItemActive": th.ChatListItemActive,
		"Tab":                th.Tab,
		"TabActive":          th.TabActive,
		"StatusBar":          th.StatusBar,
		"ChatListUnread":     th.ChatListUnread,
	}

	for name, style := range styles {
		for _, width := range []int{6, 10, 20, 28, 80} {
			// Deliberately fill the *entire* width before the style's own
			// padding is even considered — exactly the shape of content
			// that triggered the wrap bug at every one of the four sites.
			content := strings.Repeat("x", width)
			out := FitLine(style, content, width)

			if n := lipgloss.Height(out); n != 1 {
				t.Errorf("%s width=%d: FitLine produced %d lines, want 1: %q", name, width, n, out)
			}
			if w := ansi.StringWidth(out); w > width {
				t.Errorf("%s width=%d: FitLine output has display width %d > %d: %q", name, width, w, width, out)
			}
		}
	}
}

// TestFitLinePadsShortContent checks short content is padded out to
// exactly totalWidth (through the style's own background/whitespace),
// not left short.
func TestFitLinePadsShortContent(t *testing.T) {
	th := theme.DarkTheme()
	out := FitLine(th.ChatListItem, "hi", 20)
	if w := ansi.StringWidth(out); w != 20 {
		t.Fatalf("FitLine(ChatListItem, \"hi\", 20) has display width %d, want exactly 20: %q", w, out)
	}
	if n := lipgloss.Height(out); n != 1 {
		t.Fatalf("FitLine(ChatListItem, \"hi\", 20) produced %d lines, want 1", n)
	}
}

// TestFitLineTruncatesOverBudgetContent checks wildly over-budget content
// is truncated to fit, not wrapped or left overflowing.
func TestFitLineTruncatesOverBudgetContent(t *testing.T) {
	th := theme.DarkTheme()
	out := FitLine(th.StatusBar, strings.Repeat("y", 200), 40)
	if w := ansi.StringWidth(out); w > 40 {
		t.Fatalf("FitLine over-budget content has display width %d, want <= 40: %q", w, out)
	}
	if n := lipgloss.Height(out); n != 1 {
		t.Fatalf("FitLine over-budget content produced %d lines, want 1: %q", n, out)
	}
}

// TestFitLineZeroWidth checks the degenerate zero/negative width case
// doesn't panic and returns something empty rather than garbage.
func TestFitLineZeroWidth(t *testing.T) {
	th := theme.DarkTheme()
	if got := FitLine(th.ChatListItem, "hi", 0); got != "" {
		t.Fatalf("FitLine(..., 0) = %q, want empty", got)
	}
	if got := FitLine(th.ChatListItem, "hi", -5); got != "" {
		t.Fatalf("FitLine(..., -5) = %q, want empty", got)
	}
}
