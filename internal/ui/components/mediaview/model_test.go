package mediaview

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ceesaxp/telegram-cli/internal/config"
	"github.com/Ceesaxp/telegram-cli/internal/media"
	"github.com/Ceesaxp/telegram-cli/internal/ui/cell"
	"github.com/Ceesaxp/telegram-cli/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins a colour profile: without one lipgloss resolves a test
// binary to Ascii and every assertion about a rendered surface passes
// because nothing was emitted. See cell.PaintedWidth.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	os.Exit(m.Run())
}

func sized(t *testing.T, w, h int, protocol string) Model {
	t.Helper()
	m := New(theme.DarkRoles(false))
	m.SetSize(w, h)
	m.ApplyMedia(config.MediaConfig{ImageProtocol: protocol})
	return m
}

// pngFile writes a small image and returns its path.
func pngFile(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for y := range 16 {
		for x := range 24 {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 15), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "fixture.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return path
}

// graphicsMarkers are the byte sequences that tell a terminal to draw
// pixels. None of them may appear before an explicit open.
var graphicsMarkers = map[string]string{
	"kitty": "\x1b_G",
	"sixel": "\x1bP",
}

func assertNoGraphics(t *testing.T, where, s string) {
	t.Helper()
	for name, marker := range graphicsMarkers {
		if strings.Contains(s, marker) {
			t.Errorf("%s emitted a %s graphics sequence", where, name)
		}
	}
}

// The design record's exit criterion for phase 8: at the default settings no
// graphics sequence reaches the terminal until the user asks for one. A
// closed overlay draws nothing at all, and an open one that is still
// downloading draws only text.
func TestNoGraphicsBeforeAnOpen(t *testing.T) {
	for _, protocol := range []string{"kitty", "sixel", "blocks"} {
		t.Run(protocol, func(t *testing.T) {
			m := sized(t, 80, 24, protocol)

			if got := m.View(); got != "" {
				t.Errorf("a closed overlay drew %d bytes", len(got))
			}

			m.Open("photo · nadia", "downloading…")
			assertNoGraphics(t, "an overlay that is still downloading", m.View())

			m.Fail("could not download this photo")
			assertNoGraphics(t, "a failed overlay", m.View())
		})
	}
}

func TestTheOverlayFillsTheFrame(t *testing.T) {
	const w, h = 100, 30
	m := sized(t, w, h, "blocks")
	m.Open("photo · nadia · 1280×960", "downloading…")

	lines := strings.Split(m.View(), "\n")
	if len(lines) != h {
		t.Fatalf("drew %d rows, want %d", len(lines), h)
	}
	for i, line := range lines {
		if got := cell.Width(line); got != w {
			t.Errorf("row %d is %d cells, want %d", i, got, w)
		}
		if p := cell.PaintedWidth(line); p != w {
			t.Errorf("row %d: painted %d of %d cells", i, p, w)
		}
		if open := cell.OpenStyle(line); open != "" {
			t.Errorf("row %d leaves %q open", i, open)
		}
	}
}

// A full-screen overlay that does not say how to leave is a trap, and this
// one is a single keystroke away from anywhere in the thread.
func TestTheOverlayAlwaysSaysHowToLeave(t *testing.T) {
	m := sized(t, 80, 24, "blocks")
	m.Open("photo", "downloading…")

	last := strings.Split(m.View(), "\n")[23]
	if !strings.Contains(last, "esc") {
		t.Errorf("the hint row does not name esc: %q", last)
	}
	m.Show(pngFile(t))
	last = strings.Split(m.View(), "\n")[23]
	if !strings.Contains(last, "esc") {
		t.Errorf("the hint row vanished once the image loaded: %q", last)
	}
}

func TestShowDrawsTheImage(t *testing.T) {
	m := sized(t, 60, 20, "blocks")
	m.Open("photo", "downloading…")
	m.Show(pngFile(t))

	view := m.View()
	// Half-blocks are cell content, so the art is text — and it must not
	// have brought a graphics sequence with it.
	assertNoGraphics(t, "the blocks renderer", view)
	if !strings.Contains(view, "▀") && !strings.Contains(view, "█") {
		t.Errorf("no half-block art in the overlay:\n%s", view)
	}
	for i, line := range strings.Split(view, "\n") {
		if got := cell.Width(line); got != 60 {
			t.Errorf("row %d is %d cells, want 60", i, got)
		}
	}
}

// An image the TERMINAL owns is not erased by redrawing text, so closing has
// to say so. Deleting by id and not wholesale, because a bare kitty delete
// takes every placement on the screen — including the thread's inline art.
func TestClosingRemovesAKittyImage(t *testing.T) {
	m := sized(t, 60, 20, "kitty")
	m.Open("photo", "downloading…")
	m.Show(pngFile(t))

	teardown := m.Close()
	if teardown == "" {
		t.Fatal("closing a kitty overlay returned no teardown sequence")
	}
	if !strings.HasPrefix(teardown, "\x1b_Ga=d") {
		t.Errorf("teardown is not a kitty delete: %q", teardown)
	}
	if !strings.Contains(teardown, "i=") {
		t.Errorf("teardown deletes every placement, not just this image: %q", teardown)
	}
	if m.IsVisible() {
		t.Error("the overlay is still visible after Close")
	}
	// Drained: a second close has nothing left to say.
	if again := m.Close(); again != "" {
		t.Errorf("Close returned the teardown twice: %q", again)
	}
}

// Blocks and sixel are cell contents; the next frame overwrites them. A
// teardown for those would be a sequence emitted for no reason.
func TestClosingACellBasedOverlayNeedsNoTeardown(t *testing.T) {
	for _, protocol := range []string{"blocks", "sixel"} {
		t.Run(protocol, func(t *testing.T) {
			m := sized(t, 60, 20, protocol)
			m.Open("photo", "downloading…")
			m.Show(pngFile(t))
			if got := m.Close(); got != "" {
				t.Errorf("teardown = %q, want none", got)
			}
		})
	}
}

// Two images in one overlay must not leave the first one behind: only the
// newer id would ever be deleted otherwise.
func TestASecondImageCarriesTheFirstsTeardown(t *testing.T) {
	m := sized(t, 60, 20, "kitty")
	m.Open("photo", "downloading…")
	m.Show(pngFile(t))
	m.Show(pngFile(t))

	teardown := m.Close()
	if got := strings.Count(teardown, "\x1b_Ga=d"); got != 2 {
		t.Errorf("teardown removes %d images, want 2: %q", got, teardown)
	}
}

// The user asked for this photo. Closing the window they just opened is not
// an answer to "it did not download".
func TestAFailedDownloadKeepsTheOverlayUp(t *testing.T) {
	m := sized(t, 60, 20, "blocks")
	m.Open("photo", "downloading…")
	m.Fail("could not download this photo: timed out")

	if !m.IsVisible() {
		t.Fatal("the overlay closed itself on a failed download")
	}
	if !strings.Contains(m.View(), "timed out") {
		t.Error("the overlay does not say why it is empty")
	}
}

// An undecodable file is reported inside the overlay for the same reason.
func TestAnUndrawableFileIsReportedInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-an-image.png")
	if err := os.WriteFile(path, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := sized(t, 60, 20, "blocks")
	m.Open("photo", "downloading…")
	m.Show(path)

	if !m.IsVisible() {
		t.Fatal("the overlay closed itself on an undecodable file")
	}
	if !strings.Contains(m.View(), "cannot draw this image") {
		t.Errorf("no reason given:\n%s", m.View())
	}
}

func TestResolveProtocolIsWhatTheOverlayDrawsWith(t *testing.T) {
	m := sized(t, 60, 20, "kitty")
	if got := m.protocol; got != media.ProtocolKitty {
		t.Errorf("protocol = %v, want kitty", got)
	}
}

// A dismissed overlay refuses content. The download that raised it outlives
// the keypress that dismissed it, and a picture accepted here would be shown
// to whoever opens the overlay next.
func TestAClosedOverlayRefusesContent(t *testing.T) {
	m := sized(t, 60, 20, "blocks")
	m.Open("photo", "downloading…")
	m.Close()

	m.Show(pngFile(t))
	if len(m.art) != 0 {
		t.Error("a closed overlay accepted a picture")
	}

	m.Fail("timed out")
	if m.status != "" {
		t.Errorf("a closed overlay accepted a status: %q", m.status)
	}
}

// Fail replaces the contents, it does not merely annotate them: a reason
// printed over the previous photo would read as a caption for it.
func TestFailReplacesThePictureItReports(t *testing.T) {
	m := sized(t, 60, 20, "blocks")
	m.Open("photo", "downloading…")
	m.Show(pngFile(t))
	if len(m.art) == 0 {
		t.Fatal("precondition: no picture to replace")
	}

	m.Fail("the connection dropped")
	if len(m.art) != 0 {
		t.Error("the old picture is still up under the failure")
	}
	if !strings.Contains(m.View(), "the connection dropped") {
		t.Error("the overlay does not say what happened")
	}
}
