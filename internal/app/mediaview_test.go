package app

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/components/chatview"
)

func overlayModel(t *testing.T) Model {
	t.Helper()
	m := framedModel(t, 100, 30)
	updated, _ := m.Update(chatview.OpenPhotoMsg{Caption: "photo · nadia · 1280×960"})
	m = updated.(Model)
	if !m.mediaView.IsVisible() {
		t.Fatal("precondition: the overlay did not open")
	}
	return m
}

// The overlay is the whole screen, and the frame's invariants are the
// frame's invariants: every row exactly the width, every cell painted.
func TestTheMediaOverlayKeepsTheFrameExact(t *testing.T) {
	m := overlayModel(t)
	for i, line := range strings.Split(m.View().Content, "\n") {
		if got := cell.Width(line); got != 100 {
			t.Errorf("row %d is %d cells, want 100", i, got)
		}
		if p := cell.PaintedWidth(line); p != 100 {
			t.Errorf("row %d: painted %d of 100 cells", i, p)
		}
	}
}

// While it is up it owns the keyboard, for the same reason the help card
// does: a key that reached a panel behind it would take effect invisibly.
func TestTheMediaOverlayOwnsTheKeyboard(t *testing.T) {
	m := overlayModel(t)
	before := m.focus

	for _, k := range []string{"j", "\t", "i", "r", "`"} {
		updated, _ := m.Update(decodeKey(t, k))
		got := updated.(Model)
		if !got.mediaView.IsVisible() {
			t.Errorf("%q closed the overlay", k)
		}
		if got.focus != before {
			t.Errorf("%q changed focus behind the overlay", k)
		}
		if got.Mode() != ModeNormal {
			t.Errorf("%q left the app in %v behind the overlay", k, got.Mode())
		}
	}
}

func TestEscapeClosesTheMediaOverlay(t *testing.T) {
	m := overlayModel(t)
	updated, _ := m.Update(decodeKey(t, "\x1b"))
	if updated.(Model).mediaView.IsVisible() {
		t.Error("esc did not close the overlay")
	}
}

// TestQClosesNoOverlay is decision I-8. q used to close this overlay and,
// one keystroke later, quit the client — the same double-press the help card
// had. q has one meaning now, and closing an overlay is not it: the key is
// swallowed here like every other key behind a full-screen surface.
func TestQClosesNoOverlay(t *testing.T) {
	m := overlayModel(t)
	updated, cmd := m.Update(decodeKey(t, "q"))
	got := updated.(Model)

	if !got.mediaView.IsVisible() {
		t.Error("q closed the media overlay; only esc closes it")
	}
	if quits(cmd) {
		t.Error("q quit from behind the media overlay")
	}
	if got.dialog != nil {
		t.Error("q raised the quit confirmation from the overlay")
	}
}

// A download outlives the keypress that started it. A picture delivered into
// an overlay the user has already dismissed is not merely invisible — it
// stays in the overlay's state, and the NEXT photo they open shows the old
// one until its own download lands.
func TestAPhotoArrivingAfterTheOverlayClosedIsDropped(t *testing.T) {
	m := overlayModel(t)
	updated, _ := m.Update(decodeKey(t, "\x1b"))
	m = updated.(Model)

	updated, _ = m.Update(chatview.OpenedPhotoMsg{Path: photoFixture(t)})
	m = updated.(Model)
	if m.mediaView.IsVisible() {
		t.Error("a late download reopened the overlay")
	}

	// Open a second overlay: it must be waiting for its own picture, not
	// showing the one that arrived too late.
	updated, _ = m.Update(chatview.OpenPhotoMsg{Caption: "photo · sam"})
	view := updated.(Model).View().Content
	if !strings.Contains(view, "downloading") {
		t.Error("the new overlay is not waiting for its own download")
	}
	if strings.Contains(view, "▀") {
		t.Error("the new overlay is showing the stale picture")
	}
}

// photoFixture writes a small PNG the overlay can actually draw, so a test
// about a stale picture has a picture to go stale.
func photoFixture(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return path
}

// The user asked for this photo; closing the window they just opened is not
// an answer to "it did not download".
func TestAFailedDownloadIsReportedInsideTheOverlay(t *testing.T) {
	m := overlayModel(t)
	updated, _ := m.Update(chatview.OpenedPhotoMsg{Err: errors.New("timed out")})
	got := updated.(Model)

	if !got.mediaView.IsVisible() {
		t.Fatal("a failed download closed the overlay")
	}
	if !strings.Contains(got.View().Content, "timed out") {
		t.Error("the overlay does not say why it is empty")
	}
}

// --- yank ----------------------------------------------------------------

func TestYankOutcomesAreAllReported(t *testing.T) {
	tests := []struct {
		name string
		msg  chatview.YankMsg
		want string
	}{
		{"a successful copy", chatview.YankMsg{Runes: 42}, "copied 42 characters"},
		{"nothing to copy", chatview.YankMsg{}, "nothing to copy"},
		{"a failure", chatview.YankMsg{Err: errors.New("no clipboard tool found")},
			"no clipboard tool found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := framedModel(t, 100, 30)
			updated, _ := m.Update(tt.msg)
			// The notice lands on two surfaces at once; the rendered
			// frame is where a reader would actually find it.
			if view := updated.(Model).View().Content; !strings.Contains(view, tt.want) {
				t.Errorf("the frame does not mention %q", tt.want)
			}
		})
	}
}

// The hint bar advertises what the keys do. y was left out while it did
// nothing; leaving it out now would under-report a binding that works.
func TestYankIsAdvertisedInTheHintBar(t *testing.T) {
	m := framedModel(t, 200, 40)
	var found bool
	for _, h := range m.hintsFor(SurfaceChatView) {
		if h.Key == "y" && h.Label == "yank" {
			found = true
		}
	}
	if !found {
		t.Error("the chat view hint set does not offer y yank")
	}
}
