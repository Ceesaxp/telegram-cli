package app

import (
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/dialog"
)

// Every row of an open overlay is painted edge to edge, the same rule the
// frame holds for its own rows (divergence 19).
//
// The overlays were migrated to the semantic palette after that fix landed
// and never got cell.Fill with it, so each one assembled a row out of styled
// spans and rendered it through a background style — where the first span's
// own ESC[0m takes the surface with it. The help card was painting 23 of 112
// cells on a binding row: a panel that stops a fifth of the way across and
// shows the terminal through the rest.
func TestAnOpenOverlayIsPaintedEdgeToEdge(t *testing.T) {
	tests := []struct {
		name string
		open func(Model) Model
	}{
		{"help", func(m Model) Model {
			m.help.SetVisible(true)
			return m
		}},
		{"the command palette", func(m Model) Model {
			updated, _ := m.Update(decodeKey(t, ":"))
			return updated.(Model)
		}},
		{"search", func(m Model) Model {
			updated, _ := m.Update(decodeKey(t, "\x07")) // ctrl+g
			return updated.(Model)
		}},
		{"a dialog", func(m Model) Model {
			d := dialog.NewConfirm(m.roles, "delete", "Delete Message",
				"Are you sure?")
			m.dialog = &d
			return m
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.open(framedModel(t, 120, 40))

			for i, line := range strings.Split(m.View().Content, "\n") {
				if w := cell.Width(line); w != 120 {
					t.Fatalf("row %d is %d cells, want 120", i, w)
				}
				if p := cell.PaintedWidth(line); p != 120 {
					t.Errorf("row %d: painted %d of 120 cells, dying at column %d\n%s",
						i, p, p, strings.ReplaceAll(line, "\x1b", "ESC"))
				}
			}
		})
	}
}
