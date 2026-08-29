package media

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckImageBounds(t *testing.T) {
	if err := checkImageBounds(100, 1, 1); err != nil {
		t.Fatalf("tiny image: %v", err)
	}
	if err := checkImageBounds(maxImageFileBytes+1, 1, 1); err == nil || !strings.Contains(err.Error(), "image too large") {
		t.Fatalf("oversized file: got %v", err)
	}
	if err := checkImageBounds(100, 5000, 5000); err == nil || !strings.Contains(err.Error(), "image dimensions too large") {
		t.Fatalf("oversized pixels: got %v", err)
	}
	if err := checkImageBounds(100, 0, 10); err == nil || !strings.Contains(err.Error(), "image dimensions too large") {
		t.Fatalf("zero width: got %v", err)
	}
}

func TestRenderFileTinyPNG(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r := NewImageRenderer(ProtocolBlocks, 50, 25)
	out, err := r.RenderFile(path)
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty render")
	}
}
