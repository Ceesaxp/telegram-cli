package composer

import (
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// SetRoles reaches the styles New handed the textarea, not only the palette
// the composer's own rows are drawn from. It used to stop at the palette, so
// the one component that looked re-themable was half re-themable.
//
// Asserted on the styles rather than on the view: the composer draws its
// rows itself and never asks the textarea to, so nothing it renders today
// would show the difference — which is exactly how a stale style would ship
// the day that changes.
func TestSetRolesRestylesTheTextarea(t *testing.T) {
	before, _ := theme.MarkerRoles()
	after, _ := theme.SecondMarkerRoles()

	m := New(before)
	m.SetRoles(after)

	for _, tt := range []struct {
		name      string
		got, want lipgloss.TerminalColor
	}{
		{"text foreground", m.textarea.Style.GetForeground(), after.Fg},
		{"text background", m.textarea.Style.GetBackground(), after.Panel},
		{"placeholder foreground", m.textarea.StylePlaceholder.GetForeground(), after.Dim},
		{"placeholder background", m.textarea.StylePlaceholder.GetBackground(), after.Panel},
	} {
		if tt.got != tt.want {
			t.Errorf("the textarea's %s is %v after SetRoles, want %v", tt.name, tt.got, tt.want)
		}
	}
}
