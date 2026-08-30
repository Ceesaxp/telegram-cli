package hintbar

import (
	"strings"
	"testing"

	"github.com/imtaqin/telegram-cli/internal/ui/cell"
	"github.com/imtaqin/telegram-cli/internal/ui/theme"
)

// goldenHints is the chat-view NORMAL set as the fixtures render it, in the
// order the bar drops them: whatever is last goes first.
func goldenHints() []Hint {
	return []Hint{
		{Key: "q", Label: "quit"},
		{Key: "i", Label: "compose"},
		{Key: ":", Label: "command"},
		{Key: "r", Label: "reply"},
		{Key: "y", Label: "yank"},
		{Key: "e", Label: "edit"},
		{Key: "?", Label: "keymap"},
	}
}

func newBar(width int) Model {
	m := New(theme.DarkRoles(false))
	m.SetWidth(width)
	m.SetHints(goldenHints())
	m.SetRight("idx 214 msgs · 9 buffers · 37 unread")
	return m
}

// TestHintCountsMatchTheGoldens pins the arithmetic against the fixtures:
// the bar keeps the longest PREFIX of the hint set that leaves at least
// MinGap before the right group. These counts were read out of the golden
// frames, not chosen.
func TestHintCountsMatchTheGoldens(t *testing.T) {
	tests := []struct{ width, wantHints int }{
		{80, 4},
		{100, 6},
		{120, 7},
		{137, 7},
		{200, 7},
	}
	for _, tc := range tests {
		t.Run(itoa(tc.width), func(t *testing.T) {
			view := golden(newBar(tc.width).View())
			for i, h := range goldenHints() {
				shown := strings.Contains(view, h.String())
				want := i < tc.wantHints
				if shown != want {
					t.Errorf("hint %q shown=%v, want %v at width %d",
						h.String(), shown, want, tc.width)
				}
			}
		})
	}
}

// TestHintsShownAreAlwaysAPrefix is what "drop whole hints from the right"
// actually means: the shown set can shrink, but never develop a hole. A bar
// showing "quit, compose, edit" would mean a hint was dropped from the
// middle, which no width rule should ever produce.
//
// It also pins that the left group is rendered EXACTLY as the prefix join —
// one leading space, hints joined by two — so a hint cannot be half-drawn.
func TestHintsShownAreAlwaysAPrefix(t *testing.T) {
	hints := goldenHints()

	for w := 20; w <= 250; w++ {
		view := golden(newBar(w).View())

		// Find the largest n whose exact prefix rendering is present.
		best := 0
		for n := len(hints); n >= 1; n-- {
			if strings.HasPrefix(view, " "+joinHints(hints[:n])) {
				best = n
				break
			}
		}

		// Nothing beyond that prefix may appear anywhere on the row.
		for i := best; i < len(hints); i++ {
			if strings.Contains(view, joinHints(hints[i:i+1])) {
				t.Errorf("width %d: hint %q is shown but the prefix stops at %d: %q",
					w, hints[i].String(), best, view)
			}
		}
	}
}

func joinHints(hints []Hint) string {
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		parts = append(parts, h.String())
	}
	return strings.Join(parts, "  ")
}

// TestViewIsExactlyWide is the property the frame depends on.
func TestViewIsExactlyWide(t *testing.T) {
	for w := 1; w <= 250; w++ {
		if got := cell.Width(newBar(w).View()); got != w {
			t.Fatalf("width %d: View() is %d cells", w, got)
		}
	}
}

// TestNoticeOwnsTheRow: a transient error is more important than hints the
// user already knows, so it takes the whole row.
func TestNoticeOwnsTheRow(t *testing.T) {
	m := newBar(100)
	m.SetNotice("⚠ send failed", "error")

	view := golden(m.View())
	if !strings.Contains(view, "send failed") {
		t.Error("the notice is not shown")
	}
	if strings.Contains(view, "q quit") {
		t.Error("hints are still shown under a notice")
	}
	if got := cell.Width(m.View()); got != 100 {
		t.Errorf("a notice row is %d cells, want 100", got)
	}

	m.ClearNotice()
	if !strings.Contains(golden(m.View()), "q quit") {
		t.Error("clearing the notice did not restore the hints")
	}
}

// TestWideRunesInANotice: a notice is arbitrary text from an error, so it
// can contain anything at all and must still not tear the row.
func TestWideRunesInANotice(t *testing.T) {
	m := newBar(80)
	m.SetNotice("⚠ "+strings.Repeat("四", 60), "error")
	if got := cell.Width(m.View()); got != 80 {
		t.Errorf("a wide-rune notice rendered %d cells, want 80", got)
	}
}

func TestEmptyHintsStillFillTheRow(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetWidth(60)
	if got := cell.Width(m.View()); got != 60 {
		t.Errorf("an empty bar is %d cells, want 60", got)
	}
}

func TestZeroWidthIsEmpty(t *testing.T) {
	m := New(theme.DarkRoles(false))
	m.SetWidth(0)
	if got := m.View(); got != "" {
		t.Errorf("View() = %q at zero width, want empty", got)
	}
}

func golden(s string) string { return stripANSI(s) }

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && (r == 'm' || r == 'K'):
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
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
