package theme

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile. Under Ascii, Style.Render is the identity
// function, and a test asking whether two styles differ would find that they
// do not — because neither emitted anything.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	os.Exit(m.Run())
}

// The overlay vocabulary exists so six components agree about what a title
// is. Agreeing is only useful if the answers are also distinguishable from
// each other — a title that renders like body copy is a title nobody can
// see, and that is the state four of these surfaces were in when they were
// drawing from the pre-2.0 theme.
func TestTheOverlayStylesAreDistinguishable(t *testing.T) {
	r := DarkRoles(false)
	const sample = "sample"

	rendered := map[string]string{
		"title":    OverlayTitle(r).Render(sample),
		"body":     OverlayBody(r).Render(sample),
		"muted":    OverlayMuted(r).Render(sample),
		"key":      OverlayKey(r).Render(sample),
		"selected": OverlaySelected(r).Render(sample),
		"input":    OverlayInput(r).Render(sample),
		"error":    OverlayError(r).Render(sample),
		"success":  OverlaySuccess(r).Render(sample),
	}

	seen := map[string]string{}
	for name, out := range rendered {
		if other, clash := seen[out]; clash {
			t.Errorf("%q and %q render identically: %q", name, other, out)
		}
		seen[out] = name
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("%q emitted no styling at all", name)
		}
	}
}

// The selected row is marked by a BACKGROUND, not only by a brighter
// foreground.
//
// The lesson came from the top bar's folder tabs, where "active is bright"
// marked nothing because the labels were colour emoji, which ignore the
// foreground they are given. Overlay rows carry chat titles, which are just
// as likely to be emoji.
func TestTheSelectedRowHasABackground(t *testing.T) {
	r := DarkRoles(false)
	out := OverlaySelected(r).Render("row")

	if !strings.Contains(out, "48;5;"+string(r.Sel)) {
		t.Errorf("the selected style has no background: %q", out)
	}
}

// The frame is a frame: a border and a surface, so an overlay reads as a
// thing on top of the UI rather than as text mixed into it.
func TestTheOverlayFrameIsBordered(t *testing.T) {
	r := DarkRoles(false)
	out := OverlayFrame(r).Render("x")

	if lipgloss.Height(out) < 3 {
		t.Errorf("the overlay frame draws no border: %q", out)
	}
	if !strings.Contains(out, "48;5;"+string(r.Panel)) {
		t.Errorf("the overlay frame has no panel surface: %q", out)
	}
}
