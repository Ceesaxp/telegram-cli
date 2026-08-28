package clipboard

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requirePasteboardTests skips the test unless the developer has opted in.
// These tests overwrite the real system pasteboard, which is surprising and
// disruptive to run as a side effect of `go test ./...`.
func requirePasteboardTests(t *testing.T) {
	t.Helper()
	if os.Getenv("TELETUI_CLIPBOARD_TESTS") != "1" {
		t.Skip("skipping: this test overwrites the real system pasteboard; set TELETUI_CLIPBOARD_TESTS=1 to run it")
	}
}

// restorePasteboardScript sets the clipboard to the UTF-8 text held in the
// file named by the first argument.
const restorePasteboardScript = `on run argv
	set inFile to item 1 of argv
	set fh to open for access (POSIX file inFile)
	set txt to (read fh as «class utf8»)
	close access fh
	set the clipboard to txt
end run`

// snapshotPasteboardText saves the pasteboard's current text flavor (if any)
// and restores it via t.Cleanup once the test finishes. It is best-effort:
// if the pasteboard holds no text right now (empty, or holding only an
// image), there's nothing to restore.
func snapshotPasteboardText(t *testing.T) {
	t.Helper()
	out, err := exec.Command("osascript", "-e", "the clipboard as text").Output()
	if err != nil || len(out) == 0 {
		return
	}
	// osascript appends exactly one trailing newline of its own when it
	// prints a result to stdout; strip it so a snapshot/restore cycle
	// doesn't grow the text by a newline each time it runs.
	out = bytes.TrimSuffix(out, []byte("\n"))
	saved := filepath.Join(t.TempDir(), "clipboard-snapshot.txt")
	if err := os.WriteFile(saved, out, 0o600); err != nil {
		return
	}
	t.Cleanup(func() {
		cmd := exec.Command("osascript", "-", saved)
		cmd.Stdin = strings.NewReader(restorePasteboardScript)
		_ = cmd.Run() // best-effort restore
	})
}

// setClipboard loads the pasteboard from a file, coerced to the given class.
// It skips the test when the pasteboard is unavailable, as it is on a build
// machine with no window server session.
func setClipboard(t *testing.T, script string) {
	t.Helper()
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		t.Skipf("cannot set the pasteboard in this environment: %v: %s", err, out)
	}
}

func writeTestPNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for x := 0; x < 32; x++ {
		for y := 0; y < 16; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 16), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPasteImageData(t *testing.T) {
	requirePasteboardTests(t)
	snapshotPasteboardText(t)
	t.Cleanup(Cleanup)

	fixture := writeTestPNG(t)
	setClipboard(t, `set the clipboard to (read (POSIX file "`+fixture+`") as «class PNGf»)`)

	res, err := Paste()
	if err != nil {
		t.Fatalf("Paste: %v", err)
	}
	if !res.IsImage || !res.Spooled {
		t.Fatalf("got %+v, want a spooled image", res)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read spooled image: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("spooled file is not a valid PNG: %v", err)
	}
}

// A file copied in Finder lands on the pasteboard as a file reference; it
// should be attached in place rather than spooled.
func TestPasteFileReference(t *testing.T) {
	requirePasteboardTests(t)
	snapshotPasteboardText(t)
	t.Cleanup(Cleanup)

	fixture := writeTestPNG(t)
	setClipboard(t, `set the clipboard to (POSIX file "`+fixture+`")`)

	res, err := Paste()
	if err != nil {
		t.Fatalf("Paste: %v", err)
	}
	if res.Path != fixture {
		t.Errorf("Path = %q, want %q", res.Path, fixture)
	}
	if !res.IsImage {
		t.Error("IsImage = false, want true for a .png file reference")
	}
	if res.Spooled {
		t.Error("Spooled = true, want false for a file already on disk")
	}
}

// Plain text coerces into a bogus file URL, so a text clipboard must report
// "nothing to paste" rather than a made-up path.
func TestPasteTextIsNotAttachable(t *testing.T) {
	requirePasteboardTests(t)
	snapshotPasteboardText(t)
	t.Cleanup(Cleanup)

	setClipboard(t, `set the clipboard to "just some text"`)

	res, err := Paste()
	if err == nil {
		t.Fatalf("Paste succeeded with %+v, want an error", res)
	}
	if err != ErrNoImage {
		t.Errorf("err = %v, want %v", err, ErrNoImage)
	}
}
